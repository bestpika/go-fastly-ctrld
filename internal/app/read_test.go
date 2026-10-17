package app

import (
	"context"
	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"sync/atomic"
	"testing"
	"time"
)

type readAPI struct {
	*testAPI
	groupsCalls  atomic.Int32
	profileCalls atomic.Int32
	fullCalls    atomic.Int32
	folderCalls  atomic.Int32
	folder       int
	wait         func(context.Context)
}

func (a *readAPI) Groups(context.Context, string) ([]domain.Group, error) {
	a.groupsCalls.Add(1)
	return []domain.Group{{ID: 7, Name: "selected"}, {ID: 8, Name: "other"}}, nil
}
func (a *readAPI) CreateGroup(context.Context, string, string) (domain.Group, error) {
	return domain.Group{}, domain.Fail("INVALID_INPUT", "測試不建立帳號資料")
}
func (a *readAPI) ListProfiles(context.Context) ([]domain.Profile, error) {
	a.profileCalls.Add(1)
	return []domain.Profile{{ID: "profile", Name: "test"}}, nil
}
func (a *readAPI) Rules(ctx context.Context, p string) (map[string]domain.Rule, []domain.Group, error) {
	a.fullCalls.Add(1)
	rules, _, err := a.testAPI.Rules(ctx, p)
	groups, _ := a.Groups(ctx, p)
	return rules, groups, err
}
func (a *readAPI) FolderRules(ctx context.Context, _ string, id int) ([]domain.Rule, error) {
	a.folderCalls.Add(1)
	a.folder = id
	if a.wait != nil {
		a.wait(ctx)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return []domain.Rule{{Hostname: "cdn.example.com", Group: id, Raw: []byte(`{"private":"not-for-ui"}`)}}, nil
}

func TestReadListIsSelectedFolderAndConfigurationUsesScopedCache(t *testing.T) {
	c, base := newTestCore(t, successfulProbe)
	api := &readAPI{testAPI: base}
	c.api = api
	if !c.ListProfilesContext(context.Background()).OK {
		t.Fatal("profile 讀取失敗")
	}
	s, _ := c.manager.Snapshot()
	if !c.SelectProfileContext(context.Background(), s.Revision, "profile").OK {
		t.Fatal("profile 保存失敗")
	}
	if !c.ListFoldersContext(context.Background()).OK {
		t.Fatal("folders 讀取失敗")
	}
	s, _ = c.manager.Snapshot()
	if !c.SelectFolderContext(context.Background(), s.Revision, 7).OK {
		t.Fatal("folder 保存失敗")
	}
	r := c.ReadRuleListContext(context.Background())
	if !r.OK || len(r.Data) != 1 || r.Data[0].Group != 7 || r.Data[0].Raw != nil || api.folder != 7 || api.fullCalls.Load() != 0 || api.groupsCalls.Load() != 1 || api.profileCalls.Load() != 1 {
		t.Fatal("清單讀取跨夾、洩原資料或重複選擇驗證")
	}
	if base.writes != 0 || base.deletes != 0 {
		t.Fatal("讀取動到規則")
	}
}

func TestReadContextCancelsAndRejectsChangedScope(t *testing.T) {
	for _, cancelRequest := range []bool{true, false} {
		t.Run(map[bool]string{true: "cancel", false: "scope"}[cancelRequest], func(t *testing.T) {
			c, base := newTestCore(t, successfulProbe)
			s, _ := c.manager.Snapshot()
			_ = c.manager.SelectProfile(s.Revision, "profile")
			started, release := make(chan struct{}), make(chan struct{})
			api := &readAPI{testAPI: base, wait: func(ctx context.Context) {
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
				}
			}}
			c.api = api
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan domain.Result[[]domain.Rule], 1)
			go func() { done <- c.ReadRuleListContext(ctx) }()
			<-started
			want := "CANCELLED"
			if cancelRequest {
				cancel()
			} else {
				s, _ = c.manager.Snapshot()
				_ = c.manager.SelectFolder(s.Revision, 8)
				want = "CONFLICT"
				close(release)
			}
			select {
			case r := <-done:
				if r.OK || r.Error.Code != want {
					t.Fatalf("接受過期讀取：%+v", r)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("讀取未停止")
			}
		})
	}
}

func TestPreviewStillUsesAllFoldersAndShowsForeignRuleMove(t *testing.T) {
	c, base := newTestCore(t, successfulProbe)
	api := &readAPI{testAPI: base}
	c.api = api
	id := importTestDomain(t, c)
	s, entries := c.manager.Snapshot()
	if err := c.manager.SelectProfile(s.Revision, "profile"); err != nil {
		t.Fatal(err)
	}
	s, _ = c.manager.Snapshot()
	if err := c.manager.SelectFolder(s.Revision, 7); err != nil {
		t.Fatal(err)
	}
	s, _ = c.manager.Snapshot()
	base.rules[entries[0].Hostname] = domain.Rule{Hostname: entries[0].Hostname, Group: 8, Do: 2, Status: 1, Via: "151.101.1.214", Safe: true}
	n := c.net.Snapshot(context.Background())
	now := time.Now().UTC()
	c.results[id] = domain.TestResult{ID: "proof", StrategyVersion: domain.ProbeStrategyVersion, DomainID: id, Hostname: entries[0].Hostname, Complete: true, DNSClassification: "confirmed", Mode: "connection", NetworkFingerprint: n.Fingerprint, SettingsRevision: s.Revision, ObservedAt: now, ExpiresAt: now.Add(time.Minute), BestIPv4: "151.101.0.214", AutomaticEligible: true}
	p := c.BuildSyncPreview([]string{id})
	if !p.OK || len(p.Data.Items) != 1 {
		t.Fatalf("預覽失敗：%+v", p.Error)
	}
	i := p.Data.Items[0]
	if api.fullCalls.Load() != 1 || api.folderCalls.Load() != 0 || !i.Applicable || !i.Move || i.Before == nil || i.Before.Group != 8 || i.After.Group != 7 {
		t.Fatalf("跨夾核對被清單讀取取代：%+v", i)
	}
	if base.writes != 0 || base.deletes != 0 {
		t.Fatal("唯讀預覽改了帳號")
	}
}
