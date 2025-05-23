package internal

import (
	"bufio"
	"crypto/md5"
	"fmt"
	"io"
	"os"
)

type historyFile struct {
	file *os.File
}

type History struct {
	file  *historyFile
	items map[string]bool
}

func newHistoryFile(path string) (*historyFile, error) {

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o600)

	if err != nil {
		return nil, err
	}

	return &historyFile{file: file}, nil
}

func (h *historyFile) readHistory() *History {
	s := bufio.NewScanner(h.file)
	items := make(map[string]bool)

	for s.Scan() {
		text := s.Text()
		items[text] = true
	}

	return &History{items: items, file: h}
}

func NewHistory(path string) (*History, error) {
	h, err := newHistoryFile(path)

	if err != nil {
		return nil, fmt.Errorf("failed to open History file %v", err)
	}

	return h.readHistory(), nil
}

func (h *History) containsDownload(title string, ft FileType) bool {
	hash := md5Download(title, ft)

	_, ok := h.items[hash]

	return ok
}

func md5Download(title string, ft FileType) string {
	h := md5.New()
	_, err := io.WriteString(h, title)

	// this should not happen
	if err != nil {
		panic(err)
	}
	_, err = io.WriteString(h, string(ft))

	if err != nil {
		panic(err)
	}

	return string(h.Sum(nil))
}

func (h *History) addItem(title string, ft FileType) {
	hash := md5Download(title, ft)
	h.items[hash] = true
}

func (h *History) writeOut() {
	w := bufio.NewWriter(h.file.file)

	defer func(w *bufio.Writer) {
		err := w.Flush()
		if err != nil {
			fmt.Printf("failed to flush out %v", err)
		}
	}(w)

	for key := range h.items {
		_, err := w.WriteString(fmt.Sprintf("%s\n", key))
		if err != nil {
			panic(err)
		}
	}
}
