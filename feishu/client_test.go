package feishu

import (
	"testing"
)

func TestNewClient(t *testing.T) {
	client := NewClient("app_id", "app_secret")

	if client.appID != "app_id" {
		t.Errorf("appID mismatch: got %q, want %q", client.appID, "app_id")
	}
	if client.appSecret != "app_secret" {
		t.Errorf("appSecret mismatch: got %q, want %q", client.appSecret, "app_secret")
	}
	if client.downloadDir != "/tmp/feishu-images" {
		t.Errorf("downloadDir mismatch: got %q", client.downloadDir)
	}
}

func TestSetDownloadDir(t *testing.T) {
	client := NewClient("app_id", "app_secret")
	client.SetDownloadDir("/custom/path")

	if client.downloadDir != "/custom/path" {
		t.Errorf("downloadDir mismatch: got %q", client.downloadDir)
	}
}

func TestOnMessage(t *testing.T) {
	client := NewClient("app_id", "app_secret")

	handler := func(msg *Message) {
		// Handler is set
	}

	client.OnMessage(handler)

	if client.onMessage == nil {
		t.Error("onMessage handler not set")
	}
}

func TestParseTextContent(t *testing.T) {
	client := NewClient("app_id", "app_secret")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple text",
			input:    `{"text": "Hello World"}`,
			expected: "Hello World",
		},
		{
			name:     "text with unicode",
			input:    `{"text": "你好世界"}`,
			expected: "你好世界",
		},
		{
			name:     "empty text",
			input:    `{"text": ""}`,
			expected: "",
		},
		{
			name:     "invalid json",
			input:    `invalid json`,
			expected: "",
		},
		{
			name:     "missing text field",
			input:    `{"content": "test"}`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.parseTextContent(tt.input)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestParseImageContent(t *testing.T) {
	client := NewClient("app_id", "app_secret")

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "valid image",
			input:    `{"image_key": "img_key_123"}`,
			expected: []string{"img_key_123"},
		},
		{
			name:     "empty image key",
			input:    `{"image_key": ""}`,
			expected: nil,
		},
		{
			name:     "invalid json",
			input:    `invalid`,
			expected: nil,
		},
		{
			name:     "missing image_key",
			input:    `{"other": "value"}`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.parseImageContent(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("length mismatch: got %d, want %d", len(result), len(tt.expected))
				return
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("element %d: got %q, want %q", i, result[i], tt.expected[i])
				}
			}
		})
	}
}

func TestParsePostContent(t *testing.T) {
	client := NewClient("app_id", "app_secret")

	tests := []struct {
		name           string
		input          string
		expectedText   string
		expectedImages []string
	}{
		{
			name: "simple post",
			input: `{
				"title": "Title",
				"content": [
					[{"tag": "text", "text": "Hello "}],
					[{"tag": "text", "text": "World"}]
				]
			}`,
			expectedText:   "Title\nHello \nWorld",
			expectedImages: nil,
		},
		{
			name: "post with image",
			input: `{
				"title": "",
				"content": [
					[{"tag": "text", "text": "Check this: "}],
					[{"tag": "img", "image_key": "img_123"}]
				]
			}`,
			expectedText:   "Check this: ",
			expectedImages: []string{"img_123"},
		},
		{
			name: "mixed content",
			input: `{
				"title": "Report",
				"content": [
					[{"tag": "text", "text": "Line 1"}, {"tag": "text", "text": " continued"}],
					[{"tag": "img", "image_key": "img_a"}],
					[{"tag": "text", "text": "Line 2"}],
					[{"tag": "img", "image_key": "img_b"}]
				]
			}`,
			expectedText:   "Report\nLine 1 continued\nLine 2",
			expectedImages: []string{"img_a", "img_b"},
		},
		{
			name:           "invalid json",
			input:          `invalid`,
			expectedText:   "",
			expectedImages: nil,
		},
		{
			name:           "empty content",
			input:          `{"title": "", "content": []}`,
			expectedText:   "",
			expectedImages: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, images := client.parsePostContent(tt.input)
			if text != tt.expectedText {
				t.Errorf("text mismatch: got %q, want %q", text, tt.expectedText)
			}
			if len(images) != len(tt.expectedImages) {
				t.Errorf("images length mismatch: got %d, want %d", len(images), len(tt.expectedImages))
				return
			}
			for i := range images {
				if images[i] != tt.expectedImages[i] {
					t.Errorf("image %d: got %q, want %q", i, images[i], tt.expectedImages[i])
				}
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		n        int
		expected string
	}{
		{"Hello", 10, "Hello"},
		{"Hello World", 5, "Hello..."},
		{"", 5, ""},
		{"Hi", 2, "Hi"},
		{"Hello", 0, "..."},
	}

	for _, tt := range tests {
		result := truncate(tt.input, tt.n)
		if result != tt.expected {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.n, result, tt.expected)
		}
	}
}

func TestJoinStrings(t *testing.T) {
	tests := []struct {
		parts    []string
		sep      string
		expected string
	}{
		{[]string{"a", "b", "c"}, ", ", "a, b, c"},
		{[]string{"a"}, ", ", "a"},
		{[]string{}, ", ", ""},
		{[]string{"hello", "world"}, "\n", "hello\nworld"},
		{[]string{"x", "y"}, "", "xy"},
	}

	for _, tt := range tests {
		result := joinStrings(tt.parts, tt.sep)
		if result != tt.expected {
			t.Errorf("joinStrings(%v, %q) = %q, want %q", tt.parts, tt.sep, result, tt.expected)
		}
	}
}

func TestMessage(t *testing.T) {
	msg := &Message{
		ChatID:    "chat_123",
		MsgID:     "msg_456",
		MsgType:   "text",
		Content:   "Hello",
		ImageKeys: []string{"img_1", "img_2"},
	}

	if msg.ChatID != "chat_123" {
		t.Errorf("ChatID mismatch")
	}
	if msg.MsgID != "msg_456" {
		t.Errorf("MsgID mismatch")
	}
	if msg.MsgType != "text" {
		t.Errorf("MsgType mismatch")
	}
	if msg.Content != "Hello" {
		t.Errorf("Content mismatch")
	}
	if len(msg.ImageKeys) != 2 {
		t.Errorf("ImageKeys length mismatch")
	}
}

func TestStop(t *testing.T) {
	client := NewClient("app_id", "app_secret")

	// Should not panic when cancel is nil
	client.Stop()

	// Should not panic after Stop is called multiple times
	client.Stop()
}

func TestSender(t *testing.T) {
	sender := &Sender{
		SenderID:   "user_123",
		SenderType: "user",
		TenantKey:  "tenant_456",
	}

	if sender.SenderID != "user_123" {
		t.Error("SenderID mismatch")
	}
	if sender.SenderType != "user" {
		t.Error("SenderType mismatch")
	}
	if sender.TenantKey != "tenant_456" {
		t.Error("TenantKey mismatch")
	}
}

func TestChatMember(t *testing.T) {
	member := &ChatMember{
		MemberID:   "member_123",
		MemberType: "user",
		Name:       "Test User",
	}

	if member.MemberID != "member_123" {
		t.Error("MemberID mismatch")
	}
	if member.MemberType != "user" {
		t.Error("MemberType mismatch")
	}
	if member.Name != "Test User" {
		t.Error("Name mismatch")
	}
}

func TestChatInfo(t *testing.T) {
	info := &ChatInfo{
		ChatID:      "chat_123",
		Name:        "Test Group",
		Description: "A test group",
		ChatType:    "group",
		OwnerID:     "owner_456",
		MemberCount: 10,
	}

	if info.ChatID != "chat_123" {
		t.Error("ChatID mismatch")
	}
	if info.Name != "Test Group" {
		t.Error("Name mismatch")
	}
	if info.MemberCount != 10 {
		t.Error("MemberCount mismatch")
	}
}

func TestHistoryMessage(t *testing.T) {
	msg := &HistoryMessage{
		MsgID:      "msg_123",
		MsgType:    "text",
		Content:    `{"text": "Hello"}`,
		CreateTime: "1234567890",
		Sender: &Sender{
			SenderID:   "user_456",
			SenderType: "user",
		},
	}

	if msg.MsgID != "msg_123" {
		t.Error("MsgID mismatch")
	}
	if msg.Sender == nil {
		t.Error("Sender should not be nil")
	}
	if msg.Sender.SenderID != "user_456" {
		t.Error("Sender ID mismatch")
	}
}

func TestFormatHistoryAsContext(t *testing.T) {
	messages := []*HistoryMessage{
		{
			MsgID:   "msg_3",
			MsgType: "text",
			Content: `{"text": "Third message"}`,
			Sender:  &Sender{SenderType: "user"},
		},
		{
			MsgID:   "msg_2",
			MsgType: "text",
			Content: `{"text": "Second message"}`,
			Sender:  &Sender{SenderType: "bot"},
		},
		{
			MsgID:   "msg_1",
			MsgType: "text",
			Content: `{"text": "First message"}`,
			Sender:  &Sender{SenderType: "user"},
		},
	}

	// Test normal formatting
	result := FormatHistoryAsContext(messages, 0)
	if result == "" {
		t.Error("Result should not be empty")
	}

	// Test with max limit
	result = FormatHistoryAsContext(messages, 2)
	if result == "" {
		t.Error("Result should not be empty with limit")
	}

	// Test empty messages
	result = FormatHistoryAsContext(nil, 0)
	if result != "" {
		t.Error("Empty messages should return empty string")
	}

	result = FormatHistoryAsContext([]*HistoryMessage{}, 0)
	if result != "" {
		t.Error("Empty slice should return empty string")
	}
}

func TestMessageWithSenderAndMentions(t *testing.T) {
	msg := &Message{
		ChatID:   "chat_123",
		MsgID:    "msg_456",
		MsgType:  "text",
		ChatType: "group",
		Content:  "Hello @bot",
		Sender: &Sender{
			SenderID:   "user_789",
			SenderType: "user",
		},
		Mentions: []string{"bot_id_1", "user_id_2"},
	}

	if msg.ChatType != "group" {
		t.Error("ChatType mismatch")
	}
	if msg.Sender == nil {
		t.Error("Sender should not be nil")
	}
	if len(msg.Mentions) != 2 {
		t.Errorf("Mentions length mismatch: got %d, want 2", len(msg.Mentions))
	}
}
