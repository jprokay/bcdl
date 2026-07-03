package main

import (
	"bcdl/internal"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

type FileTypeFlag struct {
	value internal.FileType
}

func (ftf *FileTypeFlag) Set(s string) error {
	for _, ft := range internal.AllFileTypes {
		if s == string(ft) {
			ftf.value = ft
			return nil
		}
	}
	return fmt.Errorf("%s is not in list of valid filetypes %s", s, internal.AllFileTypes)
}

func (ftf *FileTypeFlag) String() string {
	return string(ftf.value)
}

func main() {
	//selected, err := tui.Run()

	var username = flag.String("username", "", "Bandcamp username")
	var identity = flag.String("password", "", "Identity")
	var identityFile = flag.String("identity-file", "", "Path to a file containing the Bandcamp identity cookie value")
	var cookiesFile = flag.String("cookies-file", "", "Path to exported Bandcamp cookies JSON or cookies.txt")
	var fileType = FileTypeFlag{
		value: internal.MP3_320,
	}
	var directory = flag.String("outpath", "", "Path to save files")
	var filter = flag.String("filter", "", "Filter criteria")
	var page = flag.Int("page", 0, "Page to scroll to")
	var limit = flag.Int("limit", 0, "Maximum number of albums to download in this run")
	var workers = flag.Int("workers", 1, "Number of parallel download workers")
	var delayMin = flag.Duration("delay-min", 2*time.Second, "Minimum delay before each album download")
	var delayMax = flag.Duration("delay-max", 6*time.Second, "Maximum delay before each album download")
	var headless = flag.Bool("headless", false, "Run browser without a visible window")
	var mode = flag.String("mode", "direct", "Download mode: direct or browser")
	flag.Var(&fileType, "filetype", "File type to download")

	flag.Parse()

	if *identityFile != "" {
		data, err := os.ReadFile(*identityFile)
		if err != nil {
			log.Fatalf("Could not read identity file: %v", err)
		}
		*identity = strings.TrimSpace(string(data))
	}

	user := internal.NewUser(*username, *identity)
	dl, err := internal.DefaultDownloader(user, *directory)

	if err != nil {
		log.Fatalf("Directory not set")
	}

	log.Printf("File type: %s", fileType.value)
	internal.WithFiletype(fileType.value)(dl)
	internal.WithCookiesFile(*cookiesFile)(dl)
	internal.WithLimit(*limit)(dl)
	internal.WithWorkers(*workers)(dl)
	internal.WithDelay(*delayMin, *delayMax)(dl)
	if *headless {
		internal.WithHeadless()(dl)
	}

	opts := internal.DownloadOpts{
		OnStart: func(name string) {
			log.Printf("Beginning download: %s\n", name)
		},
		OnSuccess: func(name string) {
			log.Printf("Successfully downloaded: %s\n", name)
		},
		OnFailure: func(name string) {
			log.Printf("Failed to download: %s\n", name)
		},
		Filter: *filter,
	}

	results := make(chan error)
	go func() {
		switch *mode {
		case "direct":
			if *page != 0 {
				log.Printf("-page is ignored in direct mode")
			}
			results <- dl.DownloadDirect(opts)
		case "browser":
			results <- dl.DownloadPage(*page, opts)
		default:
			results <- fmt.Errorf("unknown mode %q; use direct or browser", *mode)
		}
	}()

	err = <-results

	if err != nil {
		log.Fatalf("Error completing download %v\n", err)
	} else {
		log.Println("Downloads complete!")
		os.Exit(0)
	}
}
