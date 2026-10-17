package app

import (
	"context"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

func (c *Core) RecordFailure(op string, err error, job string) domain.ErrorRecord {
	e, ok := err.(*domain.Error)
	if !ok {
		e = &domain.Error{Code: "INTERNAL_ERROR", Message: "操作未完成；底層內容已遮蔽"}
	}
	if e.Message == "" {
		e.Message = "作業失敗，請查看詳細紀錄"
	}
	if e.ErrorID != "" {
		for _, r := range c.errors.List() {
			if r.ID == e.ErrorID {
				return r
			}
		}
	}
	r := c.errors.Add(e, op, job)
	e.ErrorID = r.ID
	seq := c.sequence.Add(1)
	if c.Emit != nil {
		c.Emit("error:recorded", Event{EventID: domain.ID(), Sequence: seq, OccurredAt: time.Now().UTC(), Error: &r})
	}
	return r
}
func (c *Core) ListErrors() domain.Result[[]domain.ErrorRecord] {
	return domain.Success(c.errors.List())
}
func (c *Core) DeleteError(id string) domain.Result[bool] { return c.changed(c.errors.Delete(id)) }
func (c *Core) ReportClientError(code string) domain.Result[domain.ErrorRecord] {
	message := "前端操作未完成"
	if code == "IPC_DISCONNECTED" {
		message = "與應用程式連線中斷"
	} else if code == "COPY_FAILED" {
		message = "無法複製遮敏詳情"
	} else if code == "BROWSER_OPEN_FAILED" {
		message = "無法開啟系統瀏覽器，授權仍可離線閱讀"
	} else {
		code = "UI_OPERATION_FAILED"
	}
	return domain.Success(c.RecordFailure("前端連線", &domain.Error{Source: "frontend", Code: code, Message: message}, ""))
}

func (c *Core) ResolveDomainImport(id string, line int, replace bool) domain.Result[domain.ImportPreview] {
	c.gate.Lock()
	defer c.gate.Unlock()
	p, ok := c.imports[id]
	if !ok {
		return domain.Failure[domain.ImportPreview](domain.Fail("PREVIEW_EXPIRED", "匯入預覽已失效"))
	}
	for index, r := range p.Rows {
		if r.Line == line && (r.State == "conflict" || r.State == "replace" || r.State == "skip") {
			if replace {
				p.Rows[index].State = "replace"
				p.Rows[index].Message = "確認後取代測速網址"
			} else {
				p.Rows[index].State = "skip"
				p.Rows[index].Message = "保留現有網址"
			}
			c.imports[id] = copyOf(p)
			return domain.Success(p)
		}
	}
	return domain.Failure[domain.ImportPreview](domain.Fail("INVALID_INPUT", "找不到待選擇匯入項目"))
}

type folderAPI interface {
	Groups(context.Context, string) ([]domain.Group, error)
	CreateGroup(context.Context, string, string) (domain.Group, error)
}

type folderRuleAPI interface {
	FolderRules(context.Context, string, int) ([]domain.Rule, error)
}

func readFailure(ctx context.Context, err error) error {
	if ctx.Err() == context.Canceled {
		return domain.Fail("CANCELLED", "讀取已取消")
	}
	return err
}

func (c *Core) ListFolders() domain.Result[[]domain.Group] {
	return c.ListFoldersContext(context.Background())
}
func (c *Core) ListFoldersContext(parent context.Context) domain.Result[[]domain.Group] {
	s, _ := c.manager.Snapshot()
	if s.ProfileID == "" {
		return domain.Failure[[]domain.Group](domain.Fail("INVALID_INPUT", "請先選擇 Profile"))
	}
	api, ok := c.api.(folderAPI)
	if !ok {
		return domain.Failure[[]domain.Group](domain.Fail("DEPENDENCY_MISSING", "資料夾功能不可用"))
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	groups, err := api.Groups(ctx, s.ProfileID)
	if err != nil {
		return domain.Failure[[]domain.Group](readFailure(parent, err))
	}
	c.gate.Lock()
	current, _ := c.manager.Snapshot()
	if parent.Err() != nil {
		c.gate.Unlock()
		return domain.Failure[[]domain.Group](readFailure(parent, parent.Err()))
	}
	if current.ProfileID != s.ProfileID || current.Revision != s.Revision {
		c.gate.Unlock()
		return domain.Failure[[]domain.Group](domain.Fail("CONFLICT", "Profile 已變更"))
	}
	c.folders = copyOf(groups)
	c.foldersProfile = s.ProfileID
	c.foldersCredential = s.CredentialRevision
	c.gate.Unlock()
	return domain.Success(groups)
}
func (c *Core) SelectFolder(revision string, id int) domain.Result[bool] {
	return c.SelectFolderContext(context.Background(), revision, id)
}
func (c *Core) SelectFolderContext(ctx context.Context, revision string, id int) domain.Result[bool] {
	s, _ := c.manager.Snapshot()
	c.gate.Lock()
	cached := copyOf(c.folders)
	valid := c.foldersProfile == s.ProfileID && c.foldersCredential == s.CredentialRevision
	c.gate.Unlock()
	r := domain.Success(cached)
	if !valid {
		r = c.ListFoldersContext(ctx)
	}
	if !r.OK {
		return domain.Failure[bool](r.Error)
	}
	if id != 0 {
		found := false
		for _, g := range r.Data {
			if g.ID == id && !g.Action {
				found = true
			}
		}
		if !found {
			return domain.Failure[bool](domain.Fail("INVALID_INPUT", "只能選擇目前的一般資料夾"))
		}
	}
	if ctx.Err() != nil {
		return domain.Failure[bool](readFailure(ctx, ctx.Err()))
	}
	return c.changed(c.manager.SelectFolder(revision, id))
}
func (c *Core) CreateFolder(revision, name string) domain.Result[domain.Group] {
	c.gate.Lock()
	s, _ := c.manager.Snapshot()
	if revision != s.Revision || s.ProfileID == "" || c.testID != "" || c.writerID != "" || c.recovery || c.stopping {
		c.gate.Unlock()
		return domain.Failure[domain.Group](domain.Fail("CONFLICT", "設定已變更或仍有作業；未新增資料夾"))
	}
	api, ok := c.api.(folderAPI)
	if !ok {
		c.gate.Unlock()
		return domain.Failure[domain.Group](domain.Fail("DEPENDENCY_MISSING", "資料夾功能不可用"))
	}
	c.writerID = domain.ID()
	c.writerDone = make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	c.writerCancel = cancel
	c.previews = map[string]domain.SyncPreview{}
	c.gate.Unlock()
	defer c.releaseWriter()
	defer cancel()
	g, err := api.CreateGroup(ctx, s.ProfileID, name)
	if err != nil {
		return domain.Failure[domain.Group](err)
	}
	return domain.Success(g)
}
func (c *Core) ReadRuleList() domain.Result[[]domain.Rule] {
	return c.ReadRuleListContext(context.Background())
}
func (c *Core) ReadRuleListContext(parent context.Context) domain.Result[[]domain.Rule] {
	s, _ := c.manager.Snapshot()
	if s.ProfileID == "" {
		return domain.Failure[[]domain.Rule](domain.Fail("INVALID_INPUT", "請先選擇 Profile"))
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	api, ok := c.api.(folderRuleAPI)
	if !ok {
		return domain.Failure[[]domain.Rule](domain.Fail("DEPENDENCY_MISSING", "服務尚未提供單一資料夾讀取"))
	}
	rules, err := api.FolderRules(ctx, s.ProfileID, s.DestinationFolderID)
	if err != nil {
		return domain.Failure[[]domain.Rule](readFailure(parent, err))
	}
	current, _ := c.manager.Snapshot()
	if parent.Err() != nil {
		return domain.Failure[[]domain.Rule](readFailure(parent, parent.Err()))
	}
	if current.Revision != s.Revision {
		return domain.Failure[[]domain.Rule](domain.Fail("CONFLICT", "讀取期間設定已變更，請重新讀取"))
	}
	out := []domain.Rule{}
	for _, r := range rules {
		if r.Group != s.DestinationFolderID {
			return domain.Failure[[]domain.Rule](domain.Fail("API_FORMAT", "清單含不符目的資料夾的規則"))
		}
		r.Raw = nil
		out = append(out, r)
	}
	return domain.Success(out)
}

// 刪除只限測試結果與失去引用的參考快取，從不碰設定、規則或復原日誌。
func (c *Core) DeleteTestResults(id string) domain.Result[bool] {
	c.gate.Lock()
	defer c.gate.Unlock()
	if c.testID != "" || c.writerID != "" || c.recovery || c.stopping {
		return domain.Failure[bool](domain.Fail("CONFLICT", "作業或復原核對期間不能刪除結果"))
	}
	keys := []string{}
	for key, r := range c.results {
		if id == "" || r.ID == id {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 && id != "" {
		return domain.Failure[bool](domain.Fail("NOT_FOUND", "結果已不存在"))
	}
	c.previews = map[string]domain.SyncPreview{}
	for _, key := range keys {
		var value domain.TestResult
		if err := c.cache.Remove("tests/"+key+".json", &value); err != nil {
			return domain.Failure[bool](domain.Fail("STORAGE_ERROR", "部分結果未刪除；未知資料保留"))
		}
		delete(c.results, key)
	}
	used := map[string]bool{}
	for _, r := range c.results {
		if r.Reference != nil {
			used[r.Reference.ID] = true
		}
	}
	paths, err := c.cache.List("references")
	if err != nil {
		return domain.Failure[bool](domain.Fail("STORAGE_ERROR", "參考快取清理未完成"))
	}
	for _, p := range paths {
		var ref domain.SharedReference
		if c.cache.Read(p, &ref) == nil && !used[ref.ID] {
			if err := c.cache.Remove(p, &ref); err != nil {
				return domain.Failure[bool](domain.Fail("STORAGE_ERROR", "無引用參考未完全清除"))
			}
		}
	}
	return domain.Success(true)
}
