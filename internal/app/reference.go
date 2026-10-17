package app

import (
	"time"

	"github.com/bestpika/go-fastly-ctrld/internal/domain"
	"github.com/bestpika/go-fastly-ctrld/internal/probe"
)

func (c *Core) validateReference(result domain.TestResult, s domain.Settings, r domain.RangeSnapshot, n domain.NetworkSnapshot) error {
	if result.Mode != "shared-reference" {
		return nil
	}
	if result.Reference == nil {
		return domain.Fail("PREVIEW_EXPIRED", "共享證據缺漏")
	}
	var saved domain.SharedReference
	if c.cache.Read("references/"+probe.ReferenceKey(s, r, n)+".json", &saved) != nil || saved.ID != result.Reference.ID || saved.Digest != result.Reference.Digest || !probe.ValidReference(saved, s, r, n, time.Now()) {
		return domain.Fail("PREVIEW_EXPIRED", "共享參考已變更或不完整")
	}
	return nil
}
