package codex

import (
	"encoding/json"
	"testing"
)

func TestUserInputSerialization(t *testing.T) {
	tests := []struct {
		name     string
		input    UserInput
		expected string
	}{
		{
			name:     "text input",
			input:    UserInput{Type: "text", Text: "Hello world"},
			expected: `{"type":"text","text":"Hello world"}`,
		},
		{
			name:     "local image input",
			input:    UserInput{Type: "localImage", Path: "/path/to/image.png"},
			expected: `{"type":"localImage","path":"/path/to/image.png"}`,
		},
		{
			name:     "image URL input",
			input:    UserInput{Type: "image", URL: "https://example.com/image.png"},
			expected: `{"type":"image","url":"https://example.com/image.png"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.input)
			if err != nil {
				t.Fatalf("Failed to marshal: %v", err)
			}

			// Unmarshal expected to compare
			var expected, actual map[string]interface{}
			json.Unmarshal([]byte(tt.expected), &expected)
			json.Unmarshal(data, &actual)

			// Check type field
			if actual["type"] != expected["type"] {
				t.Errorf("Type mismatch: got %v, want %v", actual["type"], expected["type"])
			}
		})
	}
}

func TestTurnStartParamsSerialization(t *testing.T) {
	params := TurnStartParams{
		ThreadID: "test-thread-123",
		Input: []UserInput{
			{Type: "text", Text: "Hello"},
		},
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var result map[string]interface{}
	json.Unmarshal(data, &result)

	if result["threadId"] != "test-thread-123" {
		t.Errorf("threadId mismatch: got %v", result["threadId"])
	}

	input, ok := result["input"].([]interface{})
	if !ok || len(input) != 1 {
		t.Errorf("input mismatch: got %v", result["input"])
	}
}

func TestThreadStartResultDeserialization(t *testing.T) {
	// Real response format from Codex app-server
	jsonData := `{
		"thread": {
			"id": "019c2468-4889-7710-9218-9afd7ecf3100",
			"preview": "",
			"modelProvider": "openai",
			"createdAt": 1770137340,
			"updatedAt": 1770137340,
			"path": "/home/rick/.codex/sessions/2026/02/04/rollout.jsonl",
			"cwd": "/media/rick/MAIN",
			"cliVersion": "0.94.0",
			"source": "vscode",
			"gitInfo": null,
			"turns": []
		},
		"model": "gpt-5.2-codex",
		"modelProvider": "openai",
		"cwd": "/media/rick/MAIN",
		"approvalPolicy": "on-request",
		"sandbox": {"type": "workspaceWrite"},
		"reasoningEffort": null
	}`

	var result ThreadStartResult
	err := json.Unmarshal([]byte(jsonData), &result)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if result.Thread.ID != "019c2468-4889-7710-9218-9afd7ecf3100" {
		t.Errorf("Thread ID mismatch: got %q, want 019c2468-4889-7710-9218-9afd7ecf3100", result.Thread.ID)
	}
	if result.Thread.ModelProvider != "openai" {
		t.Errorf("ModelProvider mismatch: got %q", result.Thread.ModelProvider)
	}
	if result.Thread.Cwd != "/media/rick/MAIN" {
		t.Errorf("Cwd mismatch: got %q", result.Thread.Cwd)
	}
}

func TestRequestSerialization(t *testing.T) {
	req := Request{
		ID:     1,
		Method: "thread/start",
		Params: ThreadStartParams{},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var result map[string]interface{}
	json.Unmarshal(data, &result)

	if result["id"].(float64) != 1 {
		t.Errorf("id mismatch: got %v", result["id"])
	}
	if result["method"] != "thread/start" {
		t.Errorf("method mismatch: got %v", result["method"])
	}
}

func TestResponseDeserialization(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		hasErr  bool
		errCode int
	}{
		{
			name:   "success response",
			json:   `{"id": 1, "result": {"thread": {"id": "test"}}}`,
			hasErr: false,
		},
		{
			name:    "error response",
			json:    `{"id": 1, "error": {"code": -32600, "message": "Invalid request"}}`,
			hasErr:  true,
			errCode: -32600,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp Response
			err := json.Unmarshal([]byte(tt.json), &resp)
			if err != nil {
				t.Fatalf("Failed to unmarshal: %v", err)
			}

			if tt.hasErr {
				if resp.Error == nil {
					t.Error("Expected error but got nil")
				} else if resp.Error.Code != tt.errCode {
					t.Errorf("Error code mismatch: got %d, want %d", resp.Error.Code, tt.errCode)
				}
			} else {
				if resp.Error != nil {
					t.Errorf("Unexpected error: %v", resp.Error)
				}
			}
		})
	}
}

func TestNotificationDeserialization(t *testing.T) {
	tests := []struct {
		name   string
		json   string
		method string
		hasID  bool
	}{
		{
			name:   "regular notification",
			json:   `{"method": "turn/completed", "params": {"threadId": "test"}}`,
			method: "turn/completed",
			hasID:  false,
		},
		{
			name:   "approval request (has ID)",
			json:   `{"id": 100, "method": "item/commandExecution/requestApproval", "params": {"command": "ls"}}`,
			method: "item/commandExecution/requestApproval",
			hasID:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var notif Notification
			err := json.Unmarshal([]byte(tt.json), &notif)
			if err != nil {
				t.Fatalf("Failed to unmarshal: %v", err)
			}

			if notif.Method != tt.method {
				t.Errorf("Method mismatch: got %v, want %v", notif.Method, tt.method)
			}

			if tt.hasID && notif.ID == 0 {
				t.Error("Expected ID but got 0")
			}
			if !tt.hasID && notif.ID != 0 {
				t.Errorf("Unexpected ID: %d", notif.ID)
			}
		})
	}
}

func TestAgentMessageDeltaParams(t *testing.T) {
	jsonStr := `{
		"threadId": "thread-1",
		"turnId": "turn-1",
		"itemId": "item-1",
		"delta": "Hello "
	}`

	var params AgentMessageDeltaParams
	err := json.Unmarshal([]byte(jsonStr), &params)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if params.ThreadID != "thread-1" {
		t.Errorf("ThreadID mismatch: got %v", params.ThreadID)
	}
	if params.Delta != "Hello " {
		t.Errorf("Delta mismatch: got %v", params.Delta)
	}
}

func TestTurnCompletedParams(t *testing.T) {
	jsonStr := `{
		"threadId": "thread-1",
		"turnId": "turn-1",
		"status": "completed"
	}`

	var params TurnCompletedParams
	err := json.Unmarshal([]byte(jsonStr), &params)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if params.Status != "completed" {
		t.Errorf("Status mismatch: got %v", params.Status)
	}
}

func TestExecutionStatus(t *testing.T) {
	if StatusPending != "pending" {
		t.Error("StatusPending mismatch")
	}
	if StatusRunning != "running" {
		t.Error("StatusRunning mismatch")
	}
	if StatusCompleted != "completed" {
		t.Error("StatusCompleted mismatch")
	}
	if StatusFailed != "failed" {
		t.Error("StatusFailed mismatch")
	}
}

func TestEventMethods(t *testing.T) {
	methods := []string{
		MethodThreadStarted,
		MethodThreadNameUpdated,
		MethodTokenUsageUpdated,
		MethodTurnStarted,
		MethodTurnCompleted,
		MethodItemStarted,
		MethodItemCompleted,
		MethodAgentMessageDelta,
		MethodReasoningTextDelta,
		MethodCommandExecutionOutputDelta,
		MethodCommandExecutionRequestApproval,
		MethodFileChangeRequestApproval,
	}

	for _, method := range methods {
		if method == "" {
			t.Error("Empty method constant found")
		}
	}
}

func TestThreadItem(t *testing.T) {
	item := ThreadItem{
		Type:    "agentMessage",
		ID:      "item-1",
		Text:    "Hello",
		Command: "ls -la",
		Status:  StatusCompleted,
		Output:  "file1.txt\nfile2.txt",
	}

	if item.Type != "agentMessage" {
		t.Error("Type mismatch")
	}
	if item.Text != "Hello" {
		t.Error("Text mismatch")
	}
	if item.Command != "ls -la" {
		t.Error("Command mismatch")
	}
}

func TestFileChange(t *testing.T) {
	change := FileChange{
		Path: "/path/to/file.go",
		Diff: "+line added\n-line removed",
	}

	if change.Path != "/path/to/file.go" {
		t.Error("Path mismatch")
	}
	if change.Diff == "" {
		t.Error("Diff should not be empty")
	}
}

func TestTurn(t *testing.T) {
	turn := Turn{
		ID:     "turn-1",
		Status: "completed",
		Items: []ThreadItem{
			{Type: "agentMessage", ID: "item-1"},
		},
		Error: &TurnError{Type: "error", Message: "something went wrong"},
	}

	if turn.ID != "turn-1" {
		t.Error("ID mismatch")
	}
	if len(turn.Items) != 1 {
		t.Error("Items length mismatch")
	}
	if turn.Error == nil || turn.Error.Message != "something went wrong" {
		t.Error("Error mismatch")
	}
}

func TestThread(t *testing.T) {
	thread := Thread{
		ID:            "thread-123",
		Preview:       "Test preview",
		ModelProvider: "openai",
		CreatedAt:     1000000,
		UpdatedAt:     1000001,
		Archived:      false,
		Cwd:           "/home/user",
		CliVersion:    "1.0.0",
		Path:          "/path/to/session.jsonl",
	}

	if thread.ID != "thread-123" {
		t.Error("ID mismatch")
	}
	if thread.ModelProvider != "openai" {
		t.Error("ModelProvider mismatch")
	}
}

func TestInitializeParams(t *testing.T) {
	params := InitializeParams{
		ClientInfo: ClientInfo{
			Name:    "test-client",
			Version: "1.0.0",
		},
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var result map[string]interface{}
	json.Unmarshal(data, &result)

	clientInfo := result["clientInfo"].(map[string]interface{})
	if clientInfo["name"] != "test-client" {
		t.Error("ClientInfo name mismatch")
	}
}

func TestThreadStartParams(t *testing.T) {
	params := ThreadStartParams{
		Name:            "Test Thread",
		Model:           "gpt-4",
		Cwd:             "/home/user",
		ApprovalPolicy:  "auto",
		ReasoningEffort: "high",
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var result map[string]interface{}
	json.Unmarshal(data, &result)

	if result["name"] != "Test Thread" {
		t.Error("Name mismatch")
	}
}

func TestApprovalResponse(t *testing.T) {
	resp := ApprovalResponse{
		Decision: "accept",
		AcceptSettings: map[string]string{
			"timeout": "300",
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var result map[string]interface{}
	json.Unmarshal(data, &result)

	if result["decision"] != "accept" {
		t.Error("Decision mismatch")
	}
}

func TestCommandExecutionApprovalParams(t *testing.T) {
	jsonStr := `{
		"threadId": "thread-1",
		"turnId": "turn-1",
		"itemId": "item-1",
		"command": "rm -rf /tmp/test",
		"cwd": "/home/user"
	}`

	var params CommandExecutionApprovalParams
	err := json.Unmarshal([]byte(jsonStr), &params)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if params.Command != "rm -rf /tmp/test" {
		t.Error("Command mismatch")
	}
	if params.Cwd != "/home/user" {
		t.Error("Cwd mismatch")
	}
}

func TestFileChangeApprovalParams(t *testing.T) {
	jsonStr := `{
		"threadId": "thread-1",
		"turnId": "turn-1",
		"itemId": "item-1",
		"changes": [
			{"path": "/file1.go", "diff": "+line1"},
			{"path": "/file2.go", "diff": "-line2"}
		]
	}`

	var params FileChangeApprovalParams
	err := json.Unmarshal([]byte(jsonStr), &params)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if len(params.Changes) != 2 {
		t.Errorf("Expected 2 changes, got %d", len(params.Changes))
	}
}

func TestTokenUsageUpdatedParams(t *testing.T) {
	jsonStr := `{
		"threadId": "thread-1",
		"inputTokens": 1000,
		"outputTokens": 500
	}`

	var params TokenUsageUpdatedParams
	err := json.Unmarshal([]byte(jsonStr), &params)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if params.InputTokens != 1000 {
		t.Error("InputTokens mismatch")
	}
	if params.OutputTokens != 500 {
		t.Error("OutputTokens mismatch")
	}
}

func TestItemStartedParams(t *testing.T) {
	jsonStr := `{
		"threadId": "thread-1",
		"turnId": "turn-1",
		"item": {
			"type": "commandExecution",
			"id": "item-1",
			"command": "ls"
		}
	}`

	var params ItemStartedParams
	err := json.Unmarshal([]byte(jsonStr), &params)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if params.Item == nil {
		t.Fatal("Item should not be nil")
	}
	if params.Item.Type != "commandExecution" {
		t.Error("Item type mismatch")
	}
}

func TestReasoningTextDeltaParams(t *testing.T) {
	jsonStr := `{
		"threadId": "thread-1",
		"turnId": "turn-1",
		"itemId": "item-1",
		"delta": "thinking..."
	}`

	var params ReasoningTextDeltaParams
	err := json.Unmarshal([]byte(jsonStr), &params)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if params.Delta != "thinking..." {
		t.Error("Delta mismatch")
	}
}

func TestCommandExecutionOutputDeltaParams(t *testing.T) {
	jsonStr := `{
		"threadId": "thread-1",
		"turnId": "turn-1",
		"itemId": "item-1",
		"delta": "output line\n"
	}`

	var params CommandExecutionOutputDeltaParams
	err := json.Unmarshal([]byte(jsonStr), &params)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if params.Delta != "output line\n" {
		t.Error("Delta mismatch")
	}
}

func TestThreadStartedParams(t *testing.T) {
	jsonStr := `{
		"threadId": "thread-1",
		"thread": {
			"id": "thread-1",
			"preview": "test"
		}
	}`

	var params ThreadStartedParams
	err := json.Unmarshal([]byte(jsonStr), &params)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if params.ThreadID != "thread-1" {
		t.Error("ThreadID mismatch")
	}
}

func TestThreadNameUpdatedParams(t *testing.T) {
	jsonStr := `{
		"threadId": "thread-1",
		"name": "New Name"
	}`

	var params ThreadNameUpdatedParams
	err := json.Unmarshal([]byte(jsonStr), &params)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if params.Name != "New Name" {
		t.Error("Name mismatch")
	}
}

func TestTurnStartedParams(t *testing.T) {
	jsonStr := `{
		"threadId": "thread-1",
		"turn": {
			"id": "turn-1",
			"status": "inProgress"
		}
	}`

	var params TurnStartedParams
	err := json.Unmarshal([]byte(jsonStr), &params)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if params.Turn == nil {
		t.Fatal("Turn should not be nil")
	}
	if params.Turn.Status != "inProgress" {
		t.Error("Turn status mismatch")
	}
}

func TestRPCError(t *testing.T) {
	rpcErr := RPCError{
		Code:    -32600,
		Message: "Invalid request",
		Data:    map[string]string{"detail": "missing field"},
	}

	if rpcErr.Code != -32600 {
		t.Error("Code mismatch")
	}
	if rpcErr.Message != "Invalid request" {
		t.Error("Message mismatch")
	}
}
