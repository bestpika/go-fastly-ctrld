package desktop

import (
	"context"
	"github.com/bestpika/go-fastly-ctrld/internal/app"
	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/settings"
)

// 明確白名單，不嵌入 Core、檔案系統或金鑰庫。
type Bridge struct {
	core   *app.Core
	ready  func()
	quit   func()
	export func() error
}

func reply[T any](b *Bridge, op string, r domain.Result[T]) domain.Result[T] {
	if !r.OK && r.Error != nil && r.Error.Code != "CANCELLED" && b.core != nil {
		record := b.core.RecordFailure(op, r.Error, "")
		r.Error.ErrorID = record.ID
	}
	return r
}
func (b *Bridge) GetAppSnapshot() domain.Result[app.Snapshot] {
	return reply(b, "讀取狀態", b.core.GetAppSnapshot())
}
func (b *Bridge) UpdateSettings(revision string, p settings.Patch) domain.Result[bool] {
	return reply(b, "設定", b.core.UpdateSettings(revision, p))
}
func (b *Bridge) SetControlDToken(token string) domain.Result[bool] {
	return reply(b, "保存權杖", b.core.SetControlDToken(token))
}
func (b *Bridge) DeleteControlDToken() domain.Result[bool] {
	return reply(b, "刪除權杖", b.core.DeleteControlDToken())
}
func (b *Bridge) GetCredentialStatus() domain.Result[settings.CredentialStatus] {
	return reply(b, "金鑰庫狀態", b.core.GetCredentialStatus())
}
func (b *Bridge) CheckControlDConnection(ctx context.Context) domain.Result[bool] {
	return reply(b, "測試連線", b.core.CheckControlDConnectionContext(ctx))
}
func (b *Bridge) ListProfiles(ctx context.Context) domain.Result[[]domain.Profile] {
	return reply(b, "設定檔清單", b.core.ListProfilesContext(ctx))
}
func (b *Bridge) SelectProfile(ctx context.Context, revision, id string) domain.Result[bool] {
	return reply(b, "選擇設定檔", b.core.SelectProfileContext(ctx, revision, id))
}
func (b *Bridge) ListFolders(ctx context.Context) domain.Result[[]domain.Group] {
	return reply(b, "資料夾清單", b.core.ListFoldersContext(ctx))
}
func (b *Bridge) SelectFolder(ctx context.Context, revision string, id int) domain.Result[bool] {
	return reply(b, "選擇資料夾", b.core.SelectFolderContext(ctx, revision, id))
}
func (b *Bridge) CreateFolder(revision, name string) domain.Result[domain.Group] {
	return reply(b, "新增一般資料夾", b.core.CreateFolder(revision, name))
}
func (b *Bridge) ReadRuleList(ctx context.Context) domain.Result[[]domain.Rule] {
	return reply(b, "規則清單", b.core.ReadRuleListContext(ctx))
}
func (b *Bridge) SelectImportFile() domain.Result[string] {
	return reply(b, "選擇匯入檔案", b.core.SelectImportFile())
}
func (b *Bridge) PreviewDomainImport(content, kind, fileID string) domain.Result[domain.ImportPreview] {
	return reply(b, "匯入預覽", b.core.PreviewDomainImport(content, kind, fileID))
}
func (b *Bridge) ResolveDomainImport(id string, line int, replace bool) domain.Result[domain.ImportPreview] {
	return reply(b, "選擇匯入衝突", b.core.ResolveDomainImport(id, line, replace))
}
func (b *Bridge) ConfirmDomainImport(id string) domain.Result[bool] {
	return reply(b, "匯入", b.core.ConfirmDomainImport(id))
}
func (b *Bridge) UpdateDomain(revision, id string, enabled bool, url string) domain.Result[bool] {
	return reply(b, "編輯網域", b.core.UpdateDomain(revision, id, enabled, url))
}
func (b *Bridge) RemoveDomain(revision, id string) domain.Result[bool] {
	return reply(b, "移除網域", b.core.RemoveDomain(revision, id))
}
func (b *Bridge) StartDetection(ids []string) domain.Result[string] {
	return reply(b, "啟動偵測", b.core.StartDetection(ids))
}
func (b *Bridge) StartTest(ids []string, budget int64) domain.Result[string] {
	return reply(b, "啟動測速", b.core.StartTest(ids, budget))
}
func (b *Bridge) GetJob(id string) domain.Result[domain.Job] {
	return reply(b, "讀取作業", b.core.GetJob(id))
}
func (b *Bridge) ListJobs() domain.Result[[]domain.Job] {
	return reply(b, "作業歷史", b.core.ListJobs())
}
func (b *Bridge) DeleteJobs(id string) domain.Result[domain.JobHistoryDeletion] {
	return reply(b, "刪除執行紀錄", b.core.DeleteJobs(id))
}
func (b *Bridge) CancelJob(id string) domain.Result[bool] {
	return reply(b, "取消作業", b.core.CancelJob(id))
}
func (b *Bridge) BuildSyncPreview(ctx context.Context, ids []string) domain.Result[domain.SyncPreview] {
	return reply(b, "建立規則差異預覽", b.core.BuildSyncPreviewContext(ctx, ids))
}
func (b *Bridge) ApplySyncPreview(id string) domain.Result[string] {
	return reply(b, "套用規則", b.core.ApplySyncPreview(id))
}
func (b *Bridge) BuildRestorePreview(ctx context.Context, id string) domain.Result[domain.SyncPreview] {
	return reply(b, "建立復原預覽", b.core.BuildRestorePreviewContext(ctx, id))
}
func (b *Bridge) ApplyRestorePreview(id string) domain.Result[string] {
	return reply(b, "復原規則", b.core.ApplyRestorePreview(id))
}
func (b *Bridge) RecoverJob(id string) domain.Result[domain.Job] {
	return reply(b, "核對未決作業", b.core.RecoverJob(id))
}
func (b *Bridge) ConfigureAutomation(revision string, a domain.Automation) domain.Result[bool] {
	return reply(b, "自動授權", b.core.ConfigureAutomation(revision, a))
}
func (b *Bridge) PauseAutomation() domain.Result[bool] {
	return reply(b, "暫停自動", b.core.PauseAutomation())
}
func (b *Bridge) ResumeAutomation() domain.Result[bool] {
	return reply(b, "恢復自動", b.core.ResumeAutomation())
}
func (b *Bridge) SetLaunchAtLogin(enabled bool) domain.Result[bool] {
	return reply(b, "登入啟動", b.core.SetLaunchAtLogin(enabled))
}
func (b *Bridge) CleanExpiredCaches() domain.Result[int] {
	return reply(b, "清除過期快取", b.core.CleanExpiredCaches())
}
func (b *Bridge) DeleteTestResults(id string) domain.Result[bool] {
	return reply(b, "刪除測試結果", b.core.DeleteTestResults(id))
}
func (b *Bridge) ListErrors() domain.Result[[]domain.ErrorRecord] {
	return reply(b, "錯誤紀錄", b.core.ListErrors())
}
func (b *Bridge) DeleteError(id string) domain.Result[bool] {
	return reply(b, "刪除錯誤紀錄", b.core.DeleteError(id))
}
func (b *Bridge) ReportClientError(code string) domain.Result[domain.ErrorRecord] {
	return b.core.ReportClientError(code)
}
func (b *Bridge) ReportUIReady() domain.Result[bool] {
	if b.ready != nil {
		b.ready()
	}
	return domain.Success(true)
}
func (b *Bridge) QuitApplication() domain.Result[bool] {
	if b.quit != nil {
		b.quit()
	}
	return domain.Success(true)
}
func (b *Bridge) ExportDiagnostics() domain.Result[bool] {
	if b.export == nil {
		return reply(b, "匯出診斷", domain.Failure[bool](domain.Fail("DEPENDENCY_MISSING", "診斷匯出不可用")))
	}
	if err := b.export(); err != nil {
		if e, ok := err.(*domain.Error); ok && e.Code == "CANCELLED" {
			return domain.Failure[bool](err)
		}
		return reply(b, "匯出診斷", domain.Failure[bool](domain.Fail("STORAGE_ERROR", "診斷匯出未完成；請選新的可寫檔案")))
	}
	return domain.Success(true)
}
