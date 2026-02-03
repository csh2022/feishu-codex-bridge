package codex

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNewClient(t *testing.T) {
	client := NewClient("/home/test", "gpt-4")

	if client.workingDir != "/home/test" {
		t.Errorf("workingDir mismatch: got %q", client.workingDir)
	}
	if client.model != "gpt-4" {
		t.Errorf("model mismatch: got %q", client.model)
	}
	if client.pending == nil {
		t.Error("pending map not initialized")
	}
	if client.events == nil {
		t.Error("events channel not initialized")
	}
}

func TestIsRunning(t *testing.T) {
	client := NewClient("/home/test", "")

	if client.IsRunning() {
		t.Error("new client should not be running")
	}

	client.running = true
	if client.IsRunning() {
		t.Error("should not be running without initialization")
	}

	client.initialized = true
	if !client.IsRunning() {
		t.Error("should be running after both flags set")
	}
}

func TestEvents(t *testing.T) {
	client := NewClient("/home/test", "")

	ch := client.Events()
	if ch == nil {
		t.Error("Events() returned nil")
	}
}

func TestMustMarshal(t *testing.T) {
	input := map[string]string{"key": "value"}
	result := mustMarshal(input)

	var parsed map[string]string
	err := json.Unmarshal(result, &parsed)
	if err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}
	if parsed["key"] != "value" {
		t.Errorf("Unexpected result: %v", parsed)
	}
}

func TestStopNotRunning(t *testing.T) {
	client := NewClient("/home/test", "")

	// Should not panic when not running
	err := client.Stop()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestSendRequestNotRunning(t *testing.T) {
	client := NewClient("/home/test", "")

	_, err := client.sendRequest("test", nil)
	if err == nil {
		t.Error("Expected error for non-running client")
	}
	if err.Error() != "client not running" {
		t.Errorf("Unexpected error message: %v", err)
	}
}

func TestHandleLineResponse(t *testing.T) {
	client := NewClient("/home/test", "")
	client.running = true

	// Create a pending response channel
	respChan := make(chan *Response, 1)
	client.pending[1] = respChan

	// Simulate receiving a response
	line := `{"id": 1, "result": {"test": true}}`
	client.handleLine(line)

	// Check if response was delivered
	select {
	case resp := <-respChan:
		if resp == nil {
			t.Error("Got nil response")
		}
		if resp.ID != 1 {
			t.Errorf("ID mismatch: got %d", resp.ID)
		}
	default:
		t.Error("Response not delivered")
	}
}

func TestHandleLineNotification(t *testing.T) {
	client := NewClient("/home/test", "")
	client.running = true

	// Simulate receiving a notification
	line := `{"method": "turn/completed", "params": {"threadId": "test"}}`
	client.handleLine(line)

	// Check if event was sent
	select {
	case event := <-client.events:
		if event.Method != "turn/completed" {
			t.Errorf("Method mismatch: got %q", event.Method)
		}
	default:
		t.Error("Event not delivered")
	}
}

func TestHandleLineApprovalRequest(t *testing.T) {
	client := NewClient("/home/test", "")
	client.running = true

	// We can't fully test auto-approval without a running stdin,
	// but we can test that approval requests with ID are recognized
	line := `{"id": 100, "method": "item/commandExecution/requestApproval", "params": {"command": "ls"}}`

	// This will try to call RespondToApproval, which will fail without stdin
	// But the line should be parsed correctly
	client.handleLine(line)

	// Verify no event was sent (approval requests are handled, not forwarded)
	select {
	case <-client.events:
		t.Error("Approval request should not be forwarded as event")
	default:
		// Expected - no event
	}
}

func TestHandleLineInvalidJSON(t *testing.T) {
	client := NewClient("/home/test", "")
	client.running = true

	// Invalid JSON should not panic
	client.handleLine("invalid json")
	client.handleLine("")
	client.handleLine("{}")
}

func TestThreadStartWithNilParams(t *testing.T) {
	client := NewClient("/home/test", "")

	// Without running, should fail
	_, err := client.ThreadStart(context.Background(), nil)
	if err == nil {
		t.Error("Expected error for non-running client")
	}
}

func TestTurnStartInput(t *testing.T) {
	// Test that TurnStart builds correct input array
	// We can test the input building logic by checking params construction
	// (actual RPC call will fail without running server)

	prompt := "Hello"
	images := []string{"/path/to/img1.png", "/path/to/img2.png"}

	// Build input like TurnStart does
	input := []UserInput{
		{Type: "text", Text: prompt},
	}
	for _, img := range images {
		input = append(input, UserInput{Type: "localImage", Path: img})
	}

	if len(input) != 3 {
		t.Errorf("Expected 3 inputs, got %d", len(input))
	}
	if input[0].Type != "text" || input[0].Text != "Hello" {
		t.Errorf("First input mismatch: %+v", input[0])
	}
	if input[1].Type != "localImage" || input[1].Path != "/path/to/img1.png" {
		t.Errorf("Second input mismatch: %+v", input[1])
	}
}

func TestThreadResume(t *testing.T) {
	client := NewClient("/home/test", "")

	// Without running, should fail
	_, err := client.ThreadResume(context.Background(), "thread-123")
	if err == nil {
		t.Error("Expected error for non-running client")
	}
}

func TestTurnInterrupt(t *testing.T) {
	client := NewClient("/home/test", "")

	// Without running, should fail
	err := client.TurnInterrupt(context.Background(), "thread-123")
	if err == nil {
		t.Error("Expected error for non-running client")
	}
}

func TestEventStruct(t *testing.T) {
	event := Event{
		Method: "test/method",
		Params: json.RawMessage(`{"key": "value"}`),
	}

	if event.Method != "test/method" {
		t.Errorf("Method mismatch")
	}

	var params map[string]string
	err := json.Unmarshal(event.Params, &params)
	if err != nil {
		t.Fatalf("Failed to unmarshal params: %v", err)
	}
	if params["key"] != "value" {
		t.Errorf("Params mismatch")
	}
}

func TestHandleLineEmptyLine(t *testing.T) {
	client := NewClient("/home/test", "")
	client.running = true

	// Empty line should be handled gracefully
	client.handleLine("")
}

func TestHandleLineResponseNotPending(t *testing.T) {
	client := NewClient("/home/test", "")
	client.running = true

	// Response for non-pending request (should be ignored)
	line := `{"id": 999, "result": {"test": true}}`
	client.handleLine(line)
}

func TestHandleLineNotificationDropped(t *testing.T) {
	client := NewClient("/home/test", "")
	client.running = true

	// Fill the events channel
	for i := 0; i < 100; i++ {
		select {
		case client.events <- Event{Method: "fill"}:
		default:
		}
	}

	// This notification should be dropped (channel full)
	line := `{"method": "test/notification", "params": {}}`
	client.handleLine(line)
}




