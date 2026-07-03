package internal

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"math/rand"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type directCollectionPage struct {
	FanData struct {
		FanID int64 `json:"fan_id"`
	} `json:"fan_data"`
	CollectionData directCollectionData `json:"collection_data"`
	HiddenData     directCollectionData `json:"hidden_data"`
}

type directCollectionData struct {
	RedownloadURLs map[string]string `json:"redownload_urls"`
	LastToken      string            `json:"last_token"`
	ItemCount      int               `json:"item_count"`
	BatchSize      int               `json:"batch_size"`
	MoreAvailable  bool              `json:"more_available"`
}

type directDownloadPage struct {
	DigitalItems []directDigitalItem `json:"digital_items"`
}

type directDigitalItem struct {
	Downloads          map[string]map[string]string `json:"downloads"`
	PackageReleaseDate *string                      `json:"package_release_date"`
	Title              string                       `json:"title"`
	Artist             string                       `json:"artist"`
	DownloadType       string                       `json:"download_type"`
	DownloadTypeStr    string                       `json:"download_type_str"`
	ItemType           string                       `json:"item_type"`
}

type directCollectionResponse struct {
	RedownloadURLs map[string]string `json:"redownload_urls"`
	LastToken      string            `json:"last_token"`
	MoreAvailable  bool              `json:"more_available"`
}

type directStatDownload struct {
	DownloadURL string `json:"download_url"`
	URL         string `json:"url"`
}

type directEntry struct {
	id  string
	url string
}

type directJobResult struct {
	entry directEntry
	title string
	err   error
}

func (d *Downloader) DownloadDirect(opts DownloadOpts) error {
	if opts.Filter != "" {
		return fmt.Errorf("direct mode does not support -filter yet; use -mode browser for filtered runs")
	}

	outDir := d.dirPath
	bcdlDir := filepath.Join(outDir, ".bcdl")

	if err := os.Mkdir(outDir, 0o777); err != nil && !os.IsExist(err) {
		return fmt.Errorf("could not create output dir %v", err)
	}
	if err := os.Mkdir(bcdlDir, 0o777); err != nil && !os.IsExist(err) {
		return fmt.Errorf("could not create output dir %v", err)
	}

	history, err := NewHistory(filepath.Join(bcdlDir, "downloaded"))
	if err != nil {
		return fmt.Errorf("failure to get history file %v", err)
	}

	cookieHeader, err := d.cookieHeader()
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: d.timeout}
	entries, err := d.directCollectionEntries(client, cookieHeader)
	if err != nil {
		return err
	}
	log.Printf("Found %d redownload URLs", len(entries))

	notDownloaded := make([]directEntry, 0, len(entries))
	for _, entry := range entries {
		if history.containsDownload(entry.id, d.filetype) {
			log.Printf("Already downloaded %s. Skipping", entry.id)
			continue
		}

		notDownloaded = append(notDownloaded, entry)
		if d.limit > 0 && len(notDownloaded) >= d.limit {
			break
		}
	}

	if len(notDownloaded) == 0 {
		log.Printf("No new downloads found")
		return nil
	}

	workers := d.workers
	if workers <= 0 {
		workers = 1
	}

	jobs := make(chan directEntry, len(notDownloaded))
	results := make(chan directJobResult, len(notDownloaded))
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for entry := range jobs {
				sleepBeforeRequest(d.delayMin, d.delayMax)
				title, err := d.directDownloadEntry(client, cookieHeader, entry)
				results <- directJobResult{entry: entry, title: title, err: err}
			}
		}()
	}

	for _, entry := range notDownloaded {
		opts.OnStart(entry.id)
		jobs <- entry
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	for result := range results {
		name := firstNonEmpty(result.title, result.entry.id)
		if result.err != nil {
			log.Printf("Error: %v", result.err)
			opts.OnFailure(name)
			continue
		}

		history.addItem(result.entry.id, d.filetype)
		history.writeOut()
		opts.OnSuccess(name)
	}

	return nil
}

func (d *Downloader) directCollectionEntries(client *http.Client, cookieHeader string) ([]directEntry, error) {
	collectionURL := bcUrl.JoinPath(d.user.username).String()
	body, err := directGet(client, collectionURL, cookieHeader)
	if err != nil {
		return nil, err
	}

	var collection directCollectionPage
	if err := parsePagedata(body, &collection); err != nil {
		return nil, err
	}

	if collection.FanData.FanID == 0 {
		return nil, fmt.Errorf("could not find fan id; cookies may not be authenticated")
	}

	entries := entriesFromRedownloadURLs(collection.CollectionData.RedownloadURLs)
	log.Printf("Collection reports %d items", collection.CollectionData.ItemCount)

	if collection.CollectionData.LastToken != "" {
		more, err := d.retrieveDirectCollectionPage(client, cookieHeader, collection.FanData.FanID, collection.CollectionData.LastToken, "collection_items")
		if err != nil {
			return nil, err
		}
		entries = append(entries, more...)
	}

	if collection.HiddenData.ItemCount > 0 || len(collection.HiddenData.RedownloadURLs) > 0 {
		log.Printf("Hidden collection reports %d items", collection.HiddenData.ItemCount)
		entries = append(entries, entriesFromRedownloadURLs(collection.HiddenData.RedownloadURLs)...)
		if collection.HiddenData.LastToken != "" {
			more, err := d.retrieveDirectCollectionPage(client, cookieHeader, collection.FanData.FanID, collection.HiddenData.LastToken, "hidden_items")
			if err != nil {
				return nil, err
			}
			entries = append(entries, more...)
		}
	}

	return entries, nil
}

func (d *Downloader) retrieveDirectCollectionPage(client *http.Client, cookieHeader string, fanID int64, lastToken string, collectionName string) ([]directEntry, error) {
	var entries []directEntry
	moreAvailable := true

	for moreAvailable {
		payload, err := json.Marshal(map[string]interface{}{
			"fan_id":           fanID,
			"older_than_token": lastToken,
		})
		if err != nil {
			return nil, err
		}

		endpoint := bcUrl.JoinPath("api", "fancollection", "1", collectionName).String()
		body, err := directPostJSON(client, endpoint, cookieHeader, payload)
		if err != nil {
			return nil, err
		}

		var page directCollectionResponse
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("could not parse collection page response: %w", err)
		}

		entries = append(entries, entriesFromRedownloadURLs(page.RedownloadURLs)...)
		lastToken = page.LastToken
		moreAvailable = page.MoreAvailable

		sleepBeforeRequest(d.delayMin, d.delayMax)
	}

	return entries, nil
}

func entriesFromRedownloadURLs(redownloadURLs map[string]string) []directEntry {
	entries := make([]directEntry, 0, len(redownloadURLs))
	for id, entryURL := range redownloadURLs {
		entries = append(entries, directEntry{id: id, url: entryURL})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].id < entries[j].id
	})
	return entries
}

func (d *Downloader) directDownloadEntry(client *http.Client, cookieHeader string, entry directEntry) (string, error) {
	pageBody, err := directGet(client, entry.url, cookieHeader)
	if err != nil {
		return "", fmt.Errorf("could not fetch download page: %w", err)
	}

	var downloadPage directDownloadPage
	if err := parsePagedata(pageBody, &downloadPage); err != nil {
		return "", err
	}
	if len(downloadPage.DigitalItems) == 0 {
		return "", fmt.Errorf("download page has no digital items")
	}

	item := downloadPage.DigitalItems[0]
	title := strings.TrimSpace(fmt.Sprintf("%s - %s", item.Artist, item.Title))
	downloads := item.Downloads
	if len(downloads) == 0 {
		return title, fmt.Errorf("release has no downloads")
	}

	downloadInfo, ok := downloads[string(d.filetype)]
	if !ok {
		return title, fmt.Errorf("release does not provide %s", d.filetype)
	}

	formatURL := downloadInfo["url"]
	if formatURL == "" {
		return title, fmt.Errorf("release has no URL for %s", d.filetype)
	}

	realURL, err := directRealDownloadURL(client, cookieHeader, formatURL)
	if err != nil {
		return title, err
	}

	path, err := directDownloadFile(client, realURL, d.dirPath)
	if err != nil {
		return title, err
	}

	log.Printf("Downloaded %s to %s", title, path)
	return title, nil
}

func directRealDownloadURL(client *http.Client, cookieHeader string, formatURL string) (string, error) {
	statURL := strings.Replace(formatURL, "/download/", "/statdownload/", 1)
	statURL = strings.Replace(statURL, "http:", "https:", 1)
	statURL = statURL + fmt.Sprintf("&.vrs=1&.rand=%d", rand.Int())

	body, err := directGet(client, statURL, cookieHeader)
	if err != nil {
		return "", fmt.Errorf("could not fetch statdownload URL: %w", err)
	}

	jsonBody := statdownloadJSON(body)
	var parsed directStatDownload
	if err := json.Unmarshal(jsonBody, &parsed); err != nil {
		return "", fmt.Errorf("could not parse statdownload response: %w", err)
	}

	return firstNonEmpty(parsed.DownloadURL, parsed.URL, formatURL), nil
}

func statdownloadJSON(body []byte) []byte {
	body = bytes.TrimSpace(body)
	prefix := regexp.MustCompile(`(?s)^\s*if\s*\(\s*window\.Downloads\s*\)\s*\{\s*Downloads\.statResult\s*\(`)
	suffix := regexp.MustCompile(`(?s)\s*\)\s*};\s*$`)
	body = prefix.ReplaceAll(body, nil)
	body = suffix.ReplaceAll(body, nil)
	return bytes.TrimSpace(body)
}

func directDownloadFile(client *http.Client, fileURL string, outDir string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, fileURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download failed with HTTP %d", resp.StatusCode)
	}

	filename := directFilename(resp.Header.Get("Content-Disposition"), fileURL)
	path := filepath.Join(outDir, filename)
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return "", err
	}

	return path, nil
}

func directFilename(contentDisposition string, fileURL string) string {
	if contentDisposition != "" {
		_, params, err := mime.ParseMediaType(contentDisposition)
		if err == nil {
			if filename := firstNonEmpty(params["filename*"], params["filename"]); filename != "" {
				return filename
			}
		}
	}

	parsedURL, err := url.Parse(fileURL)
	if err != nil {
		return "download.zip"
	}

	filename := filepath.Base(parsedURL.Path)
	if filename == "." || filename == "/" || filename == "" {
		return "download.zip"
	}
	return filename
}

func parsePagedata(body []byte, target interface{}) error {
	re := regexp.MustCompile(`(?s)<div id="pagedata" data-blob="(.*?)"></div>`)
	match := re.FindSubmatch(body)
	if len(match) < 2 {
		return fmt.Errorf("could not find #pagedata[data-blob]")
	}

	blob := html.UnescapeString(string(match[1]))
	if err := json.Unmarshal([]byte(blob), target); err != nil {
		return fmt.Errorf("could not parse #pagedata JSON: %w", err)
	}

	return nil
}

func directGet(client *http.Client, targetURL string, cookieHeader string) ([]byte, error) {
	return directRequest(client, http.MethodGet, targetURL, cookieHeader, nil)
}

func directPostJSON(client *http.Client, targetURL string, cookieHeader string, body []byte) ([]byte, error) {
	return directRequest(client, http.MethodPost, targetURL, cookieHeader, body)
}

func directRequest(client *http.Client, method string, targetURL string, cookieHeader string, body []byte) ([]byte, error) {
	const maxAttempts = 6

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}

		req, err := http.NewRequest(method, targetURL, reader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Cookie", cookieHeader)
		req.Header.Set("User-Agent", "Mozilla/5.0")
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := client.Do(req)
		if err != nil {
			if attempt == maxAttempts {
				return nil, err
			}
			time.Sleep(backoffDelay(attempt, nil))
			continue
		}

		responseBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return responseBody, nil
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			if attempt < maxAttempts {
				delay := backoffDelay(attempt, resp.Header)
				log.Printf("%s %s returned HTTP %d; retrying in %s", method, targetURL, resp.StatusCode, delay)
				time.Sleep(delay)
				continue
			}
		}

		return nil, fmt.Errorf("%s %s failed with HTTP %d", method, targetURL, resp.StatusCode)
	}

	return nil, fmt.Errorf("%s %s failed after retries", method, targetURL)
}

func backoffDelay(attempt int, header http.Header) time.Duration {
	if header != nil {
		if retryAfter := header.Get("Retry-After"); retryAfter != "" {
			if seconds, err := strconv.Atoi(retryAfter); err == nil {
				return time.Duration(seconds) * time.Second
			}
			if retryAt, err := http.ParseTime(retryAfter); err == nil {
				delay := time.Until(retryAt)
				if delay > 0 {
					return delay
				}
			}
		}
	}

	base := time.Duration(1<<uint(attempt-1)) * time.Second
	jitter := time.Duration(rand.Int63n(int64(time.Second)))
	return base + jitter
}

func (d *Downloader) cookieHeader() (string, error) {
	cookies, err := d.httpCookies()
	if err != nil {
		return "", err
	}

	pairs := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie.Name == "" {
			continue
		}
		pairs = append(pairs, cookie.Name+"="+cookie.Value)
	}

	if len(pairs) == 0 {
		return "", fmt.Errorf("no cookies available")
	}

	return strings.Join(pairs, "; "), nil
}

func (d *Downloader) httpCookies() ([]*http.Cookie, error) {
	if d.cookiesFile == "" {
		if d.user.identity == "" {
			return nil, fmt.Errorf("direct mode requires -cookies-file or -identity-file")
		}
		return []*http.Cookie{{Name: "identity", Value: d.user.identity}}, nil
	}

	if strings.HasSuffix(strings.ToLower(d.cookiesFile), ".json") {
		return httpCookiesFromJSONFile(d.cookiesFile, d.user.identity)
	}

	return httpCookiesFromNetscapeFile(d.cookiesFile, d.user.identity)
}

func httpCookiesFromJSONFile(path string, identity string) ([]*http.Cookie, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read cookies file: %w", err)
	}

	var exported []exportedCookie
	if err := json.Unmarshal(data, &exported); err != nil {
		return nil, fmt.Errorf("could not parse JSON cookies file: %w", err)
	}

	cookies := make([]*http.Cookie, 0, len(exported))
	for _, cookie := range exported {
		name := firstNonEmpty(cookie.Name, cookie.NameRaw)
		value := firstNonEmpty(cookie.Value, cookie.ContentRaw)
		domain := normalizeCookieDomain(firstNonEmpty(cookie.Domain, cookie.Host, cookie.HostRaw))
		if name == "" || value == "" || !strings.Contains(domain, "bandcamp.com") {
			continue
		}
		cookies = append(cookies, &http.Cookie{Name: name, Value: value})
	}

	if identity != "" {
		cookies = append(cookies, &http.Cookie{Name: "identity", Value: identity})
	}

	if len(cookies) == 0 {
		return nil, fmt.Errorf("no bandcamp.com cookies found in %s", path)
	}
	log.Printf("Loaded %d Bandcamp cookies from %s", len(cookies), path)
	return cookies, nil
}

func httpCookiesFromNetscapeFile(path string, identity string) ([]*http.Cookie, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("could not read cookies file: %w", err)
	}
	defer file.Close()

	var cookies []*http.Cookie
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) != 7 || !strings.Contains(normalizeCookieDomain(parts[0]), "bandcamp.com") {
			continue
		}

		cookies = append(cookies, &http.Cookie{Name: parts[5], Value: parts[6]})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("could not scan cookies file: %w", err)
	}

	if identity != "" {
		cookies = append(cookies, &http.Cookie{Name: "identity", Value: identity})
	}

	if len(cookies) == 0 {
		return nil, fmt.Errorf("no bandcamp.com cookies found in %s", path)
	}
	log.Printf("Loaded %d Bandcamp cookies from %s", len(cookies), path)
	return cookies, nil
}
