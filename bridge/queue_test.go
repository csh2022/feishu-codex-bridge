package bridge

import (
	"strings"
	"testing"

	"github.com/anthropics/feishu-codex-bridge/feishu"
)

func TestFormatQueueStatus_OnlyShowsPendingCount(t *testing.T) {
	b := &Bridge{
		chatQueues: make(map[string]*chatQueue),
		chatStates: make(map[string]*ChatState),
	}
	chatID := "c1"

	q := &chatQueue{
		ch: make(chan *feishu.Message, 10),
		pending: []*feishu.Message{
			{ChatID: chatID, MsgID: "m1", Content: "a"},
			{ChatID: chatID, MsgID: "m2", Content: "b"},
		},
	}
	b.chatQueues[chatID] = q

	out := b.formatQueueStatus(chatID)
	if !strings.Contains(out, "待处理：2") {
		t.Fatalf("expected pending count, got %q", out)
	}
	if strings.Contains(out, "正在处理") {
		t.Fatalf("did not expect processing line, got %q", out)
	}
	if strings.Contains(out, "1)") || strings.Contains(out, "2)") {
		t.Fatalf("did not expect item list, got %q", out)
	}
}
