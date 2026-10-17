package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/probe"
	"github.com/bestpika/go-fastly-ctrld/internal/storage"
	"golang.org/x/net/dns/dnsmessage"
)

type testVault struct {
	mu    sync.Mutex
	token string
}

func (v *testVault) Get() (string, error) { v.mu.Lock(); defer v.mu.Unlock(); return v.token, nil }
func (v *testVault) Set(s string) error   { v.mu.Lock(); defer v.mu.Unlock(); v.token = s; return nil }
func (v *testVault) Delete() error        { return v.Set("") }

type testNetwork struct{}

func (testNetwork) Snapshot(context.Context) domain.NetworkSnapshot {
	return domain.NetworkSnapshot{Fingerprint: "network", ObservedAt: time.Now().UTC()}
}

type testAPI struct {
	mu      sync.Mutex
	rules   map[string]domain.Rule
	writes  int
	deletes int
}

func (a *testAPI) ListProfiles(context.Context) ([]domain.Profile, error) {
	return []domain.Profile{{ID: "profile", Name: "測試"}}, nil
}
func (a *testAPI) Rules(context.Context, string) (map[string]domain.Rule, []domain.Group, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string]domain.Rule{}
	for k, r := range a.rules {
		out[k] = r
	}
	return out, nil, nil
}
func (a *testAPI) Write(_ context.Context, _ string, r domain.Rule, _ bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rules[r.Hostname] = r
	a.writes++
	return nil
}
func (a *testAPI) Delete(_ context.Context, _, h string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.rules, h)
	a.deletes++
	return nil
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newTestCore(t *testing.T, run ProbeRunner) (*Core, *testAPI) {
	t.Helper()
	root := t.TempDir()
	api := &testAPI{rules: map[string]domain.Rule{}}
	c, err := New(Options{Paths: storage.Paths{Config: filepath.Join(root, "config"), Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache")}, Vault: &testVault{}, Network: testNetwork{}, API: api, Probe: run})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Stop() })
	c.fastly.HTTP = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewBufferString(`{"addresses":["151.101.0.0/16"],"ipv6_addresses":["2a04:4e42::/32"]}`))}, nil
	})}
	c.dns.HTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		var query dnsmessage.Message
		if err := query.Unpack(b); err != nil {
			return nil, err
		}
		reply := dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true}, Questions: query.Questions}
		q := query.Questions[0]
		if q.Type == dnsmessage.TypeA {
			reply.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: q.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: [4]byte{151, 101, 0, 214}}}}
		}
		packed, _ := reply.Pack()
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/dns-message"}}, Body: io.NopCloser(bytes.NewReader(packed))}, nil
	})}
	if run != nil {
		c.probeEngine = localVerifier(t, nil)
	}
	return c, api
}

func importTestDomain(t *testing.T, c *Core) string {
	t.Helper()
	p := c.PreviewDomainImport("www.twitch.tv", "paste", "")
	if !p.OK || !c.ConfirmDomainImport(p.Data.ID).OK {
		t.Fatal("匯入失敗")
	}
	return c.GetAppSnapshot().Data.Domains[0].ID
}
func waitJob(t *testing.T, c *Core, id string) domain.Job {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		r := c.GetJob(id)
		c.gate.Lock()
		busy := c.testID == id || c.writerID == id
		c.gate.Unlock()
		if r.OK && !busy && r.Data.State != "queued" && r.Data.State != "running" {
			return r.Data
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("作業未完成：%+v", c.GetJob(id))
	return domain.Job{}
}

func successfulProbe(_ context.Context, e domain.DomainEntry, s domain.Settings, d domain.DetectionResult, _ domain.RangeSnapshot, n domain.NetworkSnapshot, _ *domain.Rule, _ *probe.Budget, _ func(int, int, string)) domain.TestResult {
	return domain.TestResult{Complete: true, DNSClassification: d.Classification, StrategyVersion: domain.ProbeStrategyVersion, ID: domain.ID(), DomainID: e.ID, Hostname: e.Hostname, SettingsRevision: s.Revision, NetworkFingerprint: n.Fingerprint, ObservedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Minute), BestIPv4: "151.101.0.214", AutomaticEligible: true, Candidates: []domain.CandidateResult{{IP: "151.101.0.214", Family: 4, Successes: 3, Samples: []domain.Sample{{Success: true}, {Success: true}, {Success: true}}}}}
}

func TestManualPreviewApplyRestoreAndNoRawInSnapshot(t *testing.T) {
	c, api := newTestCore(t, successfulProbe)
	id := importTestDomain(t, c)
	if !c.SetControlDToken("test-secret").OK {
		t.Fatal("權杖設定失敗")
	}
	s := c.GetAppSnapshot().Data.Settings
	if !c.SelectProfile(s.Revision, "profile").OK {
		t.Fatal("Profile 選擇失敗")
	}
	r := c.StartTest([]string{id}, 1_000_000)
	if !r.OK {
		t.Fatal(r.Error)
	}
	waitJob(t, c, r.Data)
	// 資料時間只供查閱；清理快取不得移除可套用結果。
	c.gate.Lock()
	result := c.results[id]
	result.ExpiresAt = time.Now().Add(-24 * time.Hour)
	c.results[id] = result
	c.gate.Unlock()
	p := c.BuildSyncPreview([]string{id})
	if !p.OK || !p.Data.Items[0].Applicable {
		t.Fatalf("預覽失敗 %+v", p)
	}
	c.gate.Lock()
	preview := c.previews[p.Data.ID]
	preview.ExpiresAt = time.Now().Add(-24 * time.Hour)
	c.previews[p.Data.ID] = preview
	c.gate.Unlock()
	if cleaned := c.CleanExpiredCaches(); !cleaned.OK {
		t.Fatal(cleaned.Error)
	}
	apply := c.ApplySyncPreview(p.Data.ID)
	if !apply.OK {
		t.Fatal(apply.Error)
	}
	j := waitJob(t, c, apply.Data)
	if j.State != "completed" {
		t.Fatalf("同步失敗 %+v", j)
	}
	if c.GetAppSnapshot().Data.Settings.Automation.Enabled {
		t.Fatal("手動套用不應啟用自動功能")
	}
	if r := c.DeleteJobs(j.ID); !r.OK || r.Data.Deleted != 1 {
		t.Fatal("已結束的套用紀錄未能從列表移除")
	}
	api.mu.Lock()
	rule := api.rules["www.twitch.tv"]
	rule.Raw = json.RawMessage(`{"hidden":"private-api-field"}`)
	api.rules[rule.Hostname] = rule
	api.mu.Unlock()
	undo := c.BuildRestorePreview(j.ID)
	if !undo.OK || undo.Data.Items[0].Applicable {
		t.Fatal("後續原始欄位變動應阻擋復原")
	}
	api.mu.Lock()
	rule.Raw = nil
	api.rules[rule.Hostname] = rule
	api.mu.Unlock()
	undo = c.BuildRestorePreview(j.ID)
	c.gate.Lock()
	preview = c.previews[undo.Data.ID]
	preview.ExpiresAt = time.Now().Add(-24 * time.Hour)
	c.previews[undo.Data.ID] = preview
	c.gate.Unlock()
	if cleaned := c.CleanExpiredCaches(); !cleaned.OK {
		t.Fatal(cleaned.Error)
	}
	restored := c.ApplyRestorePreview(undo.Data.ID)
	if !restored.OK {
		t.Fatal(restored.Error)
	}
	waitJob(t, c, restored.Data)
	api.mu.Lock()
	defer api.mu.Unlock()
	if api.writes != 1 || api.deletes != 1 {
		t.Fatalf("寫入／刪除次數錯誤 %d %d", api.writes, api.deletes)
	}
}

func TestSettingsGuardAndRecoveryProtection(t *testing.T) {
	c, _ := newTestCore(t, nil)
	c.gate.Lock()
	c.writerID = "writer"
	c.gate.Unlock()
	if c.SetControlDToken("secret").OK {
		t.Fatal("寫入中仍變更憑證")
	}
	c.gate.Lock()
	c.writerID = ""
	c.recovery = true
	c.gate.Unlock()
	if c.SetControlDToken("secret").OK {
		t.Fatal("未決狀態仍變更憑證")
	}
	if !c.PauseAutomation().OK {
		t.Fatal("未決狀態應可停用自動排程")
	}
}

func TestCancelledTestDoesNotWrite(t *testing.T) {
	started := make(chan struct{})
	run := func(ctx context.Context, e domain.DomainEntry, s domain.Settings, d domain.DetectionResult, r domain.RangeSnapshot, n domain.NetworkSnapshot, current *domain.Rule, b *probe.Budget, p func(int, int, string)) domain.TestResult {
		close(started)
		<-ctx.Done()
		out := successfulProbe(ctx, e, s, d, r, n, current, b, p)
		out.BestIPv4 = ""
		out.AutomaticEligible = false
		return out
	}
	c, api := newTestCore(t, run)
	id := importTestDomain(t, c)
	job := c.StartTest([]string{id}, 1_000_000)
	if !job.OK {
		t.Fatal(job.Error)
	}
	<-started
	c.CancelJob(job.Data)
	waitJob(t, c, job.Data)
	api.mu.Lock()
	defer api.mu.Unlock()
	if api.writes != 0 || api.deletes != 0 {
		t.Fatal("取消純測試仍寫入")
	}
}
