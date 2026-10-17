package storage

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

// CleanExpired 只清理可重取的網段與 DNS 快取，保留結果、共享參考與安全底帳。
func (s *Store) CleanExpired(now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := fileLock(filepath.Join(s.Root, ".lock"))
	if err != nil {
		return 0, err
	}
	defer unlock()
	names := []string{"fastly.json"}
	for _, dir := range []string{"detections"} {
		p, err := s.path(dir + "/entry")
		if err != nil {
			return 0, err
		}
		entries, err := os.ReadDir(filepath.Dir(p))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return 0, err
		}
		for _, e := range entries {
			if !e.IsDir() && e.Type()&os.ModeSymlink == 0 && strings.HasSuffix(e.Name(), ".json") {
				names = append(names, dir+"/"+e.Name())
			}
		}
	}
	removed := 0
	for _, name := range names {
		p, err := s.path(name)
		if err != nil {
			return removed, err
		}
		body, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return removed, err
		}
		var e envelope
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if len(body) > 32<<20 || decoder.Decode(&e) != nil || e.Version != Version || decoder.Decode(new(any)) != io.EOF {
			continue
		}
		var value any
		var expires time.Time
		switch {
		case name == "fastly.json":
			value = &domain.RangeSnapshot{}
		case strings.HasPrefix(name, "detections/"):
			value = &domain.DetectionResult{}
		default:
			continue
		}
		decoder = json.NewDecoder(bytes.NewReader(e.Data))
		decoder.DisallowUnknownFields()
		if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
			continue
		}
		switch v := value.(type) {
		case *domain.RangeSnapshot:
			expires = v.ExpiresAt
		case *domain.DetectionResult:
			expires = v.ExpiresAt
		}
		if !expires.IsZero() && !expires.After(now) {
			if err = os.Remove(p); err != nil {
				return removed, err
			}
			removed++
		}
	}
	return removed, nil
}
