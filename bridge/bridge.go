package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/feishu-codex-bridge/codex"
	"github.com/anthropics/feishu-codex-bridge/feishu"
	"github.com/anthropics/feishu-codex-bridge/session"
)

type Config struct {
	FeishuAppID     string
	FeishuAppSecret string
	WorkingDir      string
	CodexModel      string
	SessionDBPath   string
	SessionIdleMin  int
	SessionResetHr  int
	Debug           bool
}

type Bridge struct {
	config       Config
	feishuClient *feishu.Client
	codexClient  *codex.Client
	sessionStore *session.Store

	// Per-chat state
	chatStates   map[string]*ChatState
	chatStatesMu sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type ChatState struct {
	ThreadID   string
	TurnID     string
	MsgID      string // Current message ID for reactions
	Processing bool
	Buffer     strings.Builder
	LastItem   string
	mu         sync.Mutex
}

func New(config Config) (*Bridge, error) {
	// Initialize session store
	sessionStore, err := session.NewStore(
		config.SessionDBPath,
		config.SessionIdleMin,
		config.SessionResetHr,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create session store: %w", err)
	}

	// Initialize Feishu client
	feishuClient := feishu.NewClient(config.FeishuAppID, config.FeishuAppSecret)

	// Initialize Codex client
	codexClient := codex.NewClient(config.WorkingDir, config.CodexModel)

	return &Bridge{
		config:       config,
		feishuClient: feishuClient,
		codexClient:  codexClient,
		sessionStore: sessionStore,
		chatStates:   make(map[string]*ChatState),
	}, nil
}

func (b *Bridge) Start() error {
	b.ctx, b.cancel = context.WithCancel(context.Background())

	fmt.Println("[Bridge] Starting Feishu-Codex bridge...")
	fmt.Printf("[Bridge] Working directory: %s\n", b.config.WorkingDir)
	fmt.Printf("[Bridge] Model: %s\n", b.config.CodexModel)
	fmt.Printf("[Bridge] Session DB: %s\n", b.config.SessionDBPath)

	// Start Codex app-server
	if err := b.codexClient.Start(b.ctx); err != nil {
		return fmt.Errorf("failed to start codex: %w", err)
	}

	// Start event processor
	b.wg.Add(1)
	go b.processEvents()

	// Set up Feishu message handler
	b.feishuClient.OnMessage(b.handleFeishuMessageV2)

	// Start session cleanup
	b.StartSessionCleanup(10 * time.Minute)

	// Start Feishu WebSocket (this blocks)
	fmt.Println("[Bridge] Starting Feishu connection...")
	return b.feishuClient.Start()
}

func (b *Bridge) Stop() {
	fmt.Println("[Bridge] Stopping...")

	b.cancel()
	b.feishuClient.Stop()
	b.codexClient.Stop()
	b.sessionStore.Close()

	b.wg.Wait()
	fmt.Println("[Bridge] Stopped")
}

func (b *Bridge) handleFeishuMessageV2(msg *feishu.Message) {
	fmt.Printf("[Bridge] Received %s from %s: %s\n", msg.MsgType, msg.ChatID, truncate(msg.Content, 50))

	// Get or create chat state
	state := b.getChatState(msg.ChatID)

	state.mu.Lock()
	if state.Processing {
		state.mu.Unlock()
		// Queue or ignore - for now, just notify user
		b.feishuClient.SendText(msg.ChatID, "⏳ 正在處理上一個請求，請稍候...")
		return
	}
	state.Processing = true
	state.MsgID = msg.MsgID
	state.mu.Unlock()

	// Add processing reaction
	b.feishuClient.AddReaction(msg.MsgID, "OnIt")

	// Download images if any
	var imagePaths []string
	for _, imageKey := range msg.ImageKeys {
		path, err := b.feishuClient.DownloadImage(msg.MsgID, imageKey)
		if err != nil {
			fmt.Printf("[Bridge] Failed to download image %s: %v\n", imageKey, err)
			continue
		}
		imagePaths = append(imagePaths, path)
	}

	// Process in goroutine
	go b.processMessageV2(msg.ChatID, msg.MsgID, msg.Content, imagePaths, state)
}

func (b *Bridge) processMessageV2(chatID, msgID, content string, imagePaths []string, state *ChatState) {
	defer func() {
		state.mu.Lock()
		state.Processing = false
		state.mu.Unlock()
	}()

	ctx := b.ctx

	// Get or create session
	entry, err := b.sessionStore.GetByChatID(chatID)
	if err != nil {
		fmt.Printf("[Bridge] Failed to get session: %v\n", err)
	}

	var threadID string

	if entry == nil || !b.sessionStore.IsFresh(entry) {
		// Create new thread
		fmt.Printf("[Bridge] Creating new thread for chat %s\n", chatID)
		threadID, err = b.codexClient.ThreadStart(ctx, nil)
		if err != nil {
			errMsg := fmt.Sprintf("❌ 創建會話失敗: %v", err)
			b.feishuClient.SendText(chatID, errMsg)
			return
		}

		// Save session
		b.sessionStore.Create(chatID, threadID)
		fmt.Printf("[Bridge] Created thread %s for chat %s\n", threadID, chatID)
	} else {
		threadID = entry.ThreadID
		fmt.Printf("[Bridge] Resuming thread %s for chat %s\n", threadID, chatID)
	}

	// Update state
	state.mu.Lock()
	state.ThreadID = threadID
	state.Buffer.Reset()
	state.mu.Unlock()

	// Start turn with images
	turnID, err := b.codexClient.TurnStart(ctx, threadID, content, imagePaths)
	if err != nil {
		// If thread not found, create a new one and retry
		if strings.Contains(err.Error(), "thread not found") {
			fmt.Printf("[Bridge] Thread %s not found, creating new one\n", threadID)
			b.sessionStore.Delete(chatID)

			threadID, err = b.codexClient.ThreadStart(ctx, nil)
			if err != nil {
				errMsg := fmt.Sprintf("❌ 創建會話失敗: %v", err)
				b.feishuClient.SendText(chatID, errMsg)
				return
			}
			b.sessionStore.Create(chatID, threadID)

			state.mu.Lock()
			state.ThreadID = threadID
			state.mu.Unlock()

			turnID, err = b.codexClient.TurnStart(ctx, threadID, content, imagePaths)
			if err != nil {
				errMsg := fmt.Sprintf("❌ 發送請求失敗: %v", err)
				b.feishuClient.SendText(chatID, errMsg)
				return
			}
		} else {
			errMsg := fmt.Sprintf("❌ 發送請求失敗: %v", err)
			b.feishuClient.SendText(chatID, errMsg)
			return
		}
	}

	state.mu.Lock()
	state.TurnID = turnID
	state.mu.Unlock()

	fmt.Printf("[Bridge] Started turn %s in thread %s\n", turnID, threadID)

	// Update session timestamp
	b.sessionStore.Touch(chatID)
}

func (b *Bridge) processEvents() {
	defer b.wg.Done()

	for event := range b.codexClient.Events() {
		b.handleEvent(event)
	}
}

func (b *Bridge) handleEvent(event codex.Event) {
	switch event.Method {
	case codex.MethodAgentMessageDelta:
		var params codex.AgentMessageDeltaParams
		if err := json.Unmarshal(event.Params, &params); err != nil {
			fmt.Printf("[Bridge] Failed to parse agent message delta: %v\n", err)
			return
		}
		b.handleAgentDelta(params)

	case codex.MethodTurnCompleted:
		var params codex.TurnCompletedParams
		if err := json.Unmarshal(event.Params, &params); err != nil {
			fmt.Printf("[Bridge] Failed to parse turn completed: %v\n", err)
			return
		}
		b.handleTurnCompleted(params)

	case codex.MethodItemStarted:
		var params codex.ItemStartedParams
		if err := json.Unmarshal(event.Params, &params); err != nil {
			return
		}
		if b.config.Debug {
			fmt.Printf("[Bridge] Item started: %s (type: %s)\n", params.Item.ID, params.Item.Type)
		}

	case codex.MethodItemCompleted:
		var params codex.ItemCompletedParams
		if err := json.Unmarshal(event.Params, &params); err != nil {
			return
		}
		if b.config.Debug {
			fmt.Printf("[Bridge] Item completed: %s\n", params.Item.ID)
		}

	default:
		if b.config.Debug {
			fmt.Printf("[Bridge] Event: %s\n", event.Method)
		}
	}
}

func (b *Bridge) handleAgentDelta(params codex.AgentMessageDeltaParams) {
	// Find chat by thread ID
	chatID := b.findChatByThread(params.ThreadID)
	if chatID == "" {
		return
	}

	state := b.getChatState(chatID)
	state.mu.Lock()
	state.Buffer.WriteString(params.Delta)
	state.mu.Unlock()
}

func (b *Bridge) handleTurnCompleted(params codex.TurnCompletedParams) {
	// Find chat by thread ID
	chatID := b.findChatByThread(params.ThreadID)
	if chatID == "" {
		fmt.Printf("[Bridge] Turn completed but no chat found for thread %s\n", params.ThreadID)
		return
	}

	state := b.getChatState(chatID)
	state.mu.Lock()
	response := state.Buffer.String()
	msgID := state.MsgID
	state.Buffer.Reset()
	state.mu.Unlock()

	if response == "" {
		response = "✅ (無文字回應)"
	}

	// Add completion reaction
	if msgID != "" {
		b.feishuClient.AddReaction(msgID, "DONE")
	}

	// Send to Feishu
	fmt.Printf("[Bridge] Turn completed, sending %d chars to %s\n", len(response), chatID)
	if err := b.feishuClient.SendText(chatID, response); err != nil {
		fmt.Printf("[Bridge] Failed to send response: %v\n", err)
	}

	// Update session timestamp
	b.sessionStore.Touch(chatID)
}

func (b *Bridge) getChatState(chatID string) *ChatState {
	b.chatStatesMu.Lock()
	defer b.chatStatesMu.Unlock()

	state, ok := b.chatStates[chatID]
	if !ok {
		state = &ChatState{}
		b.chatStates[chatID] = state
	}
	return state
}

func (b *Bridge) findChatByThread(threadID string) string {
	b.chatStatesMu.RLock()
	defer b.chatStatesMu.RUnlock()

	for chatID, state := range b.chatStates {
		if state.ThreadID == threadID {
			return chatID
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// StartSessionCleanup starts a goroutine to periodically clean up stale sessions
func (b *Bridge) StartSessionCleanup(interval time.Duration) {
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				count, err := b.sessionStore.CleanupStale()
				if err != nil {
					fmt.Printf("[Bridge] Session cleanup error: %v\n", err)
				} else if count > 0 {
					fmt.Printf("[Bridge] Cleaned up %d stale sessions\n", count)
				}
			case <-b.ctx.Done():
				return
			}
		}
	}()
}
