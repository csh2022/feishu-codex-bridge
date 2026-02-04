package bridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/feishu-codex-bridge/codex"
	"github.com/anthropics/feishu-codex-bridge/feishu"
	"github.com/anthropics/feishu-codex-bridge/session"
)

func TestQueueCommand_AddsDoneReaction(t *testing.T) {
	m := &MockFeishuClient{}
	b := &Bridge{
		config:       Config{},
		feishuClient: m,
		chatQueues:   make(map[string]*chatQueue),
		chatStates:   make(map[string]*ChatState),
	}

	b.handleFeishuMessageV2(&feishu.Message{
		ChatID:   "c1",
		ChatType: "p2p",
		MsgID:    "om1",
		MsgType:  "text",
		Content:  "/q",
	})

	found := false
	for _, r := range m.Reactions {
		if r.MessageID == "om1" && r.EmojiType == "DONE" && !r.IsRemove {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected DONE reaction to be added")
	}
}

func TestClearCommand_ReplyTextIsShort(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := session.NewStore(filepath.Join(tmpDir, "sessions.db"), 60, -1)
	if err != nil {
		t.Fatalf("failed to create session store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	m := &MockFeishuClient{}
	b := &Bridge{
		config:       Config{},
		feishuClient: m,
		chatQueues:   make(map[string]*chatQueue),
		chatStates:   make(map[string]*ChatState),
		sessionStore: store,
	}

	b.handleFeishuMessageV2(&feishu.Message{
		ChatID:   "c1",
		ChatType: "p2p",
		MsgID:    "om1",
		MsgType:  "text",
		Content:  "/c",
	})

	reply := findReplyText(m, "om1")
	if reply == "" {
		t.Fatalf("expected a reply")
	}
	if reply != "✅ 已清空当前会话上下文" {
		t.Fatalf("unexpected reply text: %q", reply)
	}
}

func TestSwitchDirCommand_ReplyTextIsNewFormat(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := session.NewStore(filepath.Join(tmpDir, "sessions.db"), 60, -1)
	if err != nil {
		t.Fatalf("failed to create session store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	m := &MockFeishuClient{}
	b := &Bridge{
		config: Config{
			WorkingDir:      tmpDir,
			SessionDBPath:   filepath.Join(tmpDir, "sessions.db"),
			SessionIdleMin:  60,
			SessionResetHr:  -1,
			FeishuAppID:     "test",
			FeishuAppSecret: "test",
		},
		feishuClient: m,
		chatQueues:   make(map[string]*chatQueue),
		chatStates:   make(map[string]*ChatState),
		sessionStore: store,
		codexClient:  codex.NewClient(tmpDir, "gpt-5.2-codex"),
		ctx:          context.Background(),
	}

	newDir := filepath.Join(tmpDir, "new")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	b.handleFeishuMessageV2(&feishu.Message{
		ChatID:   "c1",
		ChatType: "p2p",
		MsgID:    "om1",
		MsgType:  "text",
		Content:  "/cd " + newDir,
	})

	reply := findReplyText(m, "om1")
	want := "✅ 已切换到新的工作目录：" + newDir
	if reply != want {
		t.Fatalf("unexpected reply text: %q, want %q", reply, want)
	}
}

func TestPwdCommand_ReplyTextHasNoEmojiPrefix(t *testing.T) {
	m := &MockFeishuClient{}
	b := &Bridge{
		config: Config{
			WorkingDir: "/tmp",
		},
		feishuClient: m,
		chatQueues:   make(map[string]*chatQueue),
		chatStates:   make(map[string]*ChatState),
	}

	b.handleFeishuMessageV2(&feishu.Message{
		ChatID:   "c1",
		ChatType: "p2p",
		MsgID:    "om1",
		MsgType:  "text",
		Content:  "/pwd",
	})

	reply := findReplyText(m, "om1")
	if reply == "" {
		t.Fatalf("expected a reply")
	}
	if strings.HasPrefix(reply, "📁") {
		t.Fatalf("expected no emoji prefix, got %q", reply)
	}
	if !strings.HasPrefix(reply, "当前工作目录：") {
		t.Fatalf("unexpected reply prefix: %q", reply)
	}
}

func findReplyText(m *MockFeishuClient, msgID string) string {
	for _, sm := range m.SentMessages {
		if sm.IsReply && sm.MsgID == msgID {
			return sm.Text
		}
	}
	return ""
}
