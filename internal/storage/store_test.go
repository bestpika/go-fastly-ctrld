package storage

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAtomicFailurePreservesData(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	v := map[string]string{"value": "原值"}
	if err = s.Write("settings.json", v); err != nil {
		t.Fatal(err)
	}
	s.BeforeReplace = func() error { return errors.New("模擬磁碟故障") }
	if s.Write("settings.json", map[string]string{"value": "新值"}) == nil {
		t.Fatal("應回報故障")
	}
	var read map[string]string
	if err = s.Read("settings.json", &read); err != nil || read["value"] != "原值" {
		t.Fatalf("原資料遭破壞：%v", err)
	}
}

func TestUnknownSchemaAndTraversal(t *testing.T) {
	s, _ := Open(t.TempDir())
	if err := os.WriteFile(filepath.Join(s.Root, "bad.json"), []byte(`{"version":999,"data":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	var v map[string]string
	if !errors.Is(s.Read("bad.json", &v), ErrSchema) {
		t.Fatal("應拒絕未知版本")
	}
	for _, name := range []string{"../x", "/x", "x\\y", "x/../../y", "x:y"} {
		if s.Write(name, v) == nil {
			t.Fatalf("未拒絕路徑：%s", name)
		}
	}
}

func TestConcurrentPersistence(t *testing.T) {
	s, _ := Open(t.TempDir())
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if err := s.Write("jobs/job.json", map[string]int{"value": n}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	var v map[string]int
	if err := s.Read("jobs/job.json", &v); err != nil {
		t.Fatal(err)
	}
	names, err := s.List("jobs")
	if err != nil || len(names) != 1 {
		t.Fatalf("清單不正確：%v %v", names, err)
	}
}
