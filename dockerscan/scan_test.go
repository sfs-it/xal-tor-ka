package dockerscan

import "testing"

// A scan wider than the cap must SAY so: the page reports the range it really probed,
// otherwise a service on a high port looks absent when it was simply never dialled.
func TestClampRangeReportsNarrowing(t *testing.T) {
	if f, to, capped := ClampRange(1, 20000); !capped || f != 1 || to != MaxScanPorts {
		t.Errorf("wide range: got %d-%d capped=%v", f, to, capped)
	}
	if f, to, capped := ClampRange(8700, 8800); capped || f != 8700 || to != 8800 {
		t.Errorf("narrow range must pass through: got %d-%d capped=%v", f, to, capped)
	}
	// Above the last port the ceiling applies first, then the width cap.
	if _, to, capped := ClampRange(1, 99999); to != MaxScanPorts || !capped {
		t.Errorf("beyond 65535 the width cap still wins: got %d capped=%v", to, capped)
	}
	// A narrow range near the ceiling is clamped to 65535 and needs no narrowing.
	if _, to, capped := ClampRange(65000, 99999); to != 65535 || capped {
		t.Errorf("high narrow range: got %d capped=%v", to, capped)
	}
}
