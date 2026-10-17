package app

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/controld"
	"github.com/bestpika/go-fastly-ctrld/internal/dns"
	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/errorlog"
	"github.com/bestpika/go-fastly-ctrld/internal/fastly"
	"github.com/bestpika/go-fastly-ctrld/internal/network"
	"github.com/bestpika/go-fastly-ctrld/internal/probe"
	"github.com/bestpika/go-fastly-ctrld/internal/secrets"
	"github.com/bestpika/go-fastly-ctrld/internal/settings"
	"github.com/bestpika/go-fastly-ctrld/internal/storage"
	rulesync "github.com/bestpika/go-fastly-ctrld/internal/sync"
	"github.com/bestpika/go-fastly-ctrld/internal/version"
)

type Network interface {
	Snapshot(context.Context) domain.NetworkSnapshot
}
type ControlAPI interface {
	rulesync.API
	ListProfiles(context.Context) ([]domain.Profile, error)
}
type ProbeRunner func(context.Context, domain.DomainEntry, domain.Settings, domain.DetectionResult, domain.RangeSnapshot, domain.NetworkSnapshot, *domain.Rule, *probe.Budget, func(int, int, string)) domain.TestResult
type Options struct {
	Engine  *probe.Engine
	Paths   storage.Paths
	Vault   secrets.Vault
	Network Network
	API     ControlAPI
	Probe   ProbeRunner
}
type DesktopStatus struct {
	TrayAvailable   bool   `json:"trayAvailable"`
	TrayMessage     string `json:"trayMessage"`
	LaunchAvailable bool   `json:"launchAvailable"`
	LaunchMessage   string `json:"launchMessage"`
}
type Snapshot struct {
	Folders         []domain.Group            `json:"folders"`
	Version         string                    `json:"version"`
	Sequence        uint64                    `json:"sequence"`
	Settings        domain.Settings           `json:"settings"`
	Domains         []domain.DomainEntry      `json:"domains"`
	Network         domain.NetworkSnapshot    `json:"network"`
	Credentials     settings.CredentialStatus `json:"credentials"`
	Detections      []domain.DetectionResult  `json:"detections"`
	Results         []domain.TestResult       `json:"results"`
	Jobs            []domain.Job              `json:"jobs"`
	Profiles        []domain.Profile          `json:"profiles"`
	Recovery        bool                      `json:"recovery"`
	ActiveTestID    string                    `json:"activeTestId"`
	ActiveWriterID  string                    `json:"activeWriterId"`
	Desktop         DesktopStatus             `json:"desktop"`
	AutomationError string                    `json:"automationError"`
}
type Event struct {
	Error      *domain.ErrorRecord `json:"error,omitempty"`
	EventID    string              `json:"eventId"`
	Sequence   uint64              `json:"sequence"`
	OccurredAt time.Time           `json:"occurredAt"`
	Job        *domain.Job         `json:"job,omitempty"`
}
type fileSelection struct {
	Content string
	Kind    string
	Expires time.Time
}

type Core struct {
	errors                   *errorlog.Log
	folders                  []domain.Group
	foldersProfile           string
	foldersCredential        string
	profilesCredential       string
	gate                     sync.Mutex
	manager                  *settings.Manager
	data, cache              *storage.Store
	config                   *storage.Store
	api                      ControlAPI
	net                      Network
	fastly                   *fastly.Client
	dns                      *dns.Client
	runProbe                 ProbeRunner
	probeEngine              *probe.Engine
	nativeProbe              bool
	jobs                     map[string]domain.Job
	hiddenJobs               map[string]bool
	detections               map[string]domain.DetectionResult
	results                  map[string]domain.TestResult
	previews                 map[string]domain.SyncPreview
	imports                  map[string]domain.ImportPreview
	files                    map[string]fileSelection
	profiles                 []domain.Profile
	network                  domain.NetworkSnapshot
	desktop                  DesktopStatus
	testID, writerID         string
	testCancel, writerCancel context.CancelFunc
	writerDone               chan struct{}
	testDone                 chan struct{}
	recovery, stopping       bool
	automationError          string
	automationGeneration     uint64
	sequence                 atomic.Uint64
	Emit                     func(string, Event)
	Launch                   func(bool) error
	SelectFile               func() (string, error)
}

func New(o Options) (*Core, error) {
	if o.Paths.Config == "" {
		p, err := storage.UserPaths()
		if err != nil {
			return nil, err
		}
		o.Paths = p
	}
	cfg, err := storage.Open(o.Paths.Config)
	if err != nil {
		return nil, err
	}
	data, err := storage.Open(o.Paths.Data)
	if err != nil {
		return nil, err
	}
	cache, err := storage.Open(o.Paths.Cache)
	if err != nil {
		return nil, err
	}
	if o.Vault == nil {
		o.Vault = secrets.OSVault{}
	}
	m, err := settings.New(cfg, o.Vault)
	if err != nil {
		return nil, err
	}
	if o.Network == nil {
		o.Network = network.New()
	}
	if o.API == nil {
		o.API = controld.New(m.Token)
	}
	c := &Core{manager: m, config: cfg, data: data, cache: cache, api: o.API, net: o.Network, fastly: fastly.New(cache), dns: dns.New(), jobs: map[string]domain.Job{}, detections: map[string]domain.DetectionResult{}, results: map[string]domain.TestResult{}, previews: map[string]domain.SyncPreview{}, imports: map[string]domain.ImportPreview{}, files: map[string]fileSelection{}, profiles: []domain.Profile{}}
	c.errors = errorlog.New(data)
	if client, ok := c.api.(*controld.Client); ok {
		client.Report = func(err error) { c.RecordFailure("Control D", err, "") }
	}
	c.runProbe = o.Probe
	if c.runProbe == nil {
		c.nativeProbe = true
		c.probeEngine = o.Engine
		if c.probeEngine == nil {
			c.probeEngine = probe.New()
		}
		c.probeEngine.RefreshDNS = c.dns.Detect
		c.runProbe = func(ctx context.Context, e domain.DomainEntry, s domain.Settings, d domain.DetectionResult, r domain.RangeSnapshot, n domain.NetworkSnapshot, current *domain.Rule, b *probe.Budget, progress func(int, int, string)) domain.TestResult {
			engine := c.probeEngine.Fork(progress)
			return engine.Run(ctx, e, s, d, r, n, current, b)
		}
	}
	m.Guard = c.mutationGuard
	paths, err := data.List("jobs")
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		var j domain.Job
		if err = data.Read(path, &j); err != nil || j.Validate() != nil {
			return nil, domain.Fail("SCHEMA_UNSUPPORTED", "作業紀錄未知或損毀；請先保留資料並人工檢查")
		}
		if j.Pending() || j.State == "needs_recovery" {
			c.recovery = true
		} else if j.State == "running" || j.State == "queued" {
			j.State = "cancelled"
			j.Message = "上次工作中斷；未自動補送"
			j.FinishedAt = time.Now().UTC()
			if err = data.Write(path, j); err != nil {
				return nil, err
			}
		}
		c.jobs[j.ID] = j
	}
	if err = c.loadJobHistory(); err != nil {
		return nil, err
	}
	if err = c.loadAutomaticCheckpoint(); err != nil {
		return nil, err
	}
	if _, err = cache.CleanExpired(time.Now()); err != nil {
		return nil, err
	}
	for _, pair := range []struct {
		dir  string
		load func(string) error
	}{{"detections", func(path string) error {
		var d domain.DetectionResult
		if err := cache.Read(path, &d); err != nil {
			return err
		}
		c.detections[d.DomainID] = d
		return nil
	}}, {"tests", func(path string) error {
		var r domain.TestResult
		if err := cache.Read(path, &r); err != nil {
			return err
		}
		c.results[r.DomainID] = r
		return nil
	}}} {
		paths, err := cache.List(pair.dir)
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			if err = pair.load(path); err != nil {
				return nil, err
			}
		}
	}
	return c, nil
}

func copyOf[T any](v T) T {
	b, _ := json.Marshal(v)
	var result T
	_ = json.Unmarshal(b, &result)
	return result
}
func uiPreview(p domain.SyncPreview) domain.SyncPreview {
	p = copyOf(p)
	for i := range p.Items {
		if p.Items[i].Before != nil {
			p.Items[i].Before.Raw = nil
		}
		if p.Items[i].After != nil {
			p.Items[i].After.Raw = nil
		}
	}
	return p
}
func uiJob(j domain.Job) domain.Job {
	j = copyOf(j)
	for i := range j.Items {
		if j.Items[i].Preview.Before != nil {
			j.Items[i].Preview.Before.Raw = nil
		}
		if j.Items[i].Preview.After != nil {
			j.Items[i].Preview.After.Raw = nil
		}
		if j.Items[i].After != nil {
			j.Items[i].After.Raw = nil
		}
	}
	return j
}

func (c *Core) mutationGuard(operation string) (func(), error) {
	c.gate.Lock()
	if operation != "pause" && (c.stopping || c.writerID != "" || c.recovery) {
		c.gate.Unlock()
		return nil, domain.Fail("STATE_RECOVERY_REQUIRED", "請先等待寫入安全停止或核對未決作業")
	}
	if c.testCancel != nil {
		c.testCancel()
	}
	c.previews = map[string]domain.SyncPreview{}
	return c.gate.Unlock, nil
}
func (c *Core) event(name string, j *domain.Job) {
	seq := c.sequence.Add(1)
	if c.Emit != nil {
		c.Emit(name, Event{EventID: domain.ID(), Sequence: seq, OccurredAt: time.Now().UTC(), Job: j})
	}
}
func (c *Core) changed(err error) domain.Result[bool] {
	if err != nil {
		return domain.Failure[bool](err)
	}
	c.event("settings:changed", nil)
	return domain.Success(true)
}
func (c *Core) GetAppSnapshot() domain.Result[Snapshot] {
	c.gate.Lock()
	s, ds := c.manager.Snapshot()
	out := Snapshot{Version: version.Version, Sequence: c.sequence.Load(), Settings: s, Domains: ds, Network: copyOf(c.network), Desktop: c.desktop, Recovery: c.recovery, ActiveTestID: c.testID, ActiveWriterID: c.writerID, Detections: []domain.DetectionResult{}, Results: []domain.TestResult{}, Jobs: []domain.Job{}, Profiles: copyOf(c.profiles)}
	out.AutomationError = c.automationError
	out.Folders = []domain.Group{}
	if c.foldersProfile == s.ProfileID {
		out.Folders = copyOf(c.folders)
	}
	if c.automationError != "" {
		out.Settings.Automation.Enabled = false
	}
	for _, d := range c.detections {
		out.Detections = append(out.Detections, copyOf(d))
	}
	for _, r := range c.results {
		out.Results = append(out.Results, copyOf(r))
	}
	for _, j := range c.jobs {
		if !c.hiddenJobs[j.ID] || !c.canDeleteJobHistory(j) {
			out.Jobs = append(out.Jobs, uiJob(j))
		}
	}
	c.gate.Unlock()
	out.Credentials = c.manager.CredentialStatus()
	sort.Slice(out.Detections, func(i, j int) bool { return out.Detections[i].Hostname < out.Detections[j].Hostname })
	sort.Slice(out.Results, func(i, j int) bool { return out.Results[i].Hostname < out.Results[j].Hostname })
	sort.Slice(out.Jobs, func(i, j int) bool { return out.Jobs[i].StartedAt.After(out.Jobs[j].StartedAt) })
	return domain.Success(out)
}
func (c *Core) UpdateSettings(revision string, p settings.Patch) domain.Result[bool] {
	return c.changed(c.manager.Update(revision, p))
}
func (c *Core) SetControlDToken(token string) domain.Result[bool] {
	return c.changed(c.manager.SetToken(token))
}
func (c *Core) DeleteControlDToken() domain.Result[bool] { return c.changed(c.manager.DeleteToken()) }
func (c *Core) GetCredentialStatus() domain.Result[settings.CredentialStatus] {
	return domain.Success(c.manager.CredentialStatus())
}
func (c *Core) ListProfiles() domain.Result[[]domain.Profile] {
	return c.ListProfilesContext(context.Background())
}
func (c *Core) ListProfilesContext(parent context.Context) domain.Result[[]domain.Profile] {
	s, _ := c.manager.Snapshot()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	profiles, err := c.api.ListProfiles(ctx)
	if err != nil {
		return domain.Failure[[]domain.Profile](readFailure(parent, err))
	}
	c.gate.Lock()
	current, _ := c.manager.Snapshot()
	if parent.Err() != nil || s.CredentialRevision != current.CredentialRevision {
		c.gate.Unlock()
		return domain.Failure[[]domain.Profile](domain.Fail("CANCELLED", "讀取範圍已變更"))
	}
	c.profiles = copyOf(profiles)
	c.profilesCredential = s.CredentialRevision
	c.gate.Unlock()
	return domain.Success(profiles)
}
func (c *Core) CheckControlDConnection() domain.Result[bool] {
	return c.CheckControlDConnectionContext(context.Background())
}
func (c *Core) CheckControlDConnectionContext(ctx context.Context) domain.Result[bool] {
	r := c.ListProfilesContext(ctx)
	if !r.OK {
		return domain.Failure[bool](r.Error)
	}
	return domain.Success(true)
}
func (c *Core) SelectProfile(revision, id string) domain.Result[bool] {
	return c.SelectProfileContext(context.Background(), revision, id)
}
func (c *Core) SelectProfileContext(ctx context.Context, revision, id string) domain.Result[bool] {
	s, _ := c.manager.Snapshot()
	c.gate.Lock()
	cached := copyOf(c.profiles)
	valid := c.profilesCredential == s.CredentialRevision
	c.gate.Unlock()
	r := domain.Success(cached)
	if !valid {
		r = c.ListProfilesContext(ctx)
	}
	if !r.OK {
		return domain.Failure[bool](r.Error)
	}
	for _, p := range r.Data {
		if p.ID == id {
			if ctx.Err() != nil {
				return domain.Failure[bool](readFailure(ctx, ctx.Err()))
			}
			return c.changed(c.manager.SelectProfile(revision, id))
		}
	}
	return domain.Failure[bool](domain.Fail("INVALID_INPUT", "Profile 不在目前帳號清單"))
}
func (c *Core) SelectImportFile() domain.Result[string] {
	if c.SelectFile == nil {
		return domain.Failure[string](domain.Fail("DEPENDENCY_MISSING", "目前沒有原生選檔介面"))
	}
	path, err := c.SelectFile()
	if err != nil {
		return domain.Failure[string](domain.Fail("INVALID_INPUT", "選檔失敗"))
	}
	if path == "" {
		return domain.Success("")
	}
	f, err := os.Open(path)
	if err != nil {
		return domain.Failure[string](domain.Fail("INVALID_INPUT", "無法讀取選取的檔案"))
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return domain.Failure[string](domain.Fail("INVALID_INPUT", "檔案須為最多 1 MiB 的一般文字檔"))
	}
	b := make([]byte, info.Size())
	read := 0
	for read < len(b) {
		n, err := f.Read(b[read:])
		read += n
		if err != nil && read < len(b) {
			return domain.Failure[string](domain.Fail("INVALID_INPUT", "文字檔讀取中斷"))
		}
	}
	kind := "txt"
	if len(path) >= 4 && path[len(path)-4:] == ".csv" {
		kind = "csv"
	}
	id := domain.ID()
	c.gate.Lock()
	c.files[id] = fileSelection{Content: string(b), Kind: kind, Expires: time.Now().Add(5 * time.Minute)}
	c.gate.Unlock()
	return domain.Success(id)
}
func (c *Core) PreviewDomainImport(content, kind, fileID string) domain.Result[domain.ImportPreview] {
	if fileID != "" {
		c.gate.Lock()
		f, ok := c.files[fileID]
		delete(c.files, fileID)
		c.gate.Unlock()
		if !ok || !f.Expires.After(time.Now()) {
			return domain.Failure[domain.ImportPreview](domain.Fail("PREVIEW_EXPIRED", "檔案選取已過期"))
		}
		content = f.Content
		kind = f.Kind
	}
	s, ds := c.manager.Snapshot()
	p, err := domain.ParseImport(content, kind, s.Revision, ds)
	if err != nil {
		return domain.Failure[domain.ImportPreview](err)
	}
	c.gate.Lock()
	c.imports[p.ID] = copyOf(p)
	c.gate.Unlock()
	return domain.Success(p)
}
func (c *Core) ConfirmDomainImport(id string) domain.Result[bool] {
	c.gate.Lock()
	p, ok := c.imports[id]
	if ok {
		for _, row := range p.Rows {
			if row.State == "conflict" {
				c.gate.Unlock()
				return domain.Failure[bool](domain.Fail("CONFLICT", "請先確認每筆不同測速網址的取代選擇"))
			}
		}
	}
	delete(c.imports, id)
	c.gate.Unlock()
	if !ok {
		return domain.Failure[bool](domain.Fail("PREVIEW_EXPIRED", "匯入預覽不存在"))
	}
	return c.changed(c.manager.Import(p))
}
func (c *Core) UpdateDomain(revision, id string, enabled bool, url string) domain.Result[bool] {
	return c.changed(c.manager.UpdateDomain(revision, id, enabled, url))
}
func (c *Core) RemoveDomain(revision, id string) domain.Result[bool] {
	return c.changed(c.manager.RemoveDomain(revision, id))
}
func (c *Core) ConfigureAutomation(revision string, a domain.Automation) domain.Result[bool] {
	c.gate.Lock()
	generation := c.automationGeneration
	c.gate.Unlock()
	if a.Enabled && !a.TestOnly && !c.manager.CredentialStatus().Saved {
		return domain.Failure[bool](domain.Fail("AUTH_FAILED", "請先安全保存可用權杖"))
	}
	if a.Enabled {
		if err := c.reconcileAutomaticHistory(); err != nil {
			if detail, ok := err.(*domain.Error); ok && detail.Code == "STORAGE_ERROR" {
				c.blockAutomatic("排程紀錄無法安全保存；請修復儲存空間後重新確認授權")
			}
			return domain.Failure[bool](err)
		}
	}
	r := c.changed(c.manager.Configure(revision, a))
	if r.OK {
		c.gate.Lock()
		blocked := c.automationError != ""
		c.gate.Unlock()
		if a.Enabled && blocked {
			if err := c.clearAutomaticCheckpoint(generation, true); err != nil {
				return domain.Failure[bool](err)
			}
		}
		c.event("automation:changed", nil)
	}
	return r
}
func (c *Core) ResumeAutomation() domain.Result[bool] {
	s, _ := c.manager.Snapshot()
	a := s.Automation
	a.Enabled = true
	return c.ConfigureAutomation(s.Revision, a)
}
func (c *Core) PauseAutomation() domain.Result[bool] {
	c.gate.Lock()
	if c.writerCancel != nil {
		c.writerCancel()
	}
	done := c.writerDone
	c.gate.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			return domain.Failure[bool](domain.Fail("STATE_RECOVERY_REQUIRED", "寫入仍在安全核對中，請稍後暫停"))
		}
	}
	return c.changed(c.manager.Pause())
}
func (c *Core) SetLaunchAtLogin(enabled bool) domain.Result[bool] {
	if c.Launch == nil {
		return domain.Failure[bool](domain.Fail("DEPENDENCY_MISSING", "登入啟動功能不可用"))
	}
	err := c.manager.LaunchAtLogin(enabled, func() error { return c.Launch(enabled) })
	c.gate.Lock()
	if err != nil {
		c.desktop.LaunchMessage = "設定保存但平台啟動設定未成功；請重試"
	} else {
		c.desktop.LaunchMessage = "平台設定已更新"
	}
	c.gate.Unlock()
	return c.changed(err)
}
func (c *Core) SetDesktopStatus(s DesktopStatus) {
	c.gate.Lock()
	c.desktop = s
	c.gate.Unlock()
	c.event("settings:changed", nil)
}

func (c *Core) publish(j domain.Job) {
	j = copyOf(j)
	c.gate.Lock()
	previous := c.jobs[j.ID].State
	if old, ok := c.jobs[j.ID]; ok && old.CancelRequested {
		j.CancelRequested = true
	}
	c.jobs[j.ID] = j
	c.recovery = false
	for _, other := range c.jobs {
		if other.Pending() || other.State == "needs_recovery" {
			c.recovery = true
		}
	}
	c.gate.Unlock()
	if previous != j.State && (j.State == "failed" || j.State == "needs_recovery") {
		c.RecordFailure("背景作業", domain.Fail("JOB_FAILED", j.Message), j.ID)
	}
	visible := uiJob(j)
	c.event("job:updated", &visible)
	if j.State == "needs_recovery" {
		c.event("recovery:required", &visible)
	}
}
func (c *Core) GetJob(id string) domain.Result[domain.Job] {
	c.gate.Lock()
	j, ok := c.jobs[id]
	c.gate.Unlock()
	if !ok {
		return domain.Failure[domain.Job](domain.Fail("INVALID_INPUT", "找不到作業"))
	}
	return domain.Success(uiJob(j))
}
func (c *Core) ListJobs() domain.Result[[]domain.Job] {
	return domain.Success(c.GetAppSnapshot().Data.Jobs)
}
func (c *Core) CancelJob(id string) domain.Result[bool] {
	c.gate.Lock()
	defer c.gate.Unlock()
	if id == c.testID && c.testCancel != nil {
		c.testCancel()
	} else if id == c.writerID && c.writerCancel != nil {
		c.writerCancel()
	} else {
		return domain.Failure[bool](domain.Fail("INVALID_INPUT", "作業未執行中"))
	}
	j := c.jobs[id]
	j.CancelRequested = true
	c.jobs[id] = j
	return domain.Success(true)
}
func (c *Core) Stop() bool {
	c.gate.Lock()
	c.stopping = true
	if c.testCancel != nil {
		c.testCancel()
	}
	if c.writerCancel != nil {
		c.writerCancel()
	}
	done := c.writerDone
	testDone := c.testDone
	c.gate.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			return false
		}
	}
	if testDone != nil {
		select {
		case <-testDone:
		case <-time.After(30 * time.Second):
			return false
		}
	}
	return true
}
func (c *Core) saveJob(j domain.Job) error {
	if err := c.data.Write("jobs/"+j.ID+".json", j); err != nil {
		return err
	}
	c.publish(j)
	return nil
}
func (c *Core) RecoverJob(id string) domain.Result[domain.Job] {
	c.gate.Lock()
	j, ok := c.jobs[id]
	if !ok || c.writerID != "" {
		c.gate.Unlock()
		return domain.Failure[domain.Job](domain.Fail("STATE_RECOVERY_REQUIRED", "作業不存在或仍在寫入"))
	}
	s, _ := c.manager.Snapshot()
	if j.ProfileID != s.ProfileID || j.CredentialRevision != s.CredentialRevision {
		c.gate.Unlock()
		return domain.Failure[domain.Job](domain.Fail("CONFLICT", "未決作業與目前帳號不一致"))
	}
	c.writerID = id
	c.writerDone = make(chan struct{})
	c.gate.Unlock()
	defer c.releaseWriter()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	e := rulesync.Engine{API: c.api, Store: c.data, Changed: c.publish}
	j, err := e.Recover(ctx, j)
	if err != nil {
		return domain.Failure[domain.Job](err)
	}
	return domain.Success(uiJob(j))
}
func (c *Core) releaseWriter() {
	c.gate.Lock()
	c.writerID = ""
	c.writerCancel = nil
	if c.writerDone != nil {
		close(c.writerDone)
		c.writerDone = nil
	}
	c.gate.Unlock()
	c.event("job:updated", nil)
}
func (c *Core) releaseTest(id string) {
	c.gate.Lock()
	changed := c.testID == id
	if changed {
		c.testID = ""
		c.testCancel = nil
		if c.testDone != nil {
			close(c.testDone)
			c.testDone = nil
		}
	}
	c.gate.Unlock()
	if changed {
		c.event("job:updated", nil)
	}
}
