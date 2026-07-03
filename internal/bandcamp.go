package internal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

// AuthorizedBandcampContext setups up a new playwright.BrowserContext with
// the correct Cookies needed to act as a logged in User.
// This is required for downloading any files.
type AuthorizedBandcampContext struct {
	ctx      playwright.BrowserContext
	identity string
}

type exportedCookie struct {
	Name       string  `json:"name"`
	Value      string  `json:"value"`
	Domain     string  `json:"domain"`
	Host       string  `json:"host"`
	Path       string  `json:"path"`
	Expires    float64 `json:"expires"`
	Expiration float64 `json:"expirationDate"`
	HttpOnly   bool    `json:"httpOnly"`
	Secure     bool    `json:"secure"`
	NameRaw    string  `json:"Name raw"`
	ContentRaw string  `json:"Content raw"`
	HostRaw    string  `json:"Host raw"`
	PathRaw    string  `json:"Path raw"`
}

var bcUrl = url.URL{
	Scheme: "https",
	Host:   "bandcamp.com",
}

// NewAuthorizedBandcampContext setups an NewAuthorizedBandcampContext.
//
// It takes in two parameters: an instance of a playwright.Browser and an identity string.
// The identity string must be the value from the "identity" cookie stored in a User's browser
// after successful authentication.
//
// Playwright has some issues running into captcha challenges during the login procedure, so this
// method is the most full proof, if a bit annoying.
func NewAuthorizedBandcampContext(browser playwright.Browser, identity string, cookiesFile string) (AuthorizedBandcampContext, error) {
	cookies, err := loadCookies(identity, cookiesFile)
	if err != nil {
		return AuthorizedBandcampContext{}, err
	}

	oss := playwright.OptionalStorageState{
		Cookies: cookies,
	}

	// Set up the storage state and context with realistic browser parameters
	ctx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		UserAgent:         playwright.String("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/116.0.0.0 Safari/537.36"),
		Viewport:          &playwright.Size{Width: 1920, Height: 1080},
		DeviceScaleFactor: playwright.Float(1.0),
		HasTouch:          playwright.Bool(false),
		JavaScriptEnabled: playwright.Bool(true),
		TimezoneId:        playwright.String("America/New_York"),
		Locale:            playwright.String("en-US"),
		StorageState:      &oss,
	})

	if err != nil {
		return AuthorizedBandcampContext{}, err
	}

	return AuthorizedBandcampContext{ctx: ctx, identity: identity}, nil
}

func loadCookies(identity string, cookiesFile string) ([]playwright.OptionalCookie, error) {
	if cookiesFile != "" {
		cookies, err := loadCookiesFile(cookiesFile)
		if err != nil {
			return nil, err
		}
		if identity != "" {
			cookies = append(cookies, identityCookie(identity).ToOptionalCookie())
		}
		return cookies, nil
	}

	if identity == "" {
		return nil, fmt.Errorf("either identity or cookies file must be provided")
	}

	return []playwright.OptionalCookie{identityCookie(identity).ToOptionalCookie()}, nil
}

func identityCookie(identity string) playwright.Cookie {
	return playwright.Cookie{
		Name:     "identity",
		Value:    identity,
		Domain:   bcUrl.Host,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		Expires:  float64(time.Now().Add(180 * 24 * time.Hour).Unix()),
	}
}

func loadCookiesFile(path string) ([]playwright.OptionalCookie, error) {
	if strings.HasSuffix(strings.ToLower(path), ".json") {
		return loadJSONCookies(path)
	}

	return loadNetscapeCookies(path)
}

func loadJSONCookies(path string) ([]playwright.OptionalCookie, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read cookies file: %w", err)
	}

	var exported []exportedCookie
	if err := json.Unmarshal(data, &exported); err != nil {
		return nil, fmt.Errorf("could not parse JSON cookies file: %w", err)
	}

	cookies := make([]playwright.OptionalCookie, 0, len(exported))
	for _, c := range exported {
		name := firstNonEmpty(c.Name, c.NameRaw)
		value := firstNonEmpty(c.Value, c.ContentRaw)
		domain := normalizeCookieDomain(firstNonEmpty(c.Domain, c.Host, c.HostRaw))
		cookiePath := firstNonEmpty(c.Path, c.PathRaw, "/")
		expires := c.Expires
		if expires == 0 {
			expires = c.Expiration
		}

		if name == "" || value == "" || domain == "" || !strings.Contains(domain, "bandcamp.com") {
			continue
		}

		cookies = append(cookies, playwright.Cookie{
			Name:     name,
			Value:    value,
			Domain:   domain,
			Path:     cookiePath,
			Secure:   c.Secure,
			HttpOnly: c.HttpOnly,
			Expires:  expires,
		}.ToOptionalCookie())
	}

	if len(cookies) == 0 {
		return nil, fmt.Errorf("no bandcamp.com cookies found in %s", path)
	}

	log.Printf("Loaded %d Bandcamp cookies from %s", len(cookies), path)
	return cookies, nil
}

func loadNetscapeCookies(path string) ([]playwright.OptionalCookie, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("could not read cookies file: %w", err)
	}
	defer file.Close()

	var cookies []playwright.OptionalCookie
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) != 7 {
			continue
		}

		domain := normalizeCookieDomain(parts[0])
		if !strings.Contains(domain, "bandcamp.com") {
			continue
		}

		expires, _ := strconv.ParseFloat(parts[4], 64)
		cookies = append(cookies, playwright.Cookie{
			Name:    parts[5],
			Value:   parts[6],
			Domain:  domain,
			Path:    firstNonEmpty(parts[2], "/"),
			Secure:  strings.EqualFold(parts[3], "TRUE"),
			Expires: expires,
		}.ToOptionalCookie())
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("could not scan cookies file: %w", err)
	}

	if len(cookies) == 0 {
		return nil, fmt.Errorf("no bandcamp.com cookies found in %s", path)
	}

	log.Printf("Loaded %d Bandcamp cookies from %s", len(cookies), path)
	return cookies, nil
}

func normalizeCookieDomain(domain string) string {
	domain = strings.TrimSpace(domain)
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimSuffix(domain, "/")
	if domain == "bandcamp.com" {
		return domain
	}
	if strings.HasSuffix(domain, ".bandcamp.com") {
		return domain
	}
	return domain
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// NewCollectionPage creates a Page Object that represents the user's collection of albums,
//
// Example collection URL: https://bandcamp.com/jbeard
// Username is: jbeard
func (bcCtx AuthorizedBandcampContext) NewCollectionPage(username string) (CollectionPage, error) {
	page, err := bcCtx.ctx.NewPage()

	if err != nil {
		return CollectionPage{}, err
	}

	return newCollectionPage(page, username), nil
}

// NewCollectionEntryPage creates a Page Object that represents an individual entry, i.e. an album, in the user's collection.
func (bcCtx AuthorizedBandcampContext) NewCollectionEntryPage(entry CollectionEntry) (CollectionEntryPage, error) {
	page, err := bcCtx.ctx.NewPage()

	if err != nil {
		return CollectionEntryPage{}, err
	}

	return newCollectionEntryPage(page, entry), nil

}

// CollectionPage represents the user's collection of albums on Bandcamp.
type CollectionPage struct {
	page     playwright.Page
	url      url.URL
	username string
}

// CollectionEntry, i.e. an album.
type CollectionEntry struct {
	url   url.URL
	title string
	id    string
}

// NewCollectionPage creates a Page Object that represents the user's collection of albums.
func newCollectionPage(page playwright.Page, username string) CollectionPage {
	cp := CollectionPage{
		username: username,
		page:     page,
		url:      *bcUrl.JoinPath(username),
	}

	return cp
}

// Goto executes the Playwright Goto method to the collection URL.
func (cp CollectionPage) Goto() (playwright.Response, error) {
	resp, err := cp.page.Goto(cp.url.String(), playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	})
	if err != nil {
		return resp, err
	}
	timeout := 30_000.0
	err = cp.page.Locator(".collection-item-container, div#collection-search").First().WaitFor(playwright.LocatorWaitForOptions{Timeout: &timeout})
	return resp, err
}

// Close wraps the Playwright page.Close() method.
func (cp CollectionPage) Close() error {
	return cp.page.Close()
}

// Filter uses the search box on the collection page to filter the results.
// This method is not public since it requires some special knowledge of how
// BC likes to show/hide things in the UI when searching.
//
// The filter parameter, if empty, will set the search box to blank.
func (cp CollectionPage) filter(filter string) error {
	if len(filter) <= 0 {
		return nil
	}

	input := cp.page.Locator("div#collection-search > input.search-box")

	err := input.Fill(filter)

	if err != nil {
		return err
	}

	// Don't wait too long for the results to return.
	timeout := 1_000.0
	return cp.page.Locator("div#collection-search.searched").WaitFor(playwright.LocatorWaitForOptions{Timeout: &timeout})
}

func (cp CollectionPage) HasMore() (bool, error) {
	return cp.ShowMoreButton().IsVisible()
}

func (cp CollectionPage) ShowMoreButton() playwright.Locator {
	return cp.page.Locator("div#collection-items > div.expand-container > button.show-more")
}

func (cp CollectionPage) AcceptCookiesModal() playwright.Locator {
	return cp.page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{Name: "Accept All"})
}

func (cp CollectionPage) Entries(filter string) ([]CollectionEntry, error) {
	var entries []playwright.Locator
	var error error
	// Have to use a different process for getting entries depending on if the list is filtered
	if filter == "" {
		entries, error = cp.page.Locator(".collection-item-container").All()
	} else {
		entries, error = cp.page.Locator("div#collection-search-items li.collection-item-container").All()
	}

	if error != nil {
		return []CollectionEntry{}, error
	}

	collectionEntries := make([]CollectionEntry, 0, cap(entries))

	for _, entry := range entries {
		title, err := entry.Locator("div.collection-title-details > a > div.collection-item-title").InnerText()
		if err != nil || title == "" {
			continue
		}

		href, err := entry.Locator("span.redownload-item a").GetAttribute("href")
		if err != nil || href == "" {
			continue
		}

		url, err := url.Parse(href)

		if err != nil || url.String() == "" {
			continue
		}

		ce := CollectionEntry{
			url:   *url,
			title: title,
			id:    collectionEntryID(*url, title),
		}

		collectionEntries = append(collectionEntries, ce)
	}

	return collectionEntries, nil
}

func (cp CollectionPage) ScrollPage() {
	loc := cp.ShowMoreButton()

	visible, _ := loc.IsVisible()

	if visible {
		loc.Click()
		return
	}

	respUrl := bcUrl.JoinPath("api", "fancollection", "1", "collection_items")
	err := cp.page.Mouse().Wheel(0, 10_000)

	if err != nil {
		log.Printf("Tried to scroll. At bottom?: %v", err)
	}

	_, err = cp.page.ExpectResponse(respUrl.String(), func() error { return nil })

	if err != nil {
		log.Printf("No response received from scroll. At bottom?: %v", err)
	}
}

func (cp CollectionPage) ScrollTimes() (int, error) {

	albumCount, err := cp.AlbumCount()

	if err != nil {
		return 0, err
	}

	return int(math.Ceil(float64(albumCount) / 20.0)), nil
}

func (cp CollectionPage) AlbumCount() (int, error) {
	button := cp.ShowMoreButton()

	visible, _ := button.IsVisible()

	albumCount := 0

	if visible {
		albums, err := button.TextContent()

		if err != nil {
			log.Printf("No more to load. Continuing... %v", err)
		}

		// Get the count of how many more albums there are to grab
		var re = regexp.MustCompile(`\b\d+\b`)
		converted, err := strconv.Atoi(re.FindString(albums))

		if err == nil {
			albumCount = converted
		}

	} else {
		albumCount = 20
	}

	return albumCount, nil
}

// GetCollection returns all items on the collection page.
// It will automatically handle scrolling the page a number of times to ensure
// all of them are loaded onto the screen.
//
// This is calculated by finding the Show More button which has a count of albums,
// dividing by 20 to approximate the number of times the page must be scrolled.
//
// A collection can contain non-album items like subscriptions to labels. These entries
// are malformed and skipped. The resulting entry set will only contain entries that
// were successfully parsed.
func (cp CollectionPage) GetCollection(filter string) ([]CollectionEntry, error) {
	err := cp.filter(filter)

	if err != nil {
		return []CollectionEntry{}, fmt.Errorf("Failed to filter albums %w", err)
	}

	moreToShow, err := cp.page.Locator("div#collection-items > div.expand-container").IsVisible()

	var albumCount int = 0

	// Bandcamp keeps the button but hides the parent container. Only go through the process of
	// clicking the button if the parent container is visible
	if moreToShow {
		loc := cp.page.Locator("div#collection-items > div.expand-container > button.show-more")
		albums, err := loc.TextContent()

		if err != nil {
			log.Printf("No more to load. Continuing... %v", err)
		}

		// Get the count of how many more albums there are to grab
		var re = regexp.MustCompile(`\b\d+\b`)
		converted, err := strconv.Atoi(re.FindString(albums))

		if err == nil {
			albumCount = converted
		}

		err = loc.Click()
		if err != nil {
			return []CollectionEntry{}, fmt.Errorf("Could not click button to load more albums: %w", err)
		}
	}

	// BC seems to load in increments of 20 at the default window size for Playwright.
	// Thus we need to scroll a number of times to get every album
	scrollTimes := int(math.Ceil(float64(albumCount) / 20.0))

	if err != nil {
		log.Printf("Nothing more to show %v", err)
	}

	// Expect a REST request made against this endpoint every time we scroll
	respUrl := bcUrl.JoinPath("api", "fancollection", "1", "collection_items")
	// Perform scrolling and wait for the API to return the results
	for i := 0; i < scrollTimes; i++ {
		err := cp.page.Mouse().Wheel(0, 10_000)

		if err != nil {
			log.Printf("Error when scrolling. Continuing...")
			continue
		}

		_, err = cp.page.ExpectResponse(respUrl.String(), func() error { return nil })

		if err != nil {
			log.Printf("Error waiting for response to scroll. Continuing...")
		}
	}

	var entries []playwright.Locator

	// Have to use a different process for getting entries depending on if the list is filtered
	if filter == "" {
		entries, _ = cp.page.Locator(".collection-item-container").All()
	} else {
		entries, _ = cp.page.Locator("div#collection-search-items li.collection-item-container").All()
	}

	collectionEntries := make([]CollectionEntry, 0, cap(entries))

	for _, entry := range entries {
		title, err := entry.Locator("div.collection-title-details > a > div.collection-item-title").InnerText()
		if err != nil || title == "" {
			continue
		}

		href, err := entry.Locator("span.redownload-item a").GetAttribute("href")
		if err != nil || href == "" {
			continue
		}

		url, err := url.Parse(href)

		if err != nil || url.String() == "" {
			continue
		}

		ce := CollectionEntry{
			url:   *url,
			title: title,
			id:    collectionEntryID(*url, title),
		}

		collectionEntries = append(collectionEntries, ce)

	}

	return collectionEntries, nil
}

func collectionEntryID(entryURL url.URL, fallback string) string {
	for _, value := range entryURL.Query() {
		for _, part := range value {
			if id := saleItemID(part); id != "" {
				return id
			}
		}
	}

	if id := saleItemID(entryURL.String()); id != "" {
		return id
	}

	return fallback
}

func saleItemID(value string) string {
	re := regexp.MustCompile(`\b[prct]\d+\b`)
	return re.FindString(value)
}

type DownloadEntriesOpts struct {
	OutDir string
	Filter string
}

func (page CollectionPage) DownloadEntries(scrollTo int, outDir string, history *History, fileType FileType, context AuthorizedBandcampContext, workers int, limit int, delayMin time.Duration, delayMax time.Duration, opts DownloadOpts) error {
	err := page.filter(opts.Filter)
	if err != nil {
		return fmt.Errorf("could not filter collection: %w", err)
	}
	// 0. Get first page of entries
	// 1. Enqueue jobs
	// 2. Scroll if there are more
	// 3. Enqueue next set of jobs
	// 4. Ensure no duplicates - should be able to use in memory history
	// 5. continue until done

	for range scrollTo {
		page.ScrollPage()
	}
	entries, err := page.Entries(opts.Filter)

	if err != nil {
		return fmt.Errorf("Could not get your collection. Err: %v\nCheck that you have the correct identity cookie value", err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("found collection items but no redownload links; exported cookies are probably not authenticated. Provide the identity cookie with -password or re-export cookies from a logged-in browser")
	}

	notDownloaded := make([]CollectionEntry, 0, len(entries))
	// Get the album name and every download link
	for _, entry := range entries {
		log.Printf("Starting job for %s", entry.title)
		// Skip any previously downloaded files
		if history.containsDownload(entry.id, fileType) {
			log.Printf("Already downloaded %s. Skipping", entry.title)
			continue
		}

		notDownloaded = append(notDownloaded, entry)
		if limit > 0 && len(notDownloaded) >= limit {
			break
		}

	}
	// Set up jobs
	jobs := make(chan downloadJob, len(notDownloaded))
	results := make(chan downloadJob, len(notDownloaded))

	if workers <= 0 {
		workers = 1
	}
	for w := 0; w < workers; w++ {
		go worker(w, jobs, results, context, delayMin, delayMax)
	}

	for _, entry := range notDownloaded {
		opts.OnStart(entry.title)
		// Enqueue those jobs
		jobs <- downloadJob{
			Entry:       entry,
			DownloadDir: outDir,
			filetype:    fileType,
			timeout:     time.Duration(time.Minute * 4),
			logger:      NewContextLogger(fmt.Sprintf("Job: %s", entry.title)),
		}

	}

	for range notDownloaded {
		job := <-results
		if job.Success {
			history.addItem(job.Entry.id, fileType)
			opts.OnSuccess(job.Entry.title)
		} else {
			log.Printf("Error: %v", job.err)
			opts.OnFailure(job.Entry.title)
		}
	}

	close(jobs)
	close(results)
	history.writeOut()

	return nil
}

// CollectionEntryPage represents a specific album.
type CollectionEntryPage struct {
	page   playwright.Page
	entry  CollectionEntry
	logger *ContextLogger
}

func newCollectionEntryPage(page playwright.Page, entry CollectionEntry) CollectionEntryPage {
	logger := NewContextLogger(fmt.Sprintf("Album: %s", entry.title))

	return CollectionEntryPage{
		page:   page,
		entry:  entry,
		logger: logger,
	}
}

// Goto navigates to the page for the Collection Entry
func (cep CollectionEntryPage) Goto() (playwright.Response, error) {
	cep.logger.Printf("Navigating to album page")
	resp, err := cep.page.Goto(cep.entry.url.String(), playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	})
	if err != nil {
		cep.logger.Printf("Failed to navigate: %v", err)
		return resp, err
	}
	timeout := 30_000.0
	err = cep.page.Locator("select#format-type, input.reauth-email").First().WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateAttached,
		Timeout: &timeout,
	})
	return resp, err
}

func (cp CollectionEntryPage) ReauthInput() playwright.Locator {
	return cp.page.Locator("input.reauth-email")
}

func (cp CollectionEntryPage) SubmitReauthFormButton() playwright.Locator {
	return cp.page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{Name: "GO"})
}

// SelectFileType selects the specified file type and waits for it to be ready to download.
//
// Supported file types are:
//   - MP3_V0
//   - MP3_320
//   - FLAC
//   - AAC_HI
//   - VORBIS
//   - ALAC
//   - WAV
//   - AIFF_LOSLESS
//
// MP3_V0 produces the smallest files and the quickest downloads. Formats like FLAC will
// require generous allowances for timeouts as they can be large and take a while to prepare
func (cep CollectionEntryPage) SelectFileType(ft FileType) error {
	cep.logger.Printf("Selecting file type: %s", ft)
	value := []string{string(ft)}
	//selector := fmt.Sprintf("option[value=\"%s\"]", ft)
	//cep.page.Locator(selector).WaitFor(playwright.LocatorWaitForOptions{})

	_, err := cep.page.Locator("select#format-type").SelectOption(playwright.SelectOptionValues{Values: &value})

	if err != nil {
		cep.logger.Printf("Error selecting format %s: %v", ft, err)
		return fmt.Errorf("Error when selecting option %s: %w", ft, err)
	}

	cep.logger.Printf("Successfully selected file type: %s", ft)
	return nil
}

func (cep CollectionEntryPage) MustReauth() (bool, error) {

	cep.logger.Printf("Checking for reauth...")
	return cep.ReauthInput().IsVisible()
}

func (cep CollectionEntryPage) WaitForManualReauth(timeout time.Duration) error {
	cep.logger.Printf("Reauth requested. Complete the prompt in the browser window.")
	timeoutMs := float64(timeout.Milliseconds())
	return cep.ReauthInput().WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateHidden,
		Timeout: &timeoutMs,
	})
}

func (cep CollectionEntryPage) HandleReauth(email string) error {
	reauthVisible, _ := cep.MustReauth()

	if reauthVisible {
		// TODO: Get the user email from inputs!
		err := cep.ReauthInput().Fill("jprokay@gmail.com")
		if err != nil {

			cep.logger.Printf("Could not fill in reauth input: %v", err)
			return fmt.Errorf("Could not reauth: %w", err)
		}

		err = cep.SubmitReauthFormButton().Click()
		if err != nil {

			cep.logger.Printf("Could not submit reauth form: %v", err)
			return fmt.Errorf("Could not reauth: %w", err)
		}

	}
	return nil
}

// DownloadFile starts a browser download and saves it to the specified outputDir.
// timeoutMs controls how long to wait for the download to Prepare NOT how long to
// wait for the download to complete!
//
// Depending on the file type, it can take longer for the download to hit the Prepared
// state
func (cep CollectionEntryPage) DownloadFile(outputDir string, timeoutMs float64) error {
	cep.logger.Printf("Starting download (timeout: %.1f seconds)", timeoutMs/1000)
	dl, err := cep.page.ExpectDownload(func() error {
		return cep.page.Locator(`.download-button + a`).Click()
	}, playwright.PageExpectDownloadOptions{
		Timeout: &timeoutMs,
	})

	if err != nil {
		cep.logger.Printf("Download failed: %v", err)
		return fmt.Errorf("Could not start download: %w", err)
	}

	// Download the file and save using the browser suggested name
	path := filepath.Join(outputDir, dl.SuggestedFilename())
	cep.logger.Printf("Downloading to: %s", path)

	err = dl.SaveAs(path)

	if err != nil {
		cep.logger.Printf("Failed to save file: %v", err)
		return fmt.Errorf("Could not download file: %w", err)
	}

	cep.logger.Printf("Download completed successfully")
	return nil
}

func (cp CollectionEntryPage) Close() error {
	return cp.page.Close()
}
