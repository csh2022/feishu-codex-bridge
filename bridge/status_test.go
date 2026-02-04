package bridge

import (
	"strings"
	"testing"
)

func TestFormatStatus_Idle(t *testing.T) {
	b := &Bridge{
		chatQueues: make(map[string]*chatQueue),
		chatStates: make(map[string]*ChatState),
	}
	out := b.formatStatus("c1")
	if !strings.Contains(out, "状态：空闲") {
		t.Fatalf("unexpected output: %q", out)
	}
	if !strings.Contains(out, "待处理：0") {
		t.Fatalf("unexpected output: %q", out)
	}
}
