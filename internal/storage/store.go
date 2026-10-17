package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
)

const Version = 1

var ErrMissing = errors.New("資料不存在")
var ErrSchema = errors.New("資料版本未知或損毀，未覆寫原檔")

type envelope struct {
	Version int             `json:"version"`
	Data    json.RawMessage `json:"data"`
}

type Store struct {
	Root          string
	mu            sync.Mutex
	BeforeReplace func() error
}

func Open(root string) (*Store, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("資料目錄必須是絕對路徑")
	}
	if info, err := os.Lstat(root); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("資料目錄不能是符號連結")
	}
	if err := privateDir(root); err != nil {
		return nil, err
	}
	return &Store{Root: root}, nil
}

func (s *Store) path(name string) (string, error) {
	if name == "" || filepath.IsAbs(name) || strings.Contains(name, "\\") {
		return "", fmt.Errorf("非法資料名稱")
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, ":\x00") {
			return "", fmt.Errorf("非法資料名稱")
		}
	}
	p := filepath.Join(s.Root, filepath.FromSlash(name))
	for d := filepath.Dir(p); ; d = filepath.Dir(d) {
		info, err := os.Lstat(d)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("資料路徑不能包含符號連結")
		}
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if d == s.Root {
			break
		}
	}
	if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("資料檔不能是符號連結")
	}
	return p, nil
}

func (s *Store) Read(name string, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.path(name)
	if err != nil {
		return err
	}
	unlock, err := fileLock(filepath.Join(s.Root, ".lock"))
	if err != nil {
		return err
	}
	defer unlock()
	f, err := os.Open(p)
	if os.IsNotExist(err) {
		return ErrMissing
	}
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 32<<20+1))
	if err != nil {
		return err
	}
	if len(b) > 32<<20 {
		return ErrSchema
	}
	var e envelope
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&e) != nil || e.Version != Version || len(e.Data) == 0 || d.Decode(new(any)) != io.EOF {
		return ErrSchema
	}
	d = json.NewDecoder(bytes.NewReader(e.Data))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		return ErrSchema
	}
	return nil
}

func (s *Store) Write(name string, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.path(name)
	if err != nil {
		return err
	}
	unlock, err := fileLock(filepath.Join(s.Root, ".lock"))
	if err != nil {
		return err
	}
	defer unlock()
	if existing, readErr := os.ReadFile(p); readErr == nil {
		var e envelope
		d := json.NewDecoder(bytes.NewReader(existing))
		d.DisallowUnknownFields()
		if len(existing) > 32<<20 || d.Decode(&e) != nil || e.Version != Version || d.Decode(new(any)) != io.EOF || v == nil {
			return ErrSchema
		}
		d = json.NewDecoder(bytes.NewReader(e.Data))
		d.DisallowUnknownFields()
		if d.Decode(reflect.New(reflect.TypeOf(v)).Interface()) != nil || d.Decode(new(any)) != io.EOF {
			return ErrSchema
		}
	} else if !os.IsNotExist(readErr) {
		return readErr
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(envelope{Version: Version, Data: data}, "", "  ")
	if err != nil {
		return err
	}
	if len(b) > 32<<20 {
		return fmt.Errorf("資料超出保存上限")
	}
	if err = privateDir(filepath.Dir(p)); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if s.BeforeReplace != nil {
		if err = s.BeforeReplace(); err != nil {
			return err
		}
	}
	return replace(f.Name(), p)
}

func (s *Store) List(dir string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.path(dir + "/entry")
	if err != nil {
		return nil, err
	}
	es, err := os.ReadDir(filepath.Dir(p))
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := []string{}
	for _, e := range es {
		if !e.IsDir() && e.Type()&os.ModeSymlink == 0 && strings.HasSuffix(e.Name(), ".json") {
			result = append(result, dir+"/"+e.Name())
		}
	}
	return result, nil
}

// 只刪除已通過同一型別與版本驗證的單一資料檔，不接受任意路徑清除。
func (s *Store) Remove(name string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.path(name)
	if err != nil {
		return err
	}
	unlock, err := fileLock(filepath.Join(s.Root, ".lock"))
	if err != nil {
		return err
	}
	defer unlock()
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(b) > 32<<20 {
		return ErrSchema
	}
	var e envelope
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&e) != nil || e.Version != Version || d.Decode(new(any)) != io.EOF {
		return ErrSchema
	}
	d = json.NewDecoder(bytes.NewReader(e.Data))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || d.Decode(new(any)) != io.EOF {
		return ErrSchema
	}
	return os.Remove(p)
}
