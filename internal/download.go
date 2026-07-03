package internal

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/playwright-community/playwright-go"
)

// User represents a person who uses Bandcamp and their Identity cookie
type User struct {
	identity string
	username string
}

// Downloader represents all the options needed to successfully download the collection
// for users
type Downloader struct {
	user        *User
	dirPath     string
	context     context.Context
	timeout     time.Duration
	headless    bool
	filetype    FileType
	cookiesFile string
	workers     int
	limit       int
	delayMin    time.Duration
	delayMax    time.Duration
}

// NewUser creates a User from the provided username and identity parameters.
func NewUser(username, identity string) *User {
	u := &User{username: username, identity: identity}

	return u
}

// NewDownloader creates a new Download object using the specified options.
func NewDownloader(user *User, dirPath string, options ...func(*Downloader)) (*Downloader, error) {
	if dirPath == "" {
		return nil, fmt.Errorf("Directory path cannot be empty")
	}

	dl := &Downloader{user: user, dirPath: dirPath}

	for _, f := range options {
		f(dl)
	}
	return dl, nil
}

// WithContext sets the context for the downloader.
func WithContext(ctx context.Context) func(*Downloader) {
	return func(d *Downloader) {
		d.context = ctx
	}
}

// WithTimeout sets the starting timeout for each job.
func WithTimeout(timeout time.Duration) func(*Downloader) {
	return func(d *Downloader) {
		d.timeout = timeout
	}
}

// WithHeadless sets whether or not to use a Headless browser.
// Very useful for debugging.
func WithHeadless() func(*Downloader) {
	return func(d *Downloader) {
		d.headless = true
	}
}

// WithFiletype sets the filetype to use for all downloads.
func WithFiletype(filetype FileType) func(*Downloader) {
	return func(d *Downloader) {
		d.filetype = filetype
	}
}

func WithCookiesFile(path string) func(*Downloader) {
	return func(d *Downloader) {
		d.cookiesFile = path
	}
}

func WithWorkers(workers int) func(*Downloader) {
	return func(d *Downloader) {
		if workers > 0 {
			d.workers = workers
		}
	}
}

func WithLimit(limit int) func(*Downloader) {
	return func(d *Downloader) {
		d.limit = limit
	}
}

func WithDelay(minDelay, maxDelay time.Duration) func(*Downloader) {
	return func(d *Downloader) {
		if minDelay >= 0 {
			d.delayMin = minDelay
		}
		if maxDelay >= minDelay {
			d.delayMax = maxDelay
		}
	}
}

// DefaultDownloader creates a Downloader with sensible defaults.
//
// Defaults:
//   - context: Background
//   - timeout: 3 minutes
//   - filetype: MP3_320
func DefaultDownloader(user *User, dirPath string) (*Downloader, error) {
	return NewDownloader(user, dirPath,
		WithContext(context.Background()),
		WithTimeout(3*time.Minute),
		WithFiletype(MP3_320),
		WithWorkers(1),
		WithDelay(2*time.Second, 6*time.Second),
	)
}

// downloadJob is used for processing a download request
type downloadJob struct {
	Entry       CollectionEntry
	err         error
	Success     bool
	DownloadDir string
	filetype    FileType
	timeout     time.Duration
	retries     int8
	logger      *ContextLogger
}

// failed marks the job as failed and sets the error
func (j *downloadJob) failed(err error) {
	j.Success = false
	j.err = err
}

// succeeded marks the job as successful
func (j *downloadJob) succeeded() {
	j.Success = true
	j.err = nil
}

const MAX_RETRIES = 5

func (j *downloadJob) timedOut() (err error) {
	if j.retries == MAX_RETRIES {
		return fmt.Errorf("Reached maximum allowed retries")
	}
	j.Success = false
	j.err = fmt.Errorf("Timed out after %f minutes", j.timeout.Minutes())
	j.retries++
	// Instead of an exponential backoff, add two minutes each time we retry
	j.timeout += time.Duration(2 * time.Minute)
	return nil
}

// workers will pull jobs off of the jobs channel and send the results to the results channel.
func worker(id int, jobs chan downloadJob, results chan<- downloadJob, browserCtx AuthorizedBandcampContext, delayMin time.Duration, delayMax time.Duration) {
	for job := range jobs {
		sleepBeforeRequest(delayMin, delayMax)
		log.Printf("Starting job: %s", job.Entry.title)
		jobCtx, cancel := context.WithTimeout(context.Background(), job.timeout)
		jobErr := make(chan error, 1)
		go func() {
			jobErr <- processJob(job, browserCtx)
			cancel()
		}()

		select {
		case <-jobCtx.Done():
			err := job.timedOut()

			if err != nil {
				// Max retries. Fail the job
				job.failed(err)
				results <- job
			} else {
				// Push it back into the queue for processing
				jobs <- job
			}
		case err := <-jobErr:
			if err != nil {
				job.failed(err)
				results <- job
			} else {
				job.succeeded()
				results <- job
			}
		}
	}
}

func sleepBeforeRequest(delayMin time.Duration, delayMax time.Duration) {
	if delayMax <= 0 {
		return
	}

	delay := delayMin
	if delayMax > delayMin {
		delay += time.Duration(rand.Int63n(int64(delayMax - delayMin)))
	}

	time.Sleep(delay)
}

// processJob does the heavy lifting of going to the URL for an album and managing the download process.
func processJob(job downloadJob, browserCtx AuthorizedBandcampContext) error {
	if job.logger == nil {
		job.logger = NewContextLogger(fmt.Sprintf("Album: %s", job.Entry.title))
	}
	job.logger.Printf("Starting download process")

	page, err := browserCtx.NewCollectionEntryPage(job.Entry)

	if err != nil {
		job.logger.Printf("Failed to create page: %v", err)
		return fmt.Errorf("Could not create page: %w", err)
	}

	defer page.Close()

	_, err = page.Goto()

	if err != nil {
		job.logger.Printf("Failed to navigate to album page: %v", err)
		return fmt.Errorf("Could not goto %s: %w", job.Entry.url.String(), err)
	}

	job.logger.Printf("Successfully loaded album page")

	mustReauth, err := page.MustReauth()

	if mustReauth == true || err != nil {
		if err != nil {
			return fmt.Errorf("could not check reauth state: %w", err)
		}
		if err := page.WaitForManualReauth(2 * time.Minute); err != nil {
			return fmt.Errorf("must reauth before download can continue: %w", err)
		}
	}
	// Download the specific format
	err = page.SelectFileType(job.filetype)

	if err != nil {
		return fmt.Errorf("Could not select file type %s: %w", job.filetype, err)
	}

	// Download the page
	var timeout float64 = float64(job.timeout.Milliseconds())
	job.logger.Printf("Initiating download with timeout of %.1f seconds", timeout/1000)

	err = page.DownloadFile(job.DownloadDir, timeout)

	if err != nil {
		return fmt.Errorf("Could not download file: %w", err)
	}

	job.logger.Printf("Download completed successfully")
	return nil
}

type fileFunc func(name string)

// DownloadOpts provides a list of callbacks and a Filter value to track
// the status of the download process.
type DownloadOpts struct {
	OnStart   fileFunc
	OnSuccess fileFunc
	OnFailure fileFunc
	Filter    string
}

// Download is the workhorse responsible for saving all of the albums in the collection
// to a directory on local the machine.
//
// In addition to the zip files, the method creates a hidden .bcdl folder to track
// files to make the tool more useful.
func (d *Downloader) Download(opts DownloadOpts) error {
	outDir := d.dirPath
	bcdlDir := filepath.Join(outDir, ".bcdl")

	// Downloads will go here
	if err := os.Mkdir(outDir, 0o777); err != nil && !os.IsExist(err) {
		return fmt.Errorf("Could not create output dir %v", err)
	}

	// Track download history to avoid repeats
	if err := os.Mkdir(bcdlDir, 0o777); err != nil && !os.IsExist(err) {
		return fmt.Errorf("Could not create output dir %v", err)
	}

	history, err := NewHistory(filepath.Join(bcdlDir, "downloaded"))

	if err != nil {
		return fmt.Errorf("Failure to get history file %v", err)
	}

	// Install browsers & run
	err = playwright.Install()
	if err != nil {
		return fmt.Errorf("Could not install playwright: %v", err)
	}
	log.Printf("Starting Playwright")
	pw, err := playwright.Run()
	if err != nil {
		return fmt.Errorf("could not start playwright: %v", err)
	}
	log.Printf("Launching Chrome")
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(d.headless),
		Channel:  playwright.String("chrome"),
	})

	if err != nil {
		return fmt.Errorf("could not launch browser: %v", err)
	}
	log.Printf("Creating Bandcamp browser context")

	context, err := NewAuthorizedBandcampContext(browser, d.user.identity, d.cookiesFile)

	if err != nil {
		return fmt.Errorf("could not create context: %v", err)
	}
	log.Printf("Creating collection page")

	page, err := context.NewCollectionPage(d.user.username)

	if err != nil {
		return fmt.Errorf("could not create page: %v", err)
	}

	// Go to the users collection
	if _, err = page.Goto(); err != nil {
		return fmt.Errorf("could not goto: %v", err)
	}

	cookiesVisible, err := page.AcceptCookiesModal().IsVisible()
	if err != nil {
		log.Printf("Could not check cookies modal: %v", err)
	}

	if cookiesVisible {
		page.AcceptCookiesModal().Click(playwright.LocatorClickOptions{})
	}

	err = page.filter(opts.Filter)
	if err != nil {
		return fmt.Errorf("could not filter collection: %w", err)
	}
	count, err := page.AlbumCount()
	if err != nil {
		return fmt.Errorf("could not determine album count: %w", err)
	}
	log.Printf("Downloading %v albums", count)
	scrollTimes, err := page.ScrollTimes()
	if err != nil {
		return fmt.Errorf("could not determine scroll count: %w", err)
	}
	log.Printf("Need to scroll %v times", scrollTimes)

	// 0. Get first page of entries
	// 1. Enqueue jobs
	// 2. Scroll if there are more
	// 3. Enqueue next set of jobs
	// 4. Ensure no duplicates - should be able to use in memory history
	// 5. continue until done

	for i := range scrollTimes {
		if err := page.DownloadEntries(i, outDir, history, d.filetype, context, d.workers, d.limit, d.delayMin, d.delayMax, opts); err != nil {
			return err
		}
		page.page.Reload()
	}

	history.writeOut()

	if err = browser.Close(); err != nil {
		return fmt.Errorf("could not close browser: %v", err)
	}
	if err = pw.Stop(); err != nil {
		return fmt.Errorf("could not stop Playwright: %v", err)
	}

	return nil
}

func (d *Downloader) DownloadPage(scrollTo int, opts DownloadOpts) error {
	outDir := d.dirPath
	bcdlDir := filepath.Join(outDir, ".bcdl")

	// Downloads will go here
	if err := os.Mkdir(outDir, 0o777); err != nil && !os.IsExist(err) {
		return fmt.Errorf("Could not create output dir %v", err)
	}

	// Track download history to avoid repeats
	if err := os.Mkdir(bcdlDir, 0o777); err != nil && !os.IsExist(err) {
		return fmt.Errorf("Could not create output dir %v", err)
	}

	history, err := NewHistory(filepath.Join(bcdlDir, "downloaded"))

	if err != nil {
		return fmt.Errorf("Failure to get history file %v", err)
	}

	// Install browsers & run
	err = playwright.Install()
	if err != nil {
		return fmt.Errorf("Could not install playwright: %v", err)
	}
	log.Printf("Starting Playwright")
	pw, err := playwright.Run()
	if err != nil {
		return fmt.Errorf("could not start playwright: %v", err)
	}
	log.Printf("Launching Chrome")
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(d.headless),
		Channel:  playwright.String("chrome"),
	})

	if err != nil {
		return fmt.Errorf("could not launch browser: %v", err)
	}
	log.Printf("Creating Bandcamp browser context")

	context, err := NewAuthorizedBandcampContext(browser, d.user.identity, d.cookiesFile)

	if err != nil {
		return fmt.Errorf("could not create context: %v", err)
	}
	log.Printf("Creating collection page")

	page, err := context.NewCollectionPage(d.user.username)

	if err != nil {
		return fmt.Errorf("could not create page: %v", err)
	}

	// Go to the users collection
	if _, err = page.Goto(); err != nil {
		return fmt.Errorf("could not goto: %v", err)
	}

	cookiesVisible, err := page.AcceptCookiesModal().IsVisible()
	if err != nil {
		log.Printf("Could not check cookies modal: %v", err)
	}

	if cookiesVisible {
		page.AcceptCookiesModal().Click(playwright.LocatorClickOptions{})
	}

	err = page.filter(opts.Filter)
	if err != nil {
		return fmt.Errorf("could not filter collection: %w", err)
	}
	scrollTimes, err := page.ScrollTimes()
	if err != nil {
		return fmt.Errorf("could not determine scroll count: %w", err)
	}

	if scrollTimes < scrollTo || scrollTo < 0 {
		return fmt.Errorf("%d is outside range of pages [0,%d]", scrollTo, scrollTimes)
	}
	// 0. Get first page of entries
	// 1. Enqueue jobs
	// 2. Scroll if there are more
	// 3. Enqueue next set of jobs
	// 4. Ensure no duplicates - should be able to use in memory history
	// 5. continue until done

	if err := page.DownloadEntries(scrollTo, outDir, history, d.filetype, context, d.workers, d.limit, d.delayMin, d.delayMax, opts); err != nil {
		return err
	}

	history.writeOut()

	if err = browser.Close(); err != nil {
		return fmt.Errorf("could not close browser: %v", err)
	}
	if err = pw.Stop(); err != nil {
		return fmt.Errorf("could not stop Playwright: %v", err)
	}

	return nil
}
