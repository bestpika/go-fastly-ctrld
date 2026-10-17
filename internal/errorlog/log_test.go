package errorlog

import (
	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/storage"
	"testing"
)

func TestPersistentErrorsDeduplicateRotateAndClear(t *testing.T) {
	s, _ := storage.Open(t.TempDir())
	l := New(s)
	e := &domain.Error{Code: "API_REJECTED", Message: "讀規則失敗", Source: "controld", HTTPStatus: 400}
	one := l.Add(e, "GET 規則", "")
	_ = l.Add(&domain.Error{Code: "OTHER", Message: "另一錯誤"}, "其他步驟", "")
	two := l.Add(e, "GET 規則", "")
	if one.ID != two.ID || two.Count != 2 {
		t.Fatal("重複未合併")
	}
	reopened := New(s)
	if len(reopened.List()) != 2 || reopened.List()[0].Count != 2 {
		t.Fatal("重開未保存")
	}
	if err := reopened.Delete(one.ID); err != nil || len(reopened.List()) != 1 {
		t.Fatal("刪除失敗")
	}
	if err := reopened.Delete("missing"); err == nil {
		t.Fatal("未知紀錄未報不存在")
	}
}
