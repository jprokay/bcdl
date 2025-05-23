package main

import (
	"bcdl/internal"
	"flag"
	"fmt"
	"log"
	"os"
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
	var fileType = FileTypeFlag{
		value: internal.MP3_320,
	}
	var directory = flag.String("outpath", "", "Path to save files")
	var filter = flag.String("filter", "", "Filter criteria")
	flag.Var(&fileType, "filetype", "File type to download")

	flag.Parse()

	log.Printf(fileType.String())
	user := internal.NewUser(*username, *identity)
	dl, err := internal.DefaultDownloader(user, *directory)

	if err != nil {
		log.Fatalf("Directory not set")
	}

	log.Printf("File type: %s", fileType.value)
	internal.WithFiletype(fileType.value)(dl)

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
