package internal

import (
	"context"
	"fmt"
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
func NewDownloader(user *User, dirPath string, options ...func(*Downloader)) *Downloader {
	dl := &Downloader{user: user, dirPath: dirPath}

	for _, f := range options {
		f(dl)
	}
	return dl
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
func DefaultDownloader(user *User, dirPath string) *Downloader {
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
	timeoutMs   float64
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

// workers will pull jobs off of the jobs channel and send the results to the results channel.
func worker(id int, jobs <-chan downloadJob, results chan<- downloadJob, browserCtx AuthorizedBandcampContext) {
	for job := range jobs {
		jobCtx, cancel := context.WithTimeout(context.Background(), time.Minute*4)
		jobErr := make(chan error, 1)
		go func() {
			jobErr <- processJob(job, browserCtx)
			cancel()
		}()

		select {
		case <-jobCtx.Done():
			job.failed(fmt.Errorf("%s timed out", job.Entry.title))
			results <- job
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

	defer page.Close()

	if err != nil {
		return fmt.Errorf("Could not create page: %w", err)
	}

	_, err = page.Goto()

	if err != nil {
		return fmt.Errorf("Could not goto %s: %w", job.Entry.url.String(), err)
	}

	// Download the specific format
	page.SelectFileType(job.filetype)

	// Download the page
	var timeout float64 = job.timeoutMs

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
	wd := d.dirPath
	outDir := filepath.Join(wd, "out")
	bcdlDir := filepath.Join(wd, "out", "bcdl")

	// Downloads will go here
	if err := os.Mkdir(outDir, 0o777); err != nil && !os.IsExist(err) {
		return fmt.Errorf("Could not create output dir %v", err)
	}

	// Track download history to avoid repeats
	if err := os.Mkdir(bcdlDir, 0o777); err != nil && !os.IsExist(err) {
		return fmt.Errorf("Could not create output dir %v", err)
	}

	// Create an append only file
	file, err := os.OpenFile(filepath.Join(wd, "out", ".bcdl", "downloaded"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)

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
	page, err := context.NewCollectionPage(d.user.username)

	if err != nil {
		return fmt.Errorf("could not create page: %v", err)
	}

	// Go to the users collection
	if _, err = page.Goto(); err != nil {
		return fmt.Errorf("could not goto: %v", err)
	}

	// Get all entries in the collection
	entries, err := page.GetCollection(opts.Filter)

	if err != nil {
		return fmt.Errorf("Could not get your collection. Check that you have the correct identity cookie value")
	}

	// Set up jobs
	jobs := make(chan downloadJob, len(entries))
	results := make(chan downloadJob, len(entries))

	// Limit jobs to 3. This seems to be the sweet spot
	for w := 0; w < 3; w++ {
		go worker(w, jobs, results, context)
	}

	// Shuffle things up
	rand.Shuffle(len(entries), func(i, j int) {
		entries[i], entries[j] = entries[j], entries[i]
	})

	// Get the album name and every download link
	for _, entry := range entries {
		opts.OnStart(entry.title)
		// Enqueue those jobs
		jobs <- downloadJob{
			Entry:       entry,
			DownloadDir: outDir,
			filetype:    d.filetype,
			timeoutMs:   240_000,
		}
	}

	for i := 0; i < len(entries); i++ {
		job := <-results
		if job.Success {
			// Write to the history file
			file.WriteString(fmt.Sprintf("%s\n", job.Entry.title))
			opts.OnSuccess(job.Entry.title)
		} else {
			opts.OnFailure(job.Entry.title)
		}
	}

	close(jobs)
	close(results)

	if err = browser.Close(); err != nil {
		return fmt.Errorf("could not close browser: %v", err)
	}
	if err = pw.Stop(); err != nil {
		return fmt.Errorf("could not stop Playwright: %v", err)
	}

	return nil
}
