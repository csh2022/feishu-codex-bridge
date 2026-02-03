package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// Event represents a notification from the Codex server
type Event struct {
	Method string
	Params json.RawMessage
}

// Client is the ACP client for communicating with Codex app-server
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	stderr io.ReadCloser

	requestID int64
	pending   map[int64]chan *Response
	pendingMu sync.Mutex

	events      chan Event
	initialized bool
	running     bool

	workingDir string
	model      string

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewClient creates a new ACP client
func NewClient(workingDir, model string) *Client {
	return &Client{
		workingDir: workingDir,
		model:      model,
		pending:    make(map[int64]chan *Response),
		events:     make(chan Event, 100),
	}
}

// Start spawns the Codex app-server process and initializes the connection
func (c *Client) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Build command arguments
	args := []string{"app-server"}
	if c.model != "" {
		args = append(args, "-c", fmt.Sprintf("model=\"%s\"", c.model))
	}
	// Enable full-auto mode for sandbox permissions
	args = append(args, "-c", `sandbox_permissions=["disk-full-read-access","disk-full-write-access","network-full-access"]`)

	fmt.Printf("[Codex] Starting: codex %v\n", args)

	c.cmd = exec.CommandContext(c.ctx, "codex", args...)
	c.cmd.Dir = c.workingDir

	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	c.stdout = bufio.NewScanner(stdout)
	c.stdout.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer for large responses

	c.stderr, err = c.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start codex: %w", err)
	}

	c.running = true

	// Start read loops
	c.wg.Add(2)
	go c.readLoop()
	go c.readStderr()

	// Initialize handshake
	if err := c.initialize(); err != nil {
		c.Stop()
		return fmt.Errorf("failed to initialize: %w", err)
	}

	fmt.Println("[Codex] Initialized successfully")
	return nil
}

// Stop gracefully shuts down the client
func (c *Client) Stop() error {
	if !c.running {
		return nil
	}

	c.running = false
	c.cancel()

	// Close stdin to signal EOF
	if c.stdin != nil {
		c.stdin.Close()
	}

	// Wait for process with timeout
	done := make(chan error, 1)
	go func() {
		done <- c.cmd.Wait()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		c.cmd.Process.Kill()
	}

	close(c.events)
	c.wg.Wait()

	fmt.Println("[Codex] Stopped")
	return nil
}

// Events returns the channel for receiving server notifications
func (c *Client) Events() <-chan Event {
	return c.events
}

// IsRunning returns true if the client is running
func (c *Client) IsRunning() bool {
	return c.running && c.initialized
}

// ============ High-level API ============

// ThreadStart creates a new thread
func (c *Client) ThreadStart(ctx context.Context, params *ThreadStartParams) (string, error) {
	if params == nil {
		params = &ThreadStartParams{}
	}

	resp, err := c.sendRequest("thread/start", params)
	if err != nil {
		return "", err
	}

	var result ThreadStartResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("failed to parse thread/start result: %w", err)
	}

	if result.Thread.ID == "" {
		return "", fmt.Errorf("thread/start returned empty thread ID")
	}

	return result.Thread.ID, nil
}

// ThreadResume resumes an existing thread
func (c *Client) ThreadResume(ctx context.Context, threadID string) (*Thread, error) {
	params := ThreadResumeParams{ThreadID: threadID}

	resp, err := c.sendRequest("thread/resume", params)
	if err != nil {
		return nil, err
	}

	var result ThreadResumeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse thread/resume result: %w", err)
	}

	return &result.Thread, nil
}

// TurnStart starts a new turn with a user prompt
func (c *Client) TurnStart(ctx context.Context, threadID, prompt string, images []string) (string, error) {
	// Build input array
	input := []UserInput{
		{Type: "text", Text: prompt},
	}
	// Add images if provided
	for _, img := range images {
		input = append(input, UserInput{Type: "localImage", Path: img})
	}

	params := TurnStartParams{
		ThreadID: threadID,
		Input:    input,
	}

	resp, err := c.sendRequest("turn/start", params)
	if err != nil {
		return "", err
	}

	var result TurnStartResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("failed to parse turn/start result: %w", err)
	}

	return result.TurnID, nil
}

// TurnInterrupt interrupts the current turn
func (c *Client) TurnInterrupt(ctx context.Context, threadID string) error {
	params := TurnInterruptParams{ThreadID: threadID}
	_, err := c.sendRequest("turn/interrupt", params)
	return err
}

// RespondToApproval responds to an approval request from the server
func (c *Client) RespondToApproval(requestID int64, decision string) error {
	response := Response{
		ID: requestID,
		Result: mustMarshal(ApprovalResponse{
			Decision: decision,
		}),
	}

	return c.sendRaw(response)
}

// ============ Internal Methods ============

func (c *Client) initialize() error {
	params := InitializeParams{
		ClientInfo: ClientInfo{
			Name:    "feishu-codex-bridge",
			Version: "1.0.0",
		},
	}

	resp, err := c.sendRequest("initialize", params)
	if err != nil {
		return err
	}

	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return fmt.Errorf("failed to parse initialize result: %w", err)
	}

	fmt.Printf("[Codex] Server: %s\n", result.UserAgent)

	// Send initialized notification
	c.sendNotification("initialized", nil)

	c.initialized = true
	return nil
}

func (c *Client) sendRequest(method string, params interface{}) (*Response, error) {
	if !c.running {
		return nil, fmt.Errorf("client not running")
	}

	id := atomic.AddInt64(&c.requestID, 1)
	req := Request{
		ID:     id,
		Method: method,
		Params: params,
	}

	// Create response channel
	respChan := make(chan *Response, 1)
	c.pendingMu.Lock()
	c.pending[id] = respChan
	c.pendingMu.Unlock()

	// Send request
	if err := c.sendRaw(req); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, err
	}

	// Wait for response with timeout
	select {
	case resp := <-respChan:
		if resp.Error != nil {
			return nil, fmt.Errorf("RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp, nil
	case <-time.After(5 * time.Minute):
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, fmt.Errorf("request %s timed out", method)
	case <-c.ctx.Done():
		return nil, c.ctx.Err()
	}
}

func (c *Client) sendNotification(method string, params interface{}) error {
	notif := struct {
		Method string      `json:"method"`
		Params interface{} `json:"params,omitempty"`
	}{
		Method: method,
		Params: params,
	}
	return c.sendRaw(notif)
}

func (c *Client) sendRaw(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal: %w", err)
	}

	line := append(data, '\n')
	_, err = c.stdin.Write(line)
	return err
}

func (c *Client) readLoop() {
	defer c.wg.Done()

	for c.stdout.Scan() {
		line := c.stdout.Text()
		if line == "" {
			continue
		}

		c.handleLine(line)
	}

	if err := c.stdout.Err(); err != nil && c.running {
		fmt.Printf("[Codex] Read error: %v\n", err)
	}
}

func (c *Client) handleLine(line string) {
	// Try to parse as Response (has "id" and "result" or "error")
	var resp Response
	if err := json.Unmarshal([]byte(line), &resp); err == nil && resp.ID != 0 {
		c.pendingMu.Lock()
		if ch, ok := c.pending[resp.ID]; ok {
			ch <- &resp
			delete(c.pending, resp.ID)
		}
		c.pendingMu.Unlock()
		return
	}

	// Otherwise it's a Notification (may or may not have "id" for approval requests)
	var notif Notification
	if err := json.Unmarshal([]byte(line), &notif); err == nil && notif.Method != "" {
		// Check if it's an approval request (has ID)
		if notif.ID != 0 {
			// Auto-approve all requests
			c.RespondToApproval(notif.ID, "accept")
			return
		}

		// Regular notification - send to events channel
		select {
		case c.events <- Event{Method: notif.Method, Params: notif.Params}:
		default:
			fmt.Printf("[Codex] Event channel full, dropping: %s\n", notif.Method)
		}
	}
}

func (c *Client) readStderr() {
	defer c.wg.Done()

	scanner := bufio.NewScanner(c.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			fmt.Printf("[Codex stderr] %s\n", line)
		}
	}
}

func mustMarshal(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
