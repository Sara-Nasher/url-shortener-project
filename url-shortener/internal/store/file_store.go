package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type fileRecord struct {
	URL       string    `json:"url"`
	Code      string    `json:"code"`
	CreatedAt time.Time `json:"created_at"`
}

type FileStore struct {
	mu    sync.RWMutex
	file  *os.File
	urls  map[string]string
	links map[string]Link
}

func NewFile(path string) (*FileStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	s := &FileStore{
		file:  file,
		urls:  make(map[string]string),
		links: make(map[string]Link),
	}

	if err := s.load(); err != nil {
		_ = file.Close()
		return nil, err
	}

	return s, nil
}

func (s *FileStore) load() error {
	data, err := os.ReadFile(s.file.Name())
	if err != nil {
		return err
	}

	if len(data) == 0 {
		return nil
	}

	lines := splitLines(data)
	lineStart := int64(0)
	lastLineHasNewline := data[len(data)-1] == '\n'

	for i, line := range lines {
		currentLineStart := lineStart
		lineStart += int64(len(line)) + 1

		if len(line) == 0 {
			continue
		}

		var record fileRecord

		if err := json.Unmarshal(line, &record); err != nil {

			if i == len(lines)-1 && !lastLineHasNewline {

				path := s.file.Name()

				if err := s.file.Close(); err != nil {
					return err
				}

				if err := os.Truncate(path, currentLineStart); err != nil {
					return err
				}

				file, err := os.OpenFile(
					path,
					os.O_CREATE|os.O_RDWR|os.O_APPEND,
					0644,
				)
				if err != nil {
					return err
				}

				s.file = file

				if err := s.file.Sync(); err != nil {
					return err
				}

				return nil
			}
			return err
		}

		if record.URL == "" || record.Code == "" {
			return errors.New("invalid record in store file")
		}

		if _, exists := s.urls[record.URL]; exists {
			return errors.New("duplicate url in store file")
		}

		if _, exists := s.links[record.Code]; exists {
			return errors.New("duplicate code in store file")
		}

		s.urls[record.URL] = record.Code
		s.links[record.Code] = Link{
			URL:       record.URL,
			CreatedAt: record.CreatedAt,
		}
	}

	return nil
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0

	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}

	if start < len(data) {
		lines = append(lines, data[start:])
	}

	return lines
}

func (s *FileStore) GetCode(url string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	code, ok := s.urls[url]
	return code, ok
}

func (s *FileStore) GetURL(code string) (Link, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	link, ok := s.links[code]
	if !ok {
		return Link{}, ErrNotFound
	}

	return link, nil
}

func (s *FileStore) Save(url, code string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.urls[url]; exists {
		return false
	}

	if _, exists := s.links[code]; exists {
		return false
	}

	record := fileRecord{
		URL:       url,
		Code:      code,
		CreatedAt: time.Now().UTC(),
	}

	data, err := json.Marshal(record)
	if err != nil {
		return false
	}

	data = append(data, '\n')

	info, err := s.file.Stat()
	if err != nil {
		return false
	}

	oldSize := info.Size()

	n, err := s.file.Write(data)
	if err != nil || n != len(data) {
		_ = s.file.Truncate(oldSize)
		_, _ = s.file.Seek(0, os.SEEK_END)
		return false
	}

	if err := s.file.Sync(); err != nil {
		_ = s.file.Truncate(oldSize)
		_, _ = s.file.Seek(0, os.SEEK_END)
		return false
	}

	s.urls[url] = code
	s.links[code] = Link{
		URL:       url,
		CreatedAt: record.CreatedAt,
	}

	return true
}

func (s *FileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.file.Close()
}
