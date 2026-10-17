package app

import (
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
)

type scheduleState struct {
	fingerprint string
	lastTick    time.Time
	stableSince time.Time
	pending     bool
}

func (s *scheduleState) observe(now time.Time, fingerprint string) {
	if s.lastTick.IsZero() || now.Before(s.lastTick) || now.Sub(s.lastTick) > 30*time.Second || s.fingerprint != fingerprint {
		s.fingerprint = fingerprint
		s.stableSince = now
		s.pending = true
	}
	s.lastTick = now
}

func (s *scheduleState) ready(now time.Time, a domain.Automation, networkError string) bool {
	return a.Enabled && networkError == "" && !s.stableSince.IsZero() && now.Sub(s.stableSince) >= time.Duration(a.StabilitySeconds)*time.Second && (s.pending || a.LastRun.IsZero() || now.Sub(a.LastRun) >= time.Duration(a.IntervalSeconds)*time.Second)
}

func limitAutomatic(p *domain.SyncPreview, a domain.Automation, results map[string]domain.TestResult, now time.Time) {
	grants := map[string]domain.Grant{}
	for _, g := range a.Grants {
		grants[g.DomainID] = g
	}
	for index := range p.Items {
		i := &p.Items[index]
		g, ok := grants[i.DomainID]
		r := results[i.DomainID]
		if !ok || !r.AutomaticEligible || (r.Mode == "shared-reference" && !a.AllowSharedReference) || (i.Before != nil && !g.Overwrite) || (i.Move && !g.Move) {
			i.Applicable = false
			i.Reason = "不在完整自動授權範圍"
		}
		last := a.LastChanged[i.DomainID]
		if !i.ForceWrite && !last.IsZero() && now.Sub(last) < time.Duration(a.CooldownSeconds)*time.Second && !r.Emergency {
			i.Applicable = false
			i.Reason = "切換冷卻時間尚未結束"
		}
	}
}
