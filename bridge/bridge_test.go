package bridge

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/anthropics/feishu-codex-bridge/codex"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		n        int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc..."},
	}

	for _, tt := range tests {
		result := truncate(tt.input, tt.n)
		if result != tt.expected {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.n, result, tt.expected)
		}
	}
}

func TestChatState(t *testing.T) {
	state := &ChatState{}

	// Test initial state
	if state.Processing {
		t.Error("Initial Processing should be false")
	}
	if state.ThreadID != "" {
		t.Error("Initial ThreadID should be empty")
	}
	if state.MsgID != "" {
		t.Error("Initial MsgID should be empty")
	}

	// Test setting values
	state.ThreadID = "test-thread"
	state.TurnID = "test-turn"
	state.MsgID = "test-msg"
	state.Processing = true
	state.Buffer.WriteString("Hello ")
	state.Buffer.WriteString("World")

	if state.ThreadID != "test-thread" {
		t.Errorf("ThreadID mismatch: got %v", state.ThreadID)
	}
	if state.MsgID != "test-msg" {
		t.Errorf("MsgID mismatch: got %v", state.MsgID)
	}
	if state.Buffer.String() != "Hello World" {
		t.Errorf("Buffer mismatch: got %v", state.Buffer.String())
	}

	// Test reset
	state.Buffer.Reset()
	if state.Buffer.String() != "" {
		t.Error("Buffer should be empty after reset")
	}
}

func TestConfig(t *testing.T) {
	config := Config{
		FeishuAppID:     "test-app-id",
		FeishuAppSecret: "test-secret",
		WorkingDir:      "/tmp/test",
		CodexModel:      "claude-sonnet-4",
		SessionDBPath:   "/tmp/sessions.db",
		SessionIdleMin:  60,
		SessionResetHr:  4,
		Debug:           true,
	}

	if config.FeishuAppID != "test-app-id" {
		t.Errorf("FeishuAppID mismatch: got %v", config.FeishuAppID)
	}
	if config.SessionIdleMin != 60 {
		t.Errorf("SessionIdleMin mismatch: got %v", config.SessionIdleMin)
	}
	if !config.Debug {
		t.Error("Debug should be true")
	}
}

func TestNew(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	config := Config{
		FeishuAppID:     "test-app-id",
		FeishuAppSecret: "test-secret",
		WorkingDir:      tmpDir,
		CodexModel:      "",
		SessionDBPath:   dbPath,
		SessionIdleMin:  60,
		SessionResetHr:  -1,
	}

	bridge, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create bridge: %v", err)
	}

	if bridge.config.FeishuAppID != "test-app-id" {
		t.Error("Config not set correctly")
	}
	if bridge.feishuClient == nil {
		t.Error("Feishu client not initialized")
	}
	if bridge.codexClient == nil {
		t.Error("Codex client not initialized")
	}
	if bridge.sessionStore == nil {
		t.Error("Session store not initialized")
	}
	if bridge.chatStates == nil {
		t.Error("ChatStates map not initialized")
	}

	bridge.sessionStore.Close()
}

func TestGetChatState(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	config := Config{
		FeishuAppID:     "test",
		FeishuAppSecret: "test",
		WorkingDir:      tmpDir,
		SessionDBPath:   dbPath,
		SessionIdleMin:  60,
		SessionResetHr:  -1,
	}

	bridge, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create bridge: %v", err)
	}
	defer bridge.sessionStore.Close()

	// Get state for new chat
	state1 := bridge.getChatState("chat1")
	if state1 == nil {
		t.Error("getChatState returned nil")
	}

	// Same chat should return same state
	state2 := bridge.getChatState("chat1")
	if state1 != state2 {
		t.Error("Same chat should return same state")
	}

	// Different chat should return different state
	state3 := bridge.getChatState("chat2")
	if state1 == state3 {
		t.Error("Different chat should return different state")
	}
}

func TestFindChatByThread(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	config := Config{
		FeishuAppID:     "test",
		FeishuAppSecret: "test",
		WorkingDir:      tmpDir,
		SessionDBPath:   dbPath,
		SessionIdleMin:  60,
		SessionResetHr:  -1,
	}

	bridge, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create bridge: %v", err)
	}
	defer bridge.sessionStore.Close()

	// Set up a chat state with thread ID
	state := bridge.getChatState("chat123")
	state.ThreadID = "thread456"

	// Should find the chat
	chatID := bridge.findChatByThread("thread456")
	if chatID != "chat123" {
		t.Errorf("Expected chat123, got %q", chatID)
	}

	// Should not find non-existent thread
	chatID = bridge.findChatByThread("nonexistent")
	if chatID != "" {
		t.Errorf("Expected empty string, got %q", chatID)
	}
}

func TestHandleAgentDelta(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	config := Config{
		FeishuAppID:     "test",
		FeishuAppSecret: "test",
		WorkingDir:      tmpDir,
		SessionDBPath:   dbPath,
		SessionIdleMin:  60,
		SessionResetHr:  -1,
	}

	bridge, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create bridge: %v", err)
	}
	defer bridge.sessionStore.Close()

	// Set up a chat state with thread ID
	state := bridge.getChatState("chat123")
	state.ThreadID = "thread456"

	// Handle delta
	params := codex.AgentMessageDeltaParams{
		ThreadID: "thread456",
		TurnID:   "turn1",
		ItemID:   "item1",
		Delta:    "Hello ",
	}
	bridge.handleAgentDelta(params)

	// Check buffer
	if state.Buffer.String() != "Hello " {
		t.Errorf("Buffer mismatch: got %q", state.Buffer.String())
	}

	// Handle another delta
	params.Delta = "World"
	bridge.handleAgentDelta(params)

	if state.Buffer.String() != "Hello World" {
		t.Errorf("Buffer mismatch: got %q", state.Buffer.String())
	}
}

func TestHandleAgentDelta_NoChat(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	config := Config{
		FeishuAppID:     "test",
		FeishuAppSecret: "test",
		WorkingDir:      tmpDir,
		SessionDBPath:   dbPath,
		SessionIdleMin:  60,
		SessionResetHr:  -1,
	}

	bridge, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create bridge: %v", err)
	}
	defer bridge.sessionStore.Close()

	// Handle delta for non-existent thread (should not panic)
	params := codex.AgentMessageDeltaParams{
		ThreadID: "unknown",
		Delta:    "Hello",
	}
	bridge.handleAgentDelta(params)
}

func TestHandleEvent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	config := Config{
		FeishuAppID:     "test",
		FeishuAppSecret: "test",
		WorkingDir:      tmpDir,
		SessionDBPath:   dbPath,
		SessionIdleMin:  60,
		SessionResetHr:  -1,
		Debug:           true,
	}

	bridge, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create bridge: %v", err)
	}
	defer bridge.sessionStore.Close()

	// Set up chat state
	state := bridge.getChatState("chat1")
	state.ThreadID = "thread1"

	// Test item/agentMessage/delta
	deltaParams, _ := json.Marshal(codex.AgentMessageDeltaParams{
		ThreadID: "thread1",
		Delta:    "Test",
	})
	bridge.handleEvent(codex.Event{
		Method: codex.MethodAgentMessageDelta,
		Params: deltaParams,
	})

	if state.Buffer.String() != "Test" {
		t.Errorf("Delta not handled: got %q", state.Buffer.String())
	}

	// Test item/started (debug event)
	itemParams, _ := json.Marshal(codex.ItemStartedParams{
		ThreadID: "thread1",
		TurnID:   "turn1",
		Item:     &codex.ThreadItem{ID: "item1", Type: "agentMessage"},
	})
	bridge.handleEvent(codex.Event{
		Method: codex.MethodItemStarted,
		Params: itemParams,
	})

	// Test item/completed
	bridge.handleEvent(codex.Event{
		Method: codex.MethodItemCompleted,
		Params: itemParams,
	})

	// Test unknown event
	bridge.handleEvent(codex.Event{
		Method: "unknown/event",
		Params: nil,
	})
}

func TestHandleEvent_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	config := Config{
		FeishuAppID:     "test",
		FeishuAppSecret: "test",
		WorkingDir:      tmpDir,
		SessionDBPath:   dbPath,
		SessionIdleMin:  60,
		SessionResetHr:  -1,
	}

	bridge, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create bridge: %v", err)
	}
	defer bridge.sessionStore.Close()

	// Invalid JSON should not panic
	bridge.handleEvent(codex.Event{
		Method: codex.MethodAgentMessageDelta,
		Params: json.RawMessage(`invalid json`),
	})

	bridge.handleEvent(codex.Event{
		Method: codex.MethodTurnCompleted,
		Params: json.RawMessage(`invalid json`),
	})

	bridge.handleEvent(codex.Event{
		Method: codex.MethodItemStarted,
		Params: json.RawMessage(`invalid json`),
	})

	bridge.handleEvent(codex.Event{
		Method: codex.MethodItemCompleted,
		Params: json.RawMessage(`invalid json`),
	})
}

func TestChatStateMutex(t *testing.T) {
	state := &ChatState{}

	// Test concurrent access
	done := make(chan bool, 2)

	go func() {
		for i := 0; i < 100; i++ {
			state.mu.Lock()
			state.Buffer.WriteString("a")
			state.mu.Unlock()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			state.mu.Lock()
			state.Buffer.WriteString("b")
			state.mu.Unlock()
		}
		done <- true
	}()

	<-done
	<-done

	// Buffer should have 200 characters total
	if len(state.Buffer.String()) != 200 {
		t.Errorf("Expected 200 chars, got %d", len(state.Buffer.String()))
	}
}

func TestHandleTurnCompleted_NoChat(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	config := Config{
		FeishuAppID:     "test",
		FeishuAppSecret: "test",
		WorkingDir:      tmpDir,
		SessionDBPath:   dbPath,
		SessionIdleMin:  60,
		SessionResetHr:  -1,
	}

	bridge, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create bridge: %v", err)
	}
	defer bridge.sessionStore.Close()

	// Handle turn completed for unknown thread (should not panic, just log)
	params := codex.TurnCompletedParams{
		ThreadID: "unknown-thread",
		TurnID:   "turn1",
		Status:   "completed",
	}

	bridge.handleTurnCompleted(params)
}

func TestMultipleChatStates(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	config := Config{
		FeishuAppID:     "test",
		FeishuAppSecret: "test",
		WorkingDir:      tmpDir,
		SessionDBPath:   dbPath,
		SessionIdleMin:  60,
		SessionResetHr:  -1,
	}

	bridge, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create bridge: %v", err)
	}
	defer bridge.sessionStore.Close()

	// Create multiple chat states
	for i := 0; i < 10; i++ {
		chatID := "chat" + string(rune('0'+i))
		state := bridge.getChatState(chatID)
		state.ThreadID = "thread" + string(rune('0'+i))
	}

	// Verify all states exist
	for i := 0; i < 10; i++ {
		chatID := "chat" + string(rune('0'+i))
		threadID := "thread" + string(rune('0'+i))

		foundChat := bridge.findChatByThread(threadID)
		if foundChat != chatID {
			t.Errorf("Expected %s, got %s", chatID, foundChat)
		}
	}
}

func TestDebugModeEvents(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	config := Config{
		FeishuAppID:     "test",
		FeishuAppSecret: "test",
		WorkingDir:      tmpDir,
		SessionDBPath:   dbPath,
		SessionIdleMin:  60,
		SessionResetHr:  -1,
		Debug:           false, // Debug off
	}

	bridge, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create bridge: %v", err)
	}
	defer bridge.sessionStore.Close()

	// These should not print anything (no assertion, just ensure no panic)
	bridge.handleEvent(codex.Event{Method: "unknown/event"})

	itemParams, _ := json.Marshal(codex.ItemStartedParams{
		ThreadID: "t1",
		TurnID:   "turn1",
		Item:     &codex.ThreadItem{ID: "item1", Type: "test"},
	})
	bridge.handleEvent(codex.Event{Method: codex.MethodItemStarted, Params: itemParams})
	bridge.handleEvent(codex.Event{Method: codex.MethodItemCompleted, Params: itemParams})
}
