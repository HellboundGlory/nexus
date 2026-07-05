package quality

import (
	"testing"

	"github.com/hellboundg/nexus/internal/core/store"
)

func upProfile(upgrade bool) store.QualityProfile {
	// order: WEBDL-720p(7) < Bluray-1080p(9); cutoff = Bluray-1080p
	return store.QualityProfile{
		Name:            "HD",
		CutoffQualityID: 9,
		UpgradeAllowed:  upgrade,
		Items: []store.QualityProfileItem{
			{QualityID: 7, Allowed: true},
			{QualityID: 9, Allowed: true},
		},
	}
}

func TestIsUpgrade(t *testing.T) {
	p := upProfile(true)
	if !IsUpgrade(9, 7, p) {
		t.Fatal("Bluray-1080p over WEBDL-720p should be an upgrade")
	}
	if IsUpgrade(7, 9, p) {
		t.Fatal("lower quality is not an upgrade")
	}
	if IsUpgrade(9, 9, p) {
		t.Fatal("same quality is not an upgrade")
	}
	// existing already at cutoff rank -> no upgrade
	if IsUpgrade(9, 9, p) {
		t.Fatal("at cutoff, no upgrade")
	}
	// upgrades disabled
	if IsUpgrade(9, 7, upProfile(false)) {
		t.Fatal("upgrades disabled -> never an upgrade")
	}
	// existing not in profile ranks below everything -> upgrade to any allowed
	if !IsUpgrade(7, 999, p) {
		t.Fatal("any allowed quality upgrades an unknown/absent existing quality")
	}
}
