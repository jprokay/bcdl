package internal

import (
	"fmt"
	"log"
	"math"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
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

var bcUrl = url.URL{
	Scheme: "https",
	Host:   "bandcamp.com",
}

// NewAuthorizedBandcampContext setups an NewAuthorizedBandcampContext.
//
// It takes in two parameters: an instance of a playwright.Browswer and an identity string.
// The identity string must be the value from the "identity" cookie stored in a User's browser
// after successful authentication.
//
// Playwright has some issues running into captcha challenges during the login procedure, so this
// method is the most full proof, if a bit annoying.
func NewAuthorizedBandcampContext(browser playwright.Browser, identity string) (AuthorizedBandcampContext, error) {
	// Cookie to handle login
	// Would be great to get rid of this and do a login flow to get the value
	cookie := playwright.Cookie{
		Name:     "identity",
		Value:    identity,
		Domain:   bcUrl.Host,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		Expires:  float64(time.Now().Add(180 * 24 * time.Hour).Unix()),
	}

	var cookies []playwright.OptionalCookie

	cookies = append(cookies, cookie.ToOptionalCookie())
	oss := playwright.OptionalStorageState{
		Cookies: cookies,
	}

	// Set up the storage state and context
	ctx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		UserAgent:    playwright.String("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/84.0.4147.135 Safari/537.36"),
		StorageState: &oss,
	})

	if err != nil {
		return AuthorizedBandcampContext{}, err
	}

	return AuthorizedBandcampContext{ctx: ctx, identity: identity}, nil
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
	return cp.page.Goto(cp.url.String(), playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	})
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
	input := cp.page.Locator("div#collection-search > input.search-box")

	input.Fill(filter)

	// Don't wait too long for the results ot return.
	timeout := 10_000.0
	return cp.page.Locator("div#collection-search.searched").WaitFor(playwright.LocatorWaitForOptions{Timeout: &timeout})
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
	cp.filter(filter)
	moreToShow, err := cp.page.Locator("div#collection-items > div.expand-container").IsHidden()

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

		loc.Click()
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
		cp.page.Mouse().Wheel(0, 10_000)

		cp.page.ExpectResponse(respUrl.String(), func() error { return nil })
	}

	var entries []playwright.Locator

	// Have to use a different process for gettng entries depending on if the list is filtered
	if filter == "" {
		entries, err = cp.page.Locator(".collection-item-container").All()
	} else {
		entries, err = cp.page.Locator("div#collection-search-items li.collection-item-container").All()
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
		}

		collectionEntries = append(collectionEntries, ce)

	}

	return collectionEntries, nil
}

// CollectionEntryPage represents a specific album.
type CollectionEntryPage struct {
	page  playwright.Page
	entry CollectionEntry
}

func newCollectionEntryPage(page playwright.Page, entry CollectionEntry) CollectionEntryPage {

	return CollectionEntryPage{
		page:  page,
		entry: entry,
	}
}

// Goto navigates to the page for the Collection Entry
func (cep CollectionEntryPage) Goto() (playwright.Response, error) {
	return cep.page.Goto(cep.entry.url.String(), playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	})
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

	value := []string{string(ft)}
	_, err := cep.page.Locator("select#format-type").SelectOption(playwright.SelectOptionValues{Values: &value})

	if err != nil {
		return fmt.Errorf("Error when selection option %s: %w", ft, err)
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
	dl, err := cep.page.ExpectDownload(func() error {
		return cep.page.Locator(`.download-button + a`).Click()
	}, playwright.PageExpectDownloadOptions{
		Timeout: &timeoutMs,
	})

	if err != nil {
		return fmt.Errorf("Could not start download: %w", err)
	}

	// Download the file and save using the browser suggested name
	path := filepath.Join(outputDir, dl.SuggestedFilename())
	err = dl.SaveAs(path)

	if err != nil {
		return fmt.Errorf("Could not download file: %w", err)
	}

	return nil
}

func (cp CollectionEntryPage) Close() error {
	return cp.page.Close()
}
