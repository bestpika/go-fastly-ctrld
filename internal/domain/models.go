package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

const SchemaVersion = 1
const ProbeStrategyVersion = 10
const PresetVersion = 2

func ID() string { return rand.Text() }

func Digest(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

type Error struct {
	ErrorID    string `json:"errorId,omitempty"`
	Source     string `json:"source,omitempty"`
	Operation  string `json:"operation,omitempty"`
	HTTPStatus int    `json:"httpStatus,omitempty"`
	VendorCode string `json:"vendorCode,omitempty"`
	RequestID  string `json:"requestId,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Retryable  bool   `json:"retryable"`
}

func (e *Error) Error() string { return e.Message }

func Fail(code, message string) error { return &Error{Code: code, Message: message} }

type Result[T any] struct {
	OK    bool   `json:"ok"`
	Data  T      `json:"data"`
	Error *Error `json:"error"`
}

func Success[T any](data T) Result[T] { return Result[T]{OK: true, Data: data} }

func Failure[T any](err error) Result[T] {
	e, ok := err.(*Error)
	if !ok {
		e = &Error{Code: "INTERNAL_ERROR", Message: "操作失敗，請檢查環境或稍後重試"}
	}
	return Result[T]{Error: e}
}

type Settings struct {
	PresetVersion       int           `json:"presetVersion"`
	DestinationFolderID int           `json:"destinationFolderId"`
	Revision            string        `json:"revision"`
	CredentialRevision  string        `json:"credentialRevision"`
	ProfileID           string        `json:"profileId"`
	Theme               string        `json:"theme"`
	LaunchAtLogin       bool          `json:"launchAtLogin"`
	ForceWrite          bool          `json:"forceWrite"`
	Automation          Automation    `json:"automation"`
	Probe               ProbeSettings `json:"probe"`
	Cursor              int           `json:"cursor"`
}

type ProbeSettings struct {
	ScanMode            string   `json:"scanMode"`
	NoResourceMode      string   `json:"noResourceMode"`
	Services            []string `json:"services"`
	EnableIPv6          bool     `json:"enableIpv6"`
	Candidates          int      `json:"candidates"`
	Rounds              int      `json:"rounds"`
	ConnectionSeconds   int      `json:"connectionSeconds"`
	SampleSeconds       int      `json:"sampleSeconds"`
	JobSeconds          int      `json:"jobSeconds"`
	SpeedImprovement    float64  `json:"speedImprovement,omitempty"`
	LatencyImprovement  float64  `json:"latencyImprovement,omitempty"`
	LatencyMilliseconds float64  `json:"latencyMilliseconds,omitempty"`
}

type Grant struct {
	DomainID  string `json:"domainId"`
	Overwrite bool   `json:"overwrite"`
	Move      bool   `json:"move"`
}

type Automation struct {
	DestinationFolderID  int                  `json:"destinationFolderId"`
	TestOnly             bool                 `json:"testOnly"`
	AllowSharedReference bool                 `json:"allowSharedReference"`
	Enabled              bool                 `json:"enabled"`
	ProfileID            string               `json:"profileId"`
	CredentialRevision   string               `json:"credentialRevision"`
	Grants               []Grant              `json:"grants"`
	IntervalSeconds      int                  `json:"intervalSeconds"`
	StabilitySeconds     int                  `json:"stabilitySeconds"`
	CooldownSeconds      int                  `json:"cooldownSeconds"`
	BudgetBytes          int64                `json:"budgetBytes"`
	LastRun              time.Time            `json:"lastRun"`
	LastChanged          map[string]time.Time `json:"lastChanged"`
}

func DefaultSettings() Settings {
	return Settings{
		Revision: ID(), CredentialRevision: ID(), Theme: "light", PresetVersion: PresetVersion,
		Probe:      FixedProbe(ProbeSettings{ScanMode: "near"}),
		Automation: Automation{Grants: []Grant{}, IntervalSeconds: 86400, StabilitySeconds: 30, CooldownSeconds: 900, BudgetBytes: 0, LastChanged: map[string]time.Time{}},
	}
}

func (s Settings) Validate() error {
	p, a := s.Probe, s.Automation
	if p.NoResourceMode != "" && p.NoResourceMode != "shared" && p.NoResourceMode != "latency" {
		return Fail("INVALID_INPUT", "無資源測試模式不正確")
	}
	seen := map[string]bool{}
	for _, service := range p.Services {
		if seen[service] || service != "github" && service != "twitch" && service != "x" {
			return Fail("INVALID_INPUT", "基準服務選擇不正確或重複")
		}
		seen[service] = true
	}
	if s.Revision == "" || s.CredentialRevision == "" || (s.Theme != "light" && s.Theme != "dark") || s.Cursor < 0 {
		return Fail("INVALID_INPUT", "設定格式不正確")
	}
	if p.Candidates < 1 || p.Candidates > 30 || p.Rounds < 3 || p.Rounds > 10 || p.ConnectionSeconds < 1 || p.ConnectionSeconds > 30 || p.SampleSeconds < 2 || p.SampleSeconds > 30 || p.JobSeconds != 0 && (p.JobSeconds < 30 || p.JobSeconds > 86400) || p.ScanMode != "" && p.ScanMode != "near" && p.ScanMode != "full" {
		return Fail("INVALID_INPUT", "探測數量或期限超出安全範圍")
	}
	if s.DestinationFolderID < 0 {
		return Fail("INVALID_INPUT", "目的資料夾無效")
	}
	if a.IntervalSeconds < 60 || a.IntervalSeconds > 31*86400 || a.StabilitySeconds < 5 || a.StabilitySeconds > 600 || a.CooldownSeconds < 60 || a.CooldownSeconds > 86400 || !ValidBudget(a.BudgetBytes) {
		return Fail("INVALID_INPUT", "排程或流量設定超出安全範圍")
	}
	if a.Enabled && !a.TestOnly && (s.ProfileID == "" || a.ProfileID != s.ProfileID || a.CredentialRevision != s.CredentialRevision || a.DestinationFolderID != s.DestinationFolderID || len(a.Grants) == 0) {
		return Fail("INVALID_INPUT", "自動管理授權與目前帳號不一致")
	}
	return nil
}

func ValidBudget(n int64) bool { return n == 0 || n >= 1_000_000 && n <= 100_000_000_000 }

// 原版數值固定；基準選擇、擴充選項與整作業上限由使用者決定。
func FixedProbe(p ProbeSettings) ProbeSettings {
	mode := p.ScanMode
	if mode == "" {
		mode = "near"
	}
	resourceMode := p.NoResourceMode
	if resourceMode == "" {
		resourceMode = "latency"
	}
	services := slices.Clone(p.Services)
	if services == nil {
		services = []string{"github", "twitch", "x"}
	}
	return ProbeSettings{ScanMode: mode, NoResourceMode: resourceMode, Services: services, EnableIPv6: p.EnableIPv6, Candidates: 10, Rounds: 3, ConnectionSeconds: 10, SampleSeconds: 10, JobSeconds: p.JobSeconds}
}

type ErrorRecord struct {
	ID         string    `json:"id"`
	OccurredAt time.Time `json:"occurredAt"`
	Source     string    `json:"source"`
	Operation  string    `json:"operation"`
	Code       string    `json:"code"`
	Message    string    `json:"message"`
	Detail     string    `json:"detail"`
	HTTPStatus int       `json:"httpStatus"`
	VendorCode string    `json:"vendorCode"`
	RequestID  string    `json:"requestId"`
	JobID      string    `json:"jobId"`
	Count      int       `json:"count"`
}

type DomainEntry struct {
	ID          string `json:"id"`
	Hostname    string `json:"hostname"`
	DisplayName string `json:"displayName"`
	Enabled     bool   `json:"enabled"`
	TestURL     string `json:"testUrl"`
}

type RangeSnapshot struct {
	CIDRs     []string  `json:"cidrs"`
	FetchedAt time.Time `json:"fetchedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Digest    string    `json:"digest"`
}

type DNSRecord struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
	TTL   uint32 `json:"ttl"`
	CIDR  string `json:"cidr"`
}

type DNSObservation struct {
	Source     string      `json:"source"`
	ObservedAt time.Time   `json:"observedAt"`
	Records    []DNSRecord `json:"records"`
	Error      string      `json:"error"`
}

type DetectionResult struct {
	DomainID       string           `json:"domainId"`
	Hostname       string           `json:"hostname"`
	Classification string           `json:"classification"`
	ObservedAt     time.Time        `json:"observedAt"`
	ExpiresAt      time.Time        `json:"expiresAt"`
	Sources        []DNSObservation `json:"sources"`
	Candidates     []string         `json:"candidates"`
	Warnings       []string         `json:"warnings"`
}

type NetworkSnapshot struct {
	Fingerprint string    `json:"fingerprint"`
	Interfaces  []string  `json:"interfaces"`
	Routes      []string  `json:"routes"`
	Resolvers   []string  `json:"resolvers"`
	IPv6        bool      `json:"ipv6"`
	IPv6Reason  string    `json:"ipv6Reason"`
	ObservedAt  time.Time `json:"observedAt"`
	Error       string    `json:"error"`
}

type Sample struct {
	ActualIP            string  `json:"actualIp"`
	HTTPStatus          int     `json:"httpStatus"`
	Stage               string  `json:"stage"`
	Hostname            string  `json:"hostname"`
	Success             bool    `json:"success"`
	TCPMilliseconds     float64 `json:"tcpMilliseconds"`
	TLSMilliseconds     float64 `json:"tlsMilliseconds"`
	TTFBMilliseconds    float64 `json:"ttfbMilliseconds"`
	ElapsedMilliseconds float64 `json:"elapsedMilliseconds"`
	Mbps                float64 `json:"mbps"`
	Bytes               int64   `json:"bytes"`
	POP                 string  `json:"pop"`
	Error               string  `json:"error"`
}

type CandidateResult struct {
	POP                 string           `json:"pop"`
	ReferenceScores     []ReferenceScore `json:"referenceScores"`
	SmoothRounds        int              `json:"smoothRounds"`
	SmoothThresholdMbps float64          `json:"smoothThresholdMbps"`
	IP                  string           `json:"ip"`
	Family              int              `json:"family"`
	Baseline            bool             `json:"baseline"`
	Samples             []Sample         `json:"samples"`
	Successes           int              `json:"successes"`
	MedianMbps          float64          `json:"medianMbps"`
	MedianLatency       float64          `json:"medianLatency"`
}

type TestResult struct {
	BaselineService    bool              `json:"baselineService"`
	Complete           bool              `json:"complete"`
	Rejected           []CandidateResult `json:"rejected"`
	DNSClassification  string            `json:"dnsClassification"`
	Reference          *SharedReference  `json:"reference"`
	ReferenceReused    bool              `json:"referenceReused"`
	ReusedFrom         string            `json:"reusedFrom"`
	StrategyVersion    int               `json:"strategyVersion"`
	Service            string            `json:"service"`
	Mode               string            `json:"mode"`
	ProbeHost          string            `json:"probeHost"`
	SpeedHost          string            `json:"speedHost"`
	ScanCount          int               `json:"scanCount"`
	ID                 string            `json:"id"`
	DomainID           string            `json:"domainId"`
	Hostname           string            `json:"hostname"`
	NetworkFingerprint string            `json:"networkFingerprint"`
	SettingsRevision   string            `json:"settingsRevision"`
	ObservedAt         time.Time         `json:"observedAt"`
	ExpiresAt          time.Time         `json:"expiresAt"`
	Candidates         []CandidateResult `json:"candidates"`
	BestIPv4           string            `json:"bestIpv4"`
	BestIPv6           string            `json:"bestIpv6"`
	Bytes              int64             `json:"bytes"`
	Reason             string            `json:"reason"`
	Emergency          bool              `json:"emergency"`
	AutomaticEligible  bool              `json:"automaticEligible"`
}

type ReferenceScore struct {
	Source        string  `json:"source"`
	Route         string  `json:"route"`
	ActualIP      string  `json:"actualIp"`
	SpeedHost     string  `json:"speedHost"`
	SmoothRounds  int     `json:"smoothRounds"`
	ThresholdMbps float64 `json:"thresholdMbps"`
	MedianMbps    float64 `json:"medianMbps"`
	Successes     int     `json:"successes"`
	Rounds        int     `json:"rounds"`
}

type ReferenceSource struct {
	Name       string            `json:"name"`
	ProbeHost  string            `json:"probeHost"`
	SpeedHost  string            `json:"speedHost"`
	Candidates []CandidateResult `json:"candidates"`
}

type SharedReference struct {
	ID                 string            `json:"id"`
	Digest             string            `json:"digest"`
	StrategyVersion    int               `json:"strategyVersion"`
	NetworkFingerprint string            `json:"networkFingerprint"`
	RangesDigest       string            `json:"rangesDigest"`
	ProbeDigest        string            `json:"probeDigest"`
	ObservedAt         time.Time         `json:"observedAt"`
	ExpiresAt          time.Time         `json:"expiresAt"`
	Bytes              int64             `json:"bytes"`
	Sources            []ReferenceSource `json:"sources"`
}

type Rule struct {
	Hostname     string          `json:"hostname"`
	Do           int             `json:"do"`
	Status       int             `json:"status"`
	Via          string          `json:"via"`
	ViaV6        string          `json:"viaV6"`
	Group        int             `json:"group"`
	Comment      string          `json:"comment"`
	Safe         bool            `json:"safe"`
	UnsafeReason string          `json:"unsafeReason"`
	Raw          json.RawMessage `json:"raw,omitempty"`
}

func (r Rule) SemanticDigest() string {
	return Digest([]any{r.Hostname, r.Do, r.Status, r.Via, r.ViaV6, r.Group, r.Comment})
}

func (r Rule) ObservationDigest() string { return Digest(r) }

type Profile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Group struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Action bool   `json:"action"`
}

func RegularFolder(groups []Group, id int) bool {
	if id == 0 {
		return true
	}
	for _, g := range groups {
		if g.ID == id {
			return !g.Action
		}
	}
	return false
}

type PreviewItem struct {
	ForceWrite   bool             `json:"forceWrite"`
	OriginalDNS  *DetectionResult `json:"originalDns"`
	ID           string           `json:"id"`
	DomainID     string           `json:"domainId"`
	Hostname     string           `json:"hostname"`
	Operation    string           `json:"operation"`
	Before       *Rule            `json:"before"`
	After        *Rule            `json:"after"`
	BeforeDigest string           `json:"beforeDigest"`
	Applicable   bool             `json:"applicable"`
	Reason       string           `json:"reason"`
	Move         bool             `json:"move"`
	ResultID     string           `json:"resultId"`
	RestoreOf    string           `json:"restoreOf"`
}

type SyncPreview struct {
	ID                 string        `json:"id"`
	Kind               string        `json:"kind"`
	ProfileID          string        `json:"profileId"`
	CredentialRevision string        `json:"credentialRevision"`
	SettingsRevision   string        `json:"settingsRevision"`
	NetworkFingerprint string        `json:"networkFingerprint"`
	ExpiresAt          time.Time     `json:"expiresAt"`
	Items              []PreviewItem `json:"items"`
}

type JournalItem struct {
	WriteAccepted bool        `json:"writeAccepted"`
	Preview       PreviewItem `json:"preview"`
	State         string      `json:"state"`
	Message       string      `json:"message"`
	After         *Rule       `json:"after"`
}

type Job struct {
	ID                 string        `json:"id"`
	Kind               string        `json:"kind"`
	Trigger            string        `json:"trigger"`
	State              string        `json:"state"`
	StartedAt          time.Time     `json:"startedAt"`
	FinishedAt         time.Time     `json:"finishedAt"`
	Completed          int           `json:"completed"`
	ManagedCompleted   int           `json:"managedCompleted"`
	BaselinesCompleted int           `json:"baselinesCompleted"`
	Phase              string        `json:"phase"`
	EstimatedBytes     int64         `json:"estimatedBytes"`
	EstimatedSeconds   int64         `json:"estimatedSeconds"`
	Total              int           `json:"total"`
	ProbeCompleted     int           `json:"probeCompleted"`
	ProbeTotal         int           `json:"probeTotal"`
	CurrentHostname    string        `json:"currentHostname"`
	Bytes              int64         `json:"bytes"`
	Budget             int64         `json:"budget"`
	CancelRequested    bool          `json:"cancelRequested"`
	Message            string        `json:"message"`
	ProfileID          string        `json:"profileId"`
	CredentialRevision string        `json:"credentialRevision"`
	Items              []JournalItem `json:"items"`
	ResultIDs          []string      `json:"resultIds"`
}

type JobHistoryDeletion struct {
	Deleted  int `json:"deleted"`
	Retained int `json:"retained"`
}

func (j Job) Pending() bool {
	for _, i := range j.Items {
		if i.State == "write_started" || i.State == "unknown" {
			return true
		}
	}
	return false
}

func (j Job) Validate() error {
	if j.ID == "" || j.Kind == "" {
		return fmt.Errorf("作業缺少識別資訊")
	}
	return nil
}
