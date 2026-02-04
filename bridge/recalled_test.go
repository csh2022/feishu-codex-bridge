package bridge

import "testing"

func TestRecalled_MarkAndClear(t *testing.T) {
	b := &Bridge{
		recalled:    make(map[string]map[string]struct{}),
		recalledAll: make(map[string]struct{}),
	}

	if b.isRecalled("c1", "m1") {
		t.Fatalf("expected not recalled yet")
	}

	b.markRecalled("c1", "m1")
	if !b.isRecalled("c1", "m1") {
		t.Fatalf("expected recalled")
	}

	b.clearRecalled("c1", "m1")
	if b.isRecalled("c1", "m1") {
		t.Fatalf("expected recalled to be cleared")
	}
}

func TestRecalled_GlobalMark(t *testing.T) {
	b := &Bridge{
		recalled:    make(map[string]map[string]struct{}),
		recalledAll: make(map[string]struct{}),
	}

	b.markRecalled("", "m1")
	if !b.isRecalled("c_any", "m1") {
		t.Fatalf("expected globally recalled")
	}
	b.clearRecalled("", "m1")
	if b.isRecalled("c_any", "m1") {
		t.Fatalf("expected global recall to be cleared")
	}
}
