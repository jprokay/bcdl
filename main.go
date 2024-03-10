package main

import (
	"bcdl/internal"
	"bcdl/internal/tui"
	"log"
	"os"
)

func main() {
	selected, err := tui.Run()

	if err != nil {
		log.Fatalf("Halting execution %v", err)
		os.Exit(1)
	}

	user := internal.NewUser(selected.Username, selected.Identity)
	dl, err := internal.DefaultDownloader(user, selected.Directory)

	if err != nil {
		log.Fatalf("Directory not set")
		os.Exit(1)
	}

	internal.WithFiletype(selected.FileType)(dl)

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
		Filter: selected.Filter,
	}

	results := make(chan error)
	go func() {
		results <- dl.Download(opts)
	}()

	err = <-results

	if err != nil {
		log.Fatalf("Error completing download %v\n", err)
	} else {
		log.Println("Downloads complete!")
		os.Exit(0)
	}
}
