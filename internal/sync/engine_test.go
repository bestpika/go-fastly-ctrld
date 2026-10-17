package rulesync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/storage"
)

type fakeAPI struct {
	groups     []domain.Group
	rules      map[string]domain.Rule
	writes     int
	deletes    int
	timeout    bool
	unreadable bool
	reject     string
}

func (a *fakeAPI) Rules(context.Context, string) (map[string]domain.Rule, []domain.Group, error) {
	if a.unreadable && a.writes > 0 {
		return nil, nil, errors.New("失敗")
	}
	result := map[string]domain.Rule{}
	for k, v := range a.rules {
		result[k] = v
	}
	return result, a.groups, nil
}

func TestDestinationChangedToActionFolderBeforeWriteStops(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	a := &fakeAPI{rules: map[string]domain.Rule{}, groups: []domain.Group{{ID: 7, Action: true}}}
	e := Engine{API: a, Store: store}
	r := domain.Rule{Hostname: "cdn.example.com", Do: 2, Status: 1, Via: "151.101.0.214", Group: 7, Safe: true}
	p := domain.SyncPreview{Kind: "apply", ProfileID: "profile", ExpiresAt: time.Now().Add(time.Minute), Items: []domain.PreviewItem{{Hostname: r.Hostname, After: &r, Applicable: true}}}
	j := e.Apply(context.Background(), p, domain.Job{})
	if a.writes != 0 || j.State != "failed" {
		t.Fatal("最後回讀發現繼承動作仍寫入")
	}
}
func (a *fakeAPI) Write(_ context.Context, _ string, r domain.Rule, _ bool) error {
	a.writes++
	if r.Hostname == a.reject {
		return errors.New("拒絕")
	}
	a.rules[r.Hostname] = r
	if a.timeout {
		return errors.New("逾時")
	}
	return nil
}

func TestPartialFailureAndDurablePostWriteRecovery(t *testing.T) {
	for _, crash := range []bool{false, true} {
		t.Run(map[bool]string{false: "部分失敗", true: "寫入後保存故障"}[crash], func(t *testing.T) {
			store, _ := storage.Open(t.TempDir())
			a := &fakeAPI{rules: map[string]domain.Rule{}, reject: "second.example.com"}
			e := Engine{API: a, Store: store}
			p := domain.SyncPreview{Kind: "apply", ProfileID: "profile", ExpiresAt: time.Now().Add(time.Minute)}
			for _, h := range []string{"first.example.com", "second.example.com"} {
				r := domain.Rule{Hostname: h, Do: 2, Status: 1, Via: "151.101.0.214", Safe: true}
				p.Items = append(p.Items, domain.PreviewItem{Hostname: h, After: &r, Applicable: true})
			}
			if crash {
				store.BeforeReplace = func() error {
					if a.writes > 0 {
						return errors.New("故障")
					}
					return nil
				}
			}
			j := e.Apply(context.Background(), p, domain.Job{})
			if !crash {
				if j.State != "partial" || a.writes != 2 {
					t.Fatalf("未正確回報部分成功 %+v", j)
				}
				return
			}
			if j.State != "needs_recovery" || a.writes != 1 {
				t.Fatal("保存故障仍繼續寫入")
			}
			store.BeforeReplace = nil
			var saved domain.Job
			if err := store.Read("jobs/"+j.ID+".json", &saved); err != nil {
				t.Fatal(err)
			}
			if !saved.Pending() {
				t.Fatal("寫入前紀錄沒有保留")
			}
			if _, err := e.Recover(context.Background(), saved); err != nil || a.writes != 1 {
				t.Fatal("重新啟動核對盲目重送")
			}
		})
	}
}
func (a *fakeAPI) Delete(_ context.Context, _, h string) error {
	a.deletes++
	delete(a.rules, h)
	return nil
}

func TestTimeoutReadbackAndRestoreConflict(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	a := &fakeAPI{rules: map[string]domain.Rule{}, timeout: true}
	e := Engine{API: a, Store: store}
	after := domain.Rule{Hostname: "www.twitch.tv", Do: 2, Status: 1, Via: "151.101.0.214", Safe: true}
	p := domain.SyncPreview{ID: domain.ID(), Kind: "apply", ProfileID: "profile", ExpiresAt: time.Now().Add(time.Minute), Items: []domain.PreviewItem{{ID: domain.ID(), Hostname: after.Hostname, After: &after, Applicable: true}}}
	job := e.Apply(context.Background(), p, domain.Job{})
	if job.State != "completed" || a.writes != 1 {
		t.Fatalf("逾時後沒有先回讀 %+v", job)
	}
	s := domain.DefaultSettings()
	s.ProfileID = "profile"
	a.rules[after.Hostname] = domain.Rule{Hostname: after.Hostname, Do: 0, Status: 1, Safe: true}
	undo := BuildRestore(s, domain.NetworkSnapshot{}, job, a.rules)
	if undo.Items[0].Applicable {
		t.Fatal("外部修改仍刪除")
	}
}

func TestUnknownJournalRecoveryDoesNotRetry(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	a := &fakeAPI{rules: map[string]domain.Rule{}, unreadable: true}
	e := Engine{API: a, Store: store}
	after := domain.Rule{Hostname: "www.twitch.tv", Do: 2, Status: 1, Via: "151.101.0.214", Safe: true}
	p := domain.SyncPreview{Kind: "apply", ProfileID: "profile", ExpiresAt: time.Now().Add(time.Minute), Items: []domain.PreviewItem{{Hostname: after.Hostname, After: &after, Applicable: true}}}
	job := e.Apply(context.Background(), p, domain.Job{})
	if !job.Pending() || job.State != "needs_recovery" {
		t.Fatal("未保留未決狀態")
	}
	a.unreadable = false
	job, err := e.Recover(context.Background(), job)
	if err != nil || job.Pending() || a.writes != 1 {
		t.Fatal("恢復核對重送寫入")
	}
}

func TestSnapshotFailureAndCancelDoNotWrite(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	a := &fakeAPI{rules: map[string]domain.Rule{}}
	e := Engine{API: a, Store: store}
	store.BeforeReplace = func() error { return errors.New("注入故障") }
	p := domain.SyncPreview{Kind: "apply", ExpiresAt: time.Now().Add(time.Minute), Items: []domain.PreviewItem{{Hostname: "www.twitch.tv", Applicable: true}}}
	e.Apply(context.Background(), p, domain.Job{})
	if a.writes != 0 || a.deletes != 0 {
		t.Fatal("保存失敗仍修改")
	}
	store.BeforeReplace = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.Apply(ctx, p, domain.Job{})
	if a.writes != 0 || a.deletes != 0 {
		t.Fatal("取消仍修改")
	}
}

func TestSameValueRewriteRequiresAcceptedResponseAndDurableEvidence(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		reject, timeout, unreadable bool
		failSave                    int
		want                        string
		accepted                    bool
	}{
		{name: "明確成功", want: "verified", accepted: true},
		{name: "拒絕不能以原值假裝成功", reject: true, want: "unconfirmed"},
		{name: "逾時同值不能假裝成功", timeout: true, want: "unconfirmed"},
		{name: "接受後回讀失敗只核對", unreadable: true, want: "verified", accepted: true},
		{name: "成功證據保存前故障", failSave: 1, want: "unconfirmed"},
		{name: "成功證據保存後故障", failSave: 2, want: "verified", accepted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := storage.Open(t.TempDir())
			r := domain.Rule{Hostname: "cdn.example.com", Do: 2, Status: 1, Via: "edge.fastly.net", Safe: true}
			a := &fakeAPI{rules: map[string]domain.Rule{r.Hostname: r}, timeout: tc.timeout, unreadable: tc.unreadable}
			if tc.reject {
				a.reject = r.Hostname
			}
			postWrites := 0
			store.BeforeReplace = func() error {
				if a.writes > 0 {
					postWrites++
					if postWrites == tc.failSave {
						return errors.New("故障")
					}
				}
				return nil
			}
			e := Engine{API: a, Store: store}
			p := domain.SyncPreview{Kind: "apply", ProfileID: "profile", ExpiresAt: time.Now().Add(time.Minute), Items: []domain.PreviewItem{{Hostname: r.Hostname, Before: &r, BeforeDigest: r.ObservationDigest(), After: &r, Applicable: true, ForceWrite: true, Operation: "rewrite"}}}
			job := e.Apply(context.Background(), p, domain.Job{})
			if a.writes != 1 {
				t.Fatal("同值未實際送出或發生重送")
			}
			store.BeforeReplace = nil
			a.unreadable = false
			var saved domain.Job
			if err := store.Read("jobs/"+job.ID+".json", &saved); err != nil {
				t.Fatal(err)
			}
			if saved.Pending() {
				var err error
				saved, err = e.Recover(context.Background(), saved)
				if err != nil {
					t.Fatal(err)
				}
			}
			if saved.Items[0].State != tc.want || saved.Items[0].WriteAccepted != tc.accepted || a.writes != 1 || saved.Pending() {
				t.Fatalf("不正確的同值核對：%+v", saved)
			}
			if tc.want == "unconfirmed" && (saved.State == "completed" || len(BuildRestore(domain.DefaultSettings(), domain.NetworkSnapshot{}, saved, a.rules).Items) != 0) {
				t.Fatal("未確認寫入被當成成功或提供復原")
			}
		})
	}
}

func TestLegacySameValueJournalWithoutAcceptedProofIsNotVerified(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	r := domain.Rule{Hostname: "cdn.example.com", Do: 2, Status: 1, Via: "151.101.0.214", Safe: true}
	a := &fakeAPI{rules: map[string]domain.Rule{r.Hostname: r}}
	e := Engine{API: a, Store: store}
	j := domain.Job{ID: domain.ID(), Items: []domain.JournalItem{{State: "write_started", Preview: domain.PreviewItem{Hostname: r.Hostname, Before: &r, BeforeDigest: r.ObservationDigest(), After: &r}}}}
	j, err := e.Recover(context.Background(), j)
	if err != nil || j.Items[0].State != "unconfirmed" || j.Pending() || a.writes != 0 {
		t.Fatal("舊 journal 同值被誤判成功或自動重送")
	}
}

func TestPreviewAgeDoesNotPreventValidatedWrite(t *testing.T) {
	store, _ := storage.Open(t.TempDir())
	a := &fakeAPI{rules: map[string]domain.Rule{}}
	r := domain.Rule{Hostname: "cdn.example.com", Do: 2, Status: 1, Via: "151.101.0.214", Safe: true}
	p := domain.SyncPreview{Kind: "apply", ProfileID: "profile", ExpiresAt: time.Now().Add(-time.Hour), Items: []domain.PreviewItem{{Hostname: r.Hostname, After: &r, Applicable: true}}}
	validations := 0
	e := Engine{API: a, Store: store, Validate: func(context.Context, domain.SyncPreview, domain.PreviewItem) error {
		validations++
		return nil
	}}
	j := e.Apply(context.Background(), p, domain.Job{})
	if a.writes != 1 || validations != 1 || j.State != "completed" || j.Pending() {
		t.Fatal("時間阻擋有效預覽或略過實際驗證")
	}
}
