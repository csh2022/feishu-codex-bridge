package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

	// Codex process lifecycle (single app-server instance)
	codexMu       sync.Mutex
	activeThreads map[string]struct{}
	activeMu      sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type ChatState struct {
	ThreadID   string
	TurnID     string
	MsgID      string // Current message ID for reactions
	Processing bool
	Gen        uint64
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
		activeThreads: make(map[string]struct{}),
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
	b.startEventProcessor(b.codexClient)

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

	if cmd, ok := ParseCommand(msg.Content); ok {
		switch cmd.Kind {
		case CommandShowDir:
			wd := b.config.WorkingDir
			if abs, err := filepath.Abs(wd); err == nil {
				wd = abs
			}
			b.feishuClient.SendText(msg.ChatID, fmt.Sprintf("📁 当前工作目录：%s\n发送 /help 查看可用命令。", wd))
			return

		case CommandHelp:
			b.feishuClient.SendText(msg.ChatID, strings.Join([]string{
				"可用命令：",
				"/help                查看帮助",
				"/pwd                 查看当前工作目录",
				"/cd <绝对路径>        切换工作目录",
				"/workdir <绝对路径>   切换工作目录",
				"/clear               清空当前会话上下文（不中断 bridge/不切目录）",
			}, "\n"))
			return

		case CommandClear:
			b.clearChatContext(msg.ChatID)
			b.feishuClient.SendText(msg.ChatID, "✅ 已清空当前会话上下文（保持当前工作目录不变）。")
			return

		case CommandSwitchDir:
			if err := b.switchWorkingDir(msg.ChatID, cmd.Arg); err != nil {
				b.feishuClient.SendText(msg.ChatID, fmt.Sprintf("❌ 切换工作目录失败：%v", err))
			} else {
				b.feishuClient.SendText(msg.ChatID, fmt.Sprintf("✅ 已切换工作目录：%s", b.config.WorkingDir))
			}
			return
		}
	}

	// Get or create chat state
	state := b.getChatState(msg.ChatID)

	var gen uint64
	state.mu.Lock()
	if state.Processing {
		state.mu.Unlock()
		// Queue or ignore - for now, just notify user
		b.feishuClient.SendText(msg.ChatID, "⏳ 正在處理上一個請求，請稍候...")
		return
	}
	state.Processing = true
	state.MsgID = msg.MsgID
	gen = state.Gen
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
	go b.processMessageV2(msg.ChatID, msg.MsgID, msg.Content, imagePaths, state, gen)
}

func (b *Bridge) processMessageV2(chatID, msgID, content string, imagePaths []string, state *ChatState, gen uint64) {
	defer func() {
		state.mu.Lock()
		state.Processing = false
		state.mu.Unlock()
	}()

	ctx := b.ctx
	sendText := func(text string) bool {
		state.mu.Lock()
		current := state.Gen
		state.mu.Unlock()
		if current != gen {
			return false
		}
		_ = b.feishuClient.SendText(chatID, text)
		return true
	}

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
			sendText(errMsg)
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
				sendText(errMsg)
				return
			}
			b.sessionStore.Create(chatID, threadID)

			state.mu.Lock()
			state.ThreadID = threadID
			state.mu.Unlock()

			turnID, err = b.codexClient.TurnStart(ctx, threadID, content, imagePaths)
			if err != nil {
				errMsg := fmt.Sprintf("❌ 發送請求失敗: %v", err)
				sendText(errMsg)
				return
			}
		} else {
			errMsg := fmt.Sprintf("❌ 發送請求失敗: %v", err)
			sendText(errMsg)
			return
		}
	}

	state.mu.Lock()
	state.TurnID = turnID
	state.mu.Unlock()

	b.activeMu.Lock()
	b.activeThreads[threadID] = struct{}{}
	b.activeMu.Unlock()

	fmt.Printf("[Bridge] Started turn %s in thread %s\n", turnID, threadID)

	// Update session timestamp
	b.sessionStore.Touch(chatID)
}

func (b *Bridge) startEventProcessor(client *codex.Client) {
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for event := range client.Events() {
			b.handleEvent(event)
		}
	}()
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
	b.activeMu.Lock()
	delete(b.activeThreads, params.ThreadID)
	b.activeMu.Unlock()

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

func (b *Bridge) switchWorkingDir(chatID, newDir string) error {
	b.codexMu.Lock()
	defer b.codexMu.Unlock()

	b.activeMu.Lock()
	active := len(b.activeThreads)
	b.activeMu.Unlock()
	if active > 0 {
		return fmt.Errorf("当前有 %d 个任务正在运行，请等待完成后再切换", active)
	}

	absDir, err := filepath.Abs(newDir)
	if err != nil {
		return fmt.Errorf("无效路径：%w", err)
	}
	info, err := os.Stat(absDir)
	if err != nil {
		return fmt.Errorf("目录不存在或不可访问：%w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("不是目录：%s", absDir)
	}
	if absDir == b.config.WorkingDir {
		return nil
	}

	// Stop old server and start a new one under the new working directory.
	_ = b.codexClient.Stop()

	newClient := codex.NewClient(absDir, b.config.CodexModel)
	if err := newClient.Start(b.ctx); err != nil {
		// Try to restore previous client to keep bridge usable.
		restore := codex.NewClient(b.config.WorkingDir, b.config.CodexModel)
		if restoreErr := restore.Start(b.ctx); restoreErr == nil {
			b.codexClient = restore
			b.startEventProcessor(b.codexClient)
		}
		return fmt.Errorf("启动 Codex 失败：%w", err)
	}

	b.codexClient = newClient
	b.config.WorkingDir = absDir
	b.startEventProcessor(b.codexClient)

	// Reset the session for this chat to avoid resuming threads from the old server.
	_ = b.sessionStore.Delete(chatID)
	state := b.getChatState(chatID)
	state.mu.Lock()
	state.ThreadID = ""
	state.TurnID = ""
	state.Buffer.Reset()
	state.mu.Unlock()

	// Clear any stale in-flight state.
	b.activeMu.Lock()
	b.activeThreads = make(map[string]struct{})
	b.activeMu.Unlock()

	return nil
}

func (b *Bridge) clearChatContext(chatID string) {
	b.codexMu.Lock()
	defer b.codexMu.Unlock()

	state := b.getChatState(chatID)

	var threadID string
	state.mu.Lock()
	threadID = state.ThreadID
	state.Gen++
	state.Processing = false
	state.ThreadID = ""
	state.TurnID = ""
	state.MsgID = ""
	state.LastItem = ""
	state.Buffer.Reset()
	state.mu.Unlock()

	if threadID != "" {
		_ = b.codexClient.TurnInterrupt(b.ctx, threadID)
	}

	_ = b.sessionStore.Delete(chatID)

	b.activeMu.Lock()
	if threadID != "" {
		delete(b.activeThreads, threadID)
	}
	b.activeMu.Unlock()
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
