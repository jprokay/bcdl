package internal

import (
	"context"
	"fmt"
	"log"
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
	user     *User
	dirPath  string
	context  context.Context
	timeout  time.Duration
	headless bool
	filetype FileType
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
func worker(id int, jobs chan downloadJob, results chan<- downloadJob, browserCtx AuthorizedBandcampContext) {
	for job := range jobs {
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

// processJob does the heavy lifting of going to the URL for an album and managing the download process.
func processJob(job downloadJob, browserCtx AuthorizedBandcampContext) error {
	page, err := browserCtx.NewCollectionEntryPage(job.Entry)

	if err != nil {
		return fmt.Errorf("Could not create page: %w", err)
	}

	defer page.Close()

	_, err = page.Goto()

	if err != nil {
		return fmt.Errorf("Could not goto %s: %w", job.Entry.url.String(), err)
	}

	// Download the specific format
	err = page.SelectFileType(job.filetype)

	if err != nil {
		return fmt.Errorf("Could not select file type %s: %w", job.filetype, err)
	}

	// Download the page
	var timeout float64 = float64(job.timeout.Milliseconds())

	err = page.DownloadFile(job.DownloadDir, timeout)

	if err != nil {
		return fmt.Errorf("Could not download file: %w", err)
	}

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
	pw, err := playwright.Run()
	if err != nil {
		return fmt.Errorf("could not start playwright: %v", err)
	}
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(d.headless),
	})

	if err != nil {
		return fmt.Errorf("could not launch browser: %v", err)
	}

	context, err := NewAuthorizedBandcampContext(browser, d.user.identity)

	if err != nil {
		return fmt.Errorf("could not create context: %v", err)
	}

	page, err := context.NewCollectionPage(d.user.username)

	if err != nil {
		return fmt.Errorf("could not create page: %v", err)
	}

	// Go to the users collection
	if _, err = page.Goto(); err != nil {
		return fmt.Errorf("could not goto: %v", err)
	}

	err = page.filter(opts.Filter)
	count, err := page.AlbumCount()
	log.Printf("Downloading %v albums", count)
	scrollTimes, err := page.ScrollTimes()
	log.Printf("Need to scroll %v times", scrollTimes)

	// 0. Get first page of entries
	// 1. Enqueue jobs
	// 2. Scroll if there are more
	// 3. Enqueue next set of jobs
	// 4. Ensure no duplicates - should be able to use in memory history
	// 5. continue until done

	for i := range scrollTimes {
		entries, err := page.Entries(opts.Filter)

		if err != nil {
			return fmt.Errorf("Could not get your collection. Err: %v\nCheck that you have the correct identity cookie value", err)
		}

		notDownloaded := make([]CollectionEntry, 0, len(entries))
		// Get the album name and every download link
		for _, entry := range entries {

			// Skip any previously downloaded files
			if history.containsDownload(entry.title, d.filetype) {
				log.Printf("Already downloaded %s. Skipping", entry.title)
				continue
			}

			notDownloaded = append(notDownloaded, entry)

		}
		// Set up jobs
		jobs := make(chan downloadJob, len(notDownloaded))
		results := make(chan downloadJob, len(notDownloaded))

		// Limit jobs to 3. This seems to be the sweet spot
		for w := 0; w < 3; w++ {
			go worker(w, jobs, results, context)
		}

		for _, entry := range notDownloaded {
			opts.OnStart(entry.title)
			// Enqueue those jobs
			jobs <- downloadJob{
				Entry:       entry,
				DownloadDir: outDir,
				filetype:    d.filetype,
				timeout:     time.Duration(time.Minute * 4),
			}

		}

		for range notDownloaded {
			job := <-results
			if job.Success {
				history.addItem(job.Entry.title, d.filetype)
				opts.OnSuccess(job.Entry.title)
			} else {
				log.Printf("Error: %v", job.err)
				opts.OnFailure(job.Entry.title)
			}
		}

		close(jobs)
		close(results)
		log.Printf("%d/%d completed. Scrolling.", i, scrollTimes)
		history.writeOut()
		page.ScrollPage()
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
