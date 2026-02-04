package bridge

import (
	"testing"

	"github.com/anthropics/feishu-codex-bridge/feishu"
)

func TestHelpCommand_UsesRichTextPost(t *testing.T) {
	b := &Bridge{
		config: Config{
			WorkingDir: ".",
		},
		feishuClient: &MockFeishuClient{},
	}

	b.handleFeishuMessageV2(&feishu.Message{
		ChatID:   "c1",
		ChatType: "p2p",
		MsgID:    "m1",
		MsgType:  "text",
		Content:  "/h",
	})

	m := b.feishuClient.(*MockFeishuClient)
	if len(m.SentMessages) == 0 {
		t.Fatalf("expected a reply")
	}
	last := m.SentMessages[len(m.SentMessages)-1]
	if !last.IsReply {
		t.Fatalf("expected reply message")
	}
	if !last.IsRich {
		t.Fatalf("expected rich text post reply")
	}
	if len(last.Content) == 0 {
		t.Fatalf("expected non-empty post content")
	}
}

func TestBuildHelpPost_NumberedLines(t *testing.T) {
	_, content := buildHelpPost()
	if len(content) < 6 {
		t.Fatalf("expected multiple lines")
	}
	// Line 1 should be a short header without extra wording and without bold.
	if got := textFromPostLine(content[0]); got != "可用命令：" {
		t.Fatalf("expected header line %q, got %q", "可用命令：", got)
	}
	if hasBoldStyle(content[0]) {
		t.Fatalf("expected header line to not be bold")
	}
	// Line 2 should start with "1)"
	if got := textFromPostLine(content[1]); len(got) < 2 || got[:2] != "1)" {
		t.Fatalf("expected line 2 to start with 1), got %q", got)
	}
}

func textFromPostLine(line []map[string]interface{}) string {
	if len(line) == 0 {
		return ""
	}
	out := ""
	for _, seg := range line {
		if seg == nil {
			continue
		}
		if tag, _ := seg["tag"].(string); tag != "text" {
			continue
		}
		if s, _ := seg["text"].(string); s != "" {
			out += s
		}
	}
	return out
}

func hasBoldStyle(line []map[string]interface{}) bool {
	for _, seg := range line {
		if seg == nil {
			continue
		}
		if tag, _ := seg["tag"].(string); tag != "text" {
			continue
		}
		raw, ok := seg["style"]
		if !ok {
			continue
		}
		styles, ok := raw.([]string)
		if !ok {
			continue
		}
		for _, s := range styles {
			if s == "bold" {
				return true
			}
		}
	}
	return false
}
