package settings

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/secrets"
	"github.com/bestpika/go-fastly-ctrld/internal/storage"
)

type state struct {
	Settings domain.Settings      `json:"settings"`
	Domains  []domain.DomainEntry `json:"domains"`
}
type Patch struct {
	ForceWrite *bool                `json:"forceWrite,omitempty"`
	Theme      string               `json:"theme"`
	Probe      domain.ProbeSettings `json:"probe"`
}
type CredentialStatus struct {
	Saved     bool   `json:"saved"`
	Available bool   `json:"available"`
	Message   string `json:"message"`
}

type Manager struct {
	mu    sync.Mutex
	store *storage.Store
	vault secrets.Vault
	state state
	Guard func(string) (func(), error)
}

func New(store *storage.Store, vault secrets.Vault) (*Manager, error) {
	m := &Manager{store: store, vault: vault}
	err := store.Read("state.json", &m.state)
	if errors.Is(err, storage.ErrMissing) {
		m.state = state{Settings: domain.DefaultSettings(), Domains: []domain.DomainEntry{}}
		if err = store.Write("state.json", m.state); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, domain.Fail("SCHEMA_UNSUPPORTED", "設定未知或損毀；未覆寫原資料")
	}
	if m.state.Settings.Probe.NoResourceMode == "" {
		m.state.Settings.Probe.NoResourceMode = "latency"
	}
	if m.state.Settings.Probe.ScanMode == "" {
		m.state.Settings.Probe.ScanMode = "near"
	}
	if err = m.state.Settings.Validate(); err != nil {
		return nil, domain.Fail("SCHEMA_UNSUPPORTED", "保存的設定不符合安全條件")
	}
	seen := map[string]bool{}
	ids := map[string]bool{}
	for _, d := range m.state.Domains {
		h, err := domain.Normalize(d.Hostname)
		if err != nil || h != d.Hostname || domain.ValidateURL(h, d.TestURL) != nil || d.ID == "" || seen[h] || ids[d.ID] {
			return nil, domain.Fail("SCHEMA_UNSUPPORTED", "保存的網域清單不正確")
		}
		seen[h] = true
		ids[d.ID] = true
	}
	if m.state.Settings.PresetVersion > domain.PresetVersion {
		return nil, domain.Fail("SCHEMA_UNSUPPORTED", "設定策略版本較新；保留原檔，不降版覆寫")
	}
	canonical := domain.FixedProbe(m.state.Settings.Probe)
	if m.state.Settings.PresetVersion < domain.PresetVersion {
		// 舊版 shared 與 IPv6 並非使用者選用，不繼承隱含擴充授權。
		canonical.NoResourceMode = "latency"
		canonical.EnableIPv6 = false
		canonical.Services = []string{"github", "twitch", "x"}
		m.state.Settings.Automation.AllowSharedReference = false
	}
	if m.state.Settings.PresetVersion != domain.PresetVersion || domain.Digest(canonical) != domain.Digest(m.state.Settings.Probe) || m.state.Settings.Automation.StabilitySeconds != 30 || m.state.Settings.Automation.CooldownSeconds != 900 {
		m.state.Settings.Probe = canonical
		m.state.Settings.PresetVersion = domain.PresetVersion
		m.state.Settings.Automation.StabilitySeconds = 30
		m.state.Settings.Automation.CooldownSeconds = 900
		m.state.Settings.Revision = domain.ID()
		invalidate(&m.state)
		if err := m.store.Write("state.json", m.state); err != nil {
			return nil, domain.Fail("STORAGE_ERROR", "固定策略遷移無法保存；未啟用自動執行")
		}
	}
	return m, nil
}

func clone[T any](v T) T {
	b, _ := json.Marshal(v)
	var result T
	_ = json.Unmarshal(b, &result)
	return result
}

func (m *Manager) Snapshot() (domain.Settings, []domain.DomainEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := clone(m.state)
	return s.Settings, s.Domains
}

func (m *Manager) mutate(expected string, internal bool, operation string, change func(*state) error, effect func() error) error {
	if m.Guard != nil {
		unlock, err := m.Guard(operation)
		if err != nil {
			return err
		}
		defer unlock()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !internal && expected != m.state.Settings.Revision {
		return domain.Fail("CONFLICT", "設定已變更，請重新讀取後確認")
	}
	s := clone(m.state)
	if err := change(&s); err != nil {
		return err
	}
	s.Settings.Revision = domain.ID()
	if err := s.Settings.Validate(); err != nil {
		return err
	}
	if err := m.store.Write("state.json", s); err != nil {
		return domain.Fail("STORAGE_ERROR", "設定無法安全保存，未繼續操作")
	}
	m.state = s
	if effect != nil {
		return effect()
	}
	return nil
}

func invalidate(s *state) {
	s.Settings.Automation.Enabled = false
	s.Settings.Automation.Grants = []domain.Grant{}
}

func (m *Manager) Update(expected string, p Patch) error {
	p.Probe = domain.FixedProbe(p.Probe)
	return m.mutate(expected, false, "settings", func(s *state) error {
		if domain.Digest(s.Settings.Probe) != domain.Digest(p.Probe) || p.ForceWrite != nil && *p.ForceWrite != s.Settings.ForceWrite {
			invalidate(s)
		}
		if p.ForceWrite != nil {
			s.Settings.ForceWrite = *p.ForceWrite
		}
		s.Settings.Theme = p.Theme
		s.Settings.Probe = p.Probe
		return nil
	}, nil)
}

func (m *Manager) SelectProfile(expected, id string) error {
	return m.mutate(expected, false, "profile", func(s *state) error {
		s.Settings.ProfileID = id
		s.Settings.DestinationFolderID = 0
		invalidate(s)
		return nil
	}, nil)
}

func (m *Manager) SelectFolder(expected string, id int) error {
	return m.mutate(expected, false, "folder", func(s *state) error {
		if id < 0 {
			return domain.Fail("INVALID_INPUT", "資料夾無效")
		}
		s.Settings.DestinationFolderID = id
		invalidate(s)
		return nil
	}, nil)
}

func (m *Manager) SetToken(token string) error {
	if err := secrets.Validate(token); err != nil {
		return err
	}
	return m.mutate("", true, "credential", func(s *state) error { s.Settings.CredentialRevision = domain.ID(); invalidate(s); return nil }, func() error { return m.vault.Set(token) })
}

func (m *Manager) DeleteToken() error {
	return m.mutate("", true, "credential", func(s *state) error { s.Settings.CredentialRevision = domain.ID(); invalidate(s); return nil }, m.vault.Delete)
}

func (m *Manager) Token() (string, error) { return m.vault.Get() }

func (m *Manager) CredentialStatus() CredentialStatus {
	token, err := m.vault.Get()
	if err != nil {
		return CredentialStatus{Message: "系統金鑰庫不可用；不使用明文備援"}
	}
	return CredentialStatus{Saved: token != "", Available: true}
}

func (m *Manager) Import(p domain.ImportPreview) error {
	return m.mutate(p.Revision, false, "domains", func(s *state) error {
		seen := map[string]int{}
		for index, d := range s.Domains {
			seen[d.Hostname] = index
		}
		for _, r := range p.Rows {
			index, exists := seen[r.Entry.Hostname]
			if r.State == "valid" && !exists {
				s.Domains = append(s.Domains, r.Entry)
				seen[r.Entry.Hostname] = len(s.Domains) - 1
			} else if exists && (r.State == "update" || r.State == "replace") {
				s.Domains[index].TestURL = r.Entry.TestURL
			}
		}
		invalidate(s)
		if len(s.Domains) > 5000 {
			return domain.Fail("INVALID_INPUT", "網域清單最多 5,000 個")
		}
		return nil
	}, nil)
}

func (m *Manager) UpdateDomain(expected, id string, enabled bool, testURL string) error {
	return m.mutate(expected, false, "domains", func(s *state) error {
		for i := range s.Domains {
			d := &s.Domains[i]
			if d.ID != id {
				continue
			}
			if err := domain.ValidateURL(d.Hostname, testURL); err != nil {
				return err
			}
			d.Enabled = enabled
			d.TestURL = testURL
			invalidate(s)
			return nil
		}
		return domain.Fail("INVALID_INPUT", "找不到網域")
	}, nil)
}

func (m *Manager) RemoveDomain(expected, id string) error {
	return m.mutate(expected, false, "domains", func(s *state) error {
		for i, d := range s.Domains {
			if d.ID == id {
				s.Domains = append(s.Domains[:i], s.Domains[i+1:]...)
				invalidate(s)
				return nil
			}
		}
		return domain.Fail("INVALID_INPUT", "找不到網域")
	}, nil)
}

func (m *Manager) Configure(expected string, a domain.Automation) error {
	a.StabilitySeconds = 30
	a.CooldownSeconds = 900
	return m.mutate(expected, false, "automation", func(s *state) error {
		if a.Enabled {
			allowed := map[string]bool{}
			for _, d := range s.Domains {
				if d.Enabled {
					allowed[d.ID] = true
				}
			}
			seen := map[string]bool{}
			for _, g := range a.Grants {
				if !allowed[g.DomainID] || seen[g.DomainID] {
					return domain.Fail("INVALID_INPUT", "自動授權包含不存在、停用或重複網域")
				}
				seen[g.DomainID] = true
			}
			if a.ProfileID != s.Settings.ProfileID || a.CredentialRevision != s.Settings.CredentialRevision {
				return domain.Fail("CONFLICT", "自動授權與目前帳號不一致")
			}
		}
		a.LastRun = s.Settings.Automation.LastRun
		a.LastChanged = s.Settings.Automation.LastChanged
		s.Settings.Automation = a
		return nil
	}, nil)
}

func (m *Manager) Pause() error {
	return m.mutate("", true, "pause", func(s *state) error { s.Settings.Automation.Enabled = false; return nil }, nil)
}

func (m *Manager) LaunchAtLogin(enabled bool, effect func() error) error {
	return m.mutate("", true, "launch", func(s *state) error { s.Settings.LaunchAtLogin = enabled; return nil }, effect)
}

func (m *Manager) RecordAutomation(run domain.Automation, cursor int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := clone(m.state)
	s.Settings.Automation.LastRun = run.LastRun
	s.Settings.Automation.LastChanged = run.LastChanged
	s.Settings.Cursor = cursor
	if err := m.store.Write("state.json", s); err != nil {
		return err
	}
	m.state = s
	return nil
}
