package codex

import "encoding/json"

// ============ JSON-RPC Base Types ============
// Note: Codex ACP doesn't include "jsonrpc":"2.0" header

// Request is a JSON-RPC request from client to server
type Request struct {
	ID     int64       `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params,omitempty"`
}

// Response is a JSON-RPC response from server to client
type Response struct {
	ID     int64           `json:"id,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *RPCError       `json:"error,omitempty"`
}

// Notification is a JSON-RPC notification (no response expected)
type Notification struct {
	ID     int64           `json:"id,omitempty"` // Server requests have ID
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// RPCError represents a JSON-RPC error
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// ============ Initialize ============

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type InitializeParams struct {
	ClientInfo ClientInfo `json:"clientInfo"`
}

type InitializeResult struct {
	UserAgent string `json:"userAgent"`
}

// ============ Thread Types ============

type Thread struct {
	ID            string `json:"id"`
	Preview       string `json:"preview"`
	ModelProvider string `json:"modelProvider,omitempty"`
	CreatedAt     int64  `json:"createdAt"`
	UpdatedAt     int64  `json:"updatedAt,omitempty"`
	Archived      bool   `json:"archived,omitempty"`
	Turns         []Turn `json:"turns,omitempty"`
	Cwd           string `json:"cwd,omitempty"`
	CliVersion    string `json:"cliVersion,omitempty"`
	Path          string `json:"path,omitempty"`
}

type Turn struct {
	ID     string       `json:"id"`
	Status string       `json:"status"` // completed|interrupted|failed|inProgress
	Items  []ThreadItem `json:"items"`
	Error  *TurnError   `json:"error,omitempty"`
}

type TurnError struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
}

// ============ Thread Items ============

type ThreadItem struct {
	Type string `json:"type"`
	ID   string `json:"id"`

	// agentMessage
	Text string `json:"text,omitempty"`

	// reasoning
	Content string `json:"content,omitempty"`
	Summary string `json:"summary,omitempty"`

	// commandExecution
	Command string          `json:"command,omitempty"`
	Status  ExecutionStatus `json:"status,omitempty"`
	Output  string          `json:"output,omitempty"`

	// fileChange
	Changes []FileChange `json:"changes,omitempty"`

	// mcpToolCall
	Server string `json:"server,omitempty"`
	Tool   string `json:"tool,omitempty"`

	// webSearch
	Query string `json:"query,omitempty"`

	// imageView
	Path string `json:"path,omitempty"`
}

type ExecutionStatus string

const (
	StatusPending   ExecutionStatus = "pending"
	StatusRunning   ExecutionStatus = "running"
	StatusCompleted ExecutionStatus = "completed"
	StatusFailed    ExecutionStatus = "failed"
)

type FileChange struct {
	Path string `json:"path"`
	Diff string `json:"diff,omitempty"`
}

// ============ Request Params ============

type ThreadStartParams struct {
	Name               string `json:"name,omitempty"`
	Model              string `json:"model,omitempty"`
	ModelProviderID    string `json:"model_provider_id,omitempty"`
	Cwd                string `json:"cwd,omitempty"`
	ApprovalPolicy     string `json:"approval_policy,omitempty"`
	SandboxPolicy      string `json:"sandbox_policy,omitempty"`
	ReasoningEffort    string `json:"reasoning_effort,omitempty"`
	Personality        string `json:"personality,omitempty"`
	SandboxPermissions string `json:"sandbox_permissions,omitempty"`
}

type ThreadResumeParams struct {
	ThreadID string `json:"threadId"`
}

// UserInput represents input content for a turn
type UserInput struct {
	Type string `json:"type"`
	// For type "text"
	Text string `json:"text,omitempty"`
	// For type "image"
	URL string `json:"url,omitempty"`
	// For type "localImage"
	Path string `json:"path,omitempty"`
}

type TurnStartParams struct {
	ThreadID string      `json:"threadId"`
	Input    []UserInput `json:"input"`
}

type TurnInterruptParams struct {
	ThreadID string `json:"threadId"`
}

// ============ Response Results ============

type ThreadStartResult struct {
	Thread Thread `json:"thread"`
}

type ThreadResumeResult struct {
	Thread Thread `json:"thread"`
}

type TurnStartResult struct {
	TurnID string `json:"turnId"`
}

// ============ Event Types (Server Notifications) ============

type ThreadStartedParams struct {
	ThreadID string  `json:"threadId"`
	Thread   *Thread `json:"thread,omitempty"`
}

type ThreadNameUpdatedParams struct {
	ThreadID string `json:"threadId"`
	Name     string `json:"name"`
}

type TurnStartedParams struct {
	ThreadID string `json:"threadId"`
	Turn     *Turn  `json:"turn,omitempty"`
}

type TurnCompletedParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	Status   string `json:"status"` // completed|interrupted|failed
}

type ItemStartedParams struct {
	ThreadID string      `json:"threadId"`
	TurnID   string      `json:"turnId"`
	Item     *ThreadItem `json:"item,omitempty"`
}

type ItemCompletedParams struct {
	ThreadID string      `json:"threadId"`
	TurnID   string      `json:"turnId"`
	Item     *ThreadItem `json:"item,omitempty"`
}

// Delta events
type AgentMessageDeltaParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Delta    string `json:"delta"`
}

type ReasoningTextDeltaParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Delta    string `json:"delta"`
}

type CommandExecutionOutputDeltaParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Delta    string `json:"delta"`
}

// ============ Approval Requests (Server → Client) ============

type CommandExecutionApprovalParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Command  string `json:"command"`
	Cwd      string `json:"cwd"`
}

type FileChangeApprovalParams struct {
	ThreadID string       `json:"threadId"`
	TurnID   string       `json:"turnId"`
	ItemID   string       `json:"itemId"`
	Changes  []FileChange `json:"changes"`
}

type ApprovalResponse struct {
	Decision       string            `json:"decision"` // accept|decline
	AcceptSettings map[string]string `json:"acceptSettings,omitempty"`
}

// ============ Token Usage ============

type TokenUsageUpdatedParams struct {
	ThreadID    string `json:"threadId"`
	InputTokens int64  `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
}

// ============ Event Methods ============

const (
	// Thread events
	MethodThreadStarted     = "thread/started"
	MethodThreadNameUpdated = "thread/started/updated"
	MethodTokenUsageUpdated = "thread/tokenUsage/updated"

	// Turn events
	MethodTurnStarted   = "turn/started"
	MethodTurnCompleted = "turn/completed"

	// Item events
	MethodItemStarted   = "item/started"
	MethodItemCompleted = "item/completed"

	// Delta events
	MethodAgentMessageDelta           = "item/agentMessage/delta"
	MethodReasoningTextDelta          = "item/reasoning/textDelta"
	MethodCommandExecutionOutputDelta = "item/commandExecution/outputDelta"

	// Approval requests
	MethodCommandExecutionRequestApproval = "item/commandExecution/requestApproval"
	MethodFileChangeRequestApproval       = "item/fileChange/requestApproval"
)
