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

	queuesMu   sync.Mutex
	chatQueues map[string]*chatQueue

	recalledMu  sync.Mutex
	recalled    map[string]map[string]struct{}
	recalledAll map[string]struct{}

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type chatQueue struct {
	ch      chan *feishu.Message
	pending []*feishu.Message
	mu      sync.Mutex
}

type ChatState struct {
	ThreadID             string
	TurnID               string
	MsgID                string // Current message ID for reactions
	ProcessingReactionID string
	Processing           bool
	Gen                  uint64
	ChatType             string
	done                 chan struct{}
	Buffer               strings.Builder
	LastItem             string
	mu                   sync.Mutex
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
		config:        config,
		feishuClient:  feishuClient,
		codexClient:   codexClient,
		sessionStore:  sessionStore,
		chatStates:    make(map[string]*ChatState),
		activeThreads: make(map[string]struct{}),
		chatQueues:    make(map[string]*chatQueue),
		recalled:      make(map[string]map[string]struct{}),
		recalledAll:   make(map[string]struct{}),
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
	b.feishuClient.OnMessageRecalled(b.handleFeishuMessageRecalled)

	// Start session cleanup
	b.StartSessionCleanup(10 * time.Minute)

	// Start Feishu WebSocket in background; we block on context cancellation
	// so Stop() can always unblock Start(), even if the SDK call doesn't return promptly.
	fmt.Println("[Bridge] Starting Feishu connection...")
	feishuErrCh := make(chan error, 1)
	go func() {
		feishuErrCh <- b.feishuClient.Start()
	}()

	select {
	case err := <-feishuErrCh:
		return err
	case <-b.ctx.Done():
		return nil
	}
}

func (b *Bridge) Stop() {
	fmt.Println("[Bridge] Stopping...")

	if b.cancel != nil {
		b.cancel()
	}
	b.feishuClient.Stop()
	b.codexClient.Stop()
	b.sessionStore.Close()

	b.closeAllChatQueues()

	b.wg.Wait()
	fmt.Println("[Bridge] Stopped")
}

func (b *Bridge) handleFeishuMessageV2(msg *feishu.Message) {
	fmt.Printf("[Bridge] Received %s from %s: %s\n", msg.MsgType, msg.ChatID, truncate(msg.Content, 50))

	if cmd, ok := ParseCommand(msg.Content); ok {
		replyInThread := msg.ChatType == "group"
		switch cmd.Kind {
		case CommandShowDir:
			wd := b.config.WorkingDir
			if abs, err := filepath.Abs(wd); err == nil {
				wd = abs
			}
			if err := b.feishuClient.ReplyText(msg.MsgID, fmt.Sprintf("📁 当前工作目录：%s\n发送 /help 查看可用命令。", wd), replyInThread); err != nil {
				b.feishuClient.SendText(msg.ChatID, fmt.Sprintf("📁 当前工作目录：%s\n发送 /help 查看可用命令。", wd))
			}
			return

		case CommandHelp:
			helpText := strings.Join([]string{
				"可用命令：",
				"/help 或 /h           查看帮助",
				"/pwd                 查看当前工作目录",
				"/cd <绝对路径>        切换工作目录",
				"/workdir <绝对路径> 或 /w <绝对路径>   切换工作目录",
				"/clear 或 /c          清空当前会话上下文",
				"/queue 或 /q          查看队列",
			}, "\n")
			if err := b.feishuClient.ReplyText(msg.MsgID, helpText, replyInThread); err != nil {
				b.feishuClient.SendText(msg.ChatID, helpText)
			}
			return

		case CommandQueue:
			text := b.formatQueueStatus(msg.ChatID)
			if err := b.feishuClient.ReplyText(msg.MsgID, text, replyInThread); err != nil {
				_ = b.feishuClient.SendText(msg.ChatID, text)
			}
			return

		case CommandClear:
			b.clearChatContext(msg.ChatID)
			if err := b.feishuClient.ReplyText(msg.MsgID, "✅ 已清空当前会话上下文（保持当前工作目录不变）。", replyInThread); err != nil {
				b.feishuClient.SendText(msg.ChatID, "✅ 已清空当前会话上下文（保持当前工作目录不变）。")
			}
			return

		case CommandSwitchDir:
			if err := b.switchWorkingDir(msg.ChatID, cmd.Arg); err != nil {
				if err2 := b.feishuClient.ReplyText(msg.MsgID, fmt.Sprintf("❌ 切换工作目录失败：%v", err), replyInThread); err2 != nil {
					b.feishuClient.SendText(msg.ChatID, fmt.Sprintf("❌ 切换工作目录失败：%v", err))
				}
			} else {
				if err2 := b.feishuClient.ReplyText(msg.MsgID, fmt.Sprintf("✅ 已切换工作目录：%s", b.config.WorkingDir), replyInThread); err2 != nil {
					b.feishuClient.SendText(msg.ChatID, fmt.Sprintf("✅ 已切换工作目录：%s", b.config.WorkingDir))
				}
			}
			return
		}
	}

	b.enqueueMessage(msg)
}

func (b *Bridge) enqueueMessage(msg *feishu.Message) {
	if b.ctx != nil {
		select {
		case <-b.ctx.Done():
			return
		default:
		}
	}

	if b.isRecalled(msg.ChatID, msg.MsgID) {
		return
	}

	b.queuesMu.Lock()
	q, ok := b.chatQueues[msg.ChatID]
	if !ok {
		q = &chatQueue{
			ch: make(chan *feishu.Message, 100),
		}
		b.chatQueues[msg.ChatID] = q
		b.wg.Add(1)
		go b.chatWorker(msg.ChatID, q)
	}
	b.queuesMu.Unlock()

	q.mu.Lock()
	q.pending = append(q.pending, msg)
	q.mu.Unlock()

	if !b.trySendQueue(q.ch, msg) {
		q.mu.Lock()
		q.pending = removePendingByMsgID(q.pending, msg.MsgID)
		q.mu.Unlock()
		_ = b.feishuClient.ReplyText(msg.MsgID, "⚠️ 排队消息过多，请稍后再试。", msg.ChatType == "group")
	}
}

func (b *Bridge) trySendQueue(q chan *feishu.Message, msg *feishu.Message) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
		}
	}()

	select {
	case q <- msg:
		return true
	default:
		return false
	}
}

func (b *Bridge) chatWorker(chatID string, q *chatQueue) {
	defer b.wg.Done()
	for {
		select {
		case <-b.ctx.Done():
			return
		case msg, ok := <-q.ch:
			if !ok {
				return
			}
			if msg == nil {
				continue
			}
			q.mu.Lock()
			q.pending = removePendingByMsgID(q.pending, msg.MsgID)
			q.mu.Unlock()
			b.processQueuedMessage(chatID, msg)
		}
	}
}

func (b *Bridge) processQueuedMessage(chatID string, msg *feishu.Message) {
	state := b.getChatState(chatID)

	if b.isRecalled(msg.ChatID, msg.MsgID) {
		b.clearRecalled(msg.ChatID, msg.MsgID)
		return
	}

	state.mu.Lock()
	state.Processing = true
	state.MsgID = msg.MsgID
	state.ProcessingReactionID = ""
	state.ChatType = msg.ChatType
	gen := state.Gen
	done := make(chan struct{})
	state.done = done
	state.Buffer.Reset()
	state.mu.Unlock()

	defer func() {
		var msgID string
		var reactionID string
		shouldClose := false
		state.mu.Lock()
		// If another generation started (e.g. /clear), don't touch state.
		if state.Gen == gen {
			msgID = state.MsgID
			reactionID = state.ProcessingReactionID
			shouldClose = state.done == done && state.done != nil
			state.Processing = false
			state.done = nil
			state.ProcessingReactionID = ""
		}
		state.mu.Unlock()
		if msgID != "" && reactionID != "" {
			_ = b.feishuClient.RemoveReaction(msgID, reactionID)
		}
		if shouldClose {
			close(done)
		}
	}()

	replyInThread := msg.ChatType == "group"
	if reactionID, err := b.feishuClient.AddReaction(msg.MsgID, "Typing"); err == nil {
		state.mu.Lock()
		if state.Gen == gen {
			state.ProcessingReactionID = reactionID
		}
		state.mu.Unlock()
	}

	sendReply := func(text string) bool {
		state.mu.Lock()
		current := state.Gen
		state.mu.Unlock()
		if current != gen {
			return false
		}
		if b.isRecalled(msg.ChatID, msg.MsgID) {
			return false
		}
		if err := b.feishuClient.ReplyText(msg.MsgID, text, replyInThread); err != nil {
			_ = b.feishuClient.SendText(chatID, text)
		}
		return true
	}

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

	ctx := b.ctx

	// Get or create session
	entry, err := b.sessionStore.GetByChatID(chatID)
	if err != nil {
		fmt.Printf("[Bridge] Failed to get session: %v\n", err)
	}

	var threadID string
	if entry == nil || !b.sessionStore.IsFresh(entry) {
		fmt.Printf("[Bridge] Creating new thread for chat %s\n", chatID)
		threadID, err = b.codexClient.ThreadStart(ctx, nil)
		if err != nil {
			sendReply(fmt.Sprintf("❌ 创建会话失败: %v", err))
			return
		}
		b.sessionStore.Create(chatID, threadID)
		fmt.Printf("[Bridge] Created thread %s for chat %s\n", threadID, chatID)
	} else {
		threadID = entry.ThreadID
		fmt.Printf("[Bridge] Resuming thread %s for chat %s\n", threadID, chatID)
	}

	state.mu.Lock()
	if state.Gen != gen {
		state.mu.Unlock()
		return
	}
	state.ThreadID = threadID
	state.mu.Unlock()

	turnID, err := b.codexClient.TurnStart(ctx, threadID, msg.Content, imagePaths)
	if err != nil {
		if strings.Contains(err.Error(), "thread not found") {
			fmt.Printf("[Bridge] Thread %s not found, creating new one\n", threadID)
			_ = b.sessionStore.Delete(chatID)
			threadID, err = b.codexClient.ThreadStart(ctx, nil)
			if err != nil {
				sendReply(fmt.Sprintf("❌ 创建会话失败: %v", err))
				return
			}
			_, _ = b.sessionStore.Create(chatID, threadID)
			state.mu.Lock()
			if state.Gen != gen {
				state.mu.Unlock()
				return
			}
			state.ThreadID = threadID
			state.mu.Unlock()
			turnID, err = b.codexClient.TurnStart(ctx, threadID, msg.Content, imagePaths)
			if err != nil {
				sendReply(fmt.Sprintf("❌ 发送请求失败: %v", err))
				return
			}
		} else {
			sendReply(fmt.Sprintf("❌ 发送请求失败: %v", err))
			return
		}
	}

	state.mu.Lock()
	if state.Gen != gen {
		state.mu.Unlock()
		return
	}
	state.TurnID = turnID
	state.mu.Unlock()

	b.activeMu.Lock()
	b.activeThreads[threadID] = struct{}{}
	b.activeMu.Unlock()

	fmt.Printf("[Bridge] Started turn %s in thread %s\n", turnID, threadID)
	_ = b.sessionStore.Touch(chatID)

	select {
	case <-done:
	case <-b.ctx.Done():
		return
	}
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
	processingReactionID := state.ProcessingReactionID
	chatType := state.ChatType
	done := state.done
	state.Buffer.Reset()
	state.done = nil
	state.Processing = false
	state.ProcessingReactionID = ""
	state.mu.Unlock()

	if response == "" {
		response = "✅（无文字回应）"
	}

	// Replace "OnIt" reaction with completion reaction
	if msgID != "" && processingReactionID != "" {
		_ = b.feishuClient.RemoveReaction(msgID, processingReactionID)
	}
	if msgID != "" {
		_, _ = b.feishuClient.AddReaction(msgID, "DONE")
	}

	// Send to Feishu
	fmt.Printf("[Bridge] Turn completed, sending %d chars to %s\n", len(response), chatID)
	replyInThread := chatType == "group"
	if msgID != "" {
		if err := b.feishuClient.ReplyText(msgID, response, replyInThread); err != nil {
			fmt.Printf("[Bridge] Failed to reply response: %v\n", err)
			if err := b.feishuClient.SendText(chatID, response); err != nil {
				fmt.Printf("[Bridge] Failed to send response: %v\n", err)
			}
		}
	} else {
		if err := b.feishuClient.SendText(chatID, response); err != nil {
			fmt.Printf("[Bridge] Failed to send response: %v\n", err)
		}
	}

	// Update session timestamp
	b.sessionStore.Touch(chatID)

	if done != nil {
		close(done)
	}
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
	if state.done != nil {
		close(state.done)
		state.done = nil
	}
	state.Buffer.Reset()
	state.mu.Unlock()

	// Clear any stale in-flight state.
	b.activeMu.Lock()
	b.activeThreads = make(map[string]struct{})
	b.activeMu.Unlock()

	// Drop queued messages for this chat (they were intended for the previous workdir).
	b.queuesMu.Lock()
	if q, ok := b.chatQueues[chatID]; ok {
		q.mu.Lock()
		q.pending = nil
		q.mu.Unlock()
		for {
			select {
			case <-q.ch:
			default:
				goto drained
			}
		}
	drained:
	}
	b.queuesMu.Unlock()

	return nil
}

func (b *Bridge) clearChatContext(chatID string) {
	b.codexMu.Lock()
	defer b.codexMu.Unlock()

	state := b.getChatState(chatID)

	var threadID string
	var msgID string
	var reactionID string
	state.mu.Lock()
	threadID = state.ThreadID
	msgID = state.MsgID
	reactionID = state.ProcessingReactionID
	if state.done != nil {
		close(state.done)
		state.done = nil
	}
	state.Gen++
	state.Processing = false
	state.ThreadID = ""
	state.TurnID = ""
	state.MsgID = ""
	state.ProcessingReactionID = ""
	state.LastItem = ""
	state.Buffer.Reset()
	state.mu.Unlock()

	if threadID != "" {
		_ = b.codexClient.TurnInterrupt(b.ctx, threadID)
	}
	if msgID != "" && reactionID != "" {
		_ = b.feishuClient.RemoveReaction(msgID, reactionID)
	}

	_ = b.sessionStore.Delete(chatID)

	b.activeMu.Lock()
	if threadID != "" {
		delete(b.activeThreads, threadID)
	}
	b.activeMu.Unlock()

	// Drop queued messages for this chat.
	b.queuesMu.Lock()
	if q, ok := b.chatQueues[chatID]; ok {
		q.mu.Lock()
		q.pending = nil
		q.mu.Unlock()
		for {
			select {
			case <-q.ch:
			default:
				goto drained
			}
		}
	drained:
	}
	b.queuesMu.Unlock()
}

func (b *Bridge) handleFeishuMessageRecalled(ev *feishu.MessageRecalled) {
	if ev == nil || ev.ChatID == "" || ev.MsgID == "" {
		// Some recall events might not include chat_id; we still try best-effort removal by msgID.
		if ev != nil && ev.MsgID != "" {
			b.markRecalled("", ev.MsgID)
			b.dropPendingMessageAllChats(ev.MsgID)
			b.clearChatContextByMsgID(ev.MsgID)
		}
		return
	}

	b.markRecalled(ev.ChatID, ev.MsgID)

	// If this message is currently being processed, interrupt and clear context
	// to avoid sending a reply to a recalled message and to avoid polluting the session.
	state := b.getChatState(ev.ChatID)
	state.mu.Lock()
	currentMsgID := state.MsgID
	state.mu.Unlock()
	if currentMsgID == ev.MsgID {
		b.clearChatContext(ev.ChatID)
	}

	// Remove from pending list for display and to reduce queue pressure.
	b.dropPendingMessage(ev.ChatID, ev.MsgID)
	// Also best-effort remove across all chats to guard against chat_id mismatches.
	b.dropPendingMessageAllChats(ev.MsgID)
}

func (b *Bridge) markRecalled(chatID, msgID string) {
	b.recalledMu.Lock()
	defer b.recalledMu.Unlock()
	b.recalledAll[msgID] = struct{}{}
	if chatID == "" {
		return
	}
	m, ok := b.recalled[chatID]
	if !ok {
		m = make(map[string]struct{})
		b.recalled[chatID] = m
	}
	m[msgID] = struct{}{}
}

func (b *Bridge) isRecalled(chatID, msgID string) bool {
	b.recalledMu.Lock()
	defer b.recalledMu.Unlock()
	if _, ok := b.recalledAll[msgID]; ok {
		return true
	}
	m, ok := b.recalled[chatID]
	if !ok {
		return false
	}
	_, ok = m[msgID]
	return ok
}

func (b *Bridge) clearRecalled(chatID, msgID string) {
	b.recalledMu.Lock()
	defer b.recalledMu.Unlock()
	delete(b.recalledAll, msgID)
	if chatID == "" {
		return
	}
	if m, ok := b.recalled[chatID]; ok {
		delete(m, msgID)
		if len(m) == 0 {
			delete(b.recalled, chatID)
		}
	}
}

func (b *Bridge) closeAllChatQueues() {
	b.queuesMu.Lock()
	defer b.queuesMu.Unlock()
	for chatID, q := range b.chatQueues {
		_ = chatID
		close(q.ch)
	}
	b.chatQueues = make(map[string]*chatQueue)
}

func (b *Bridge) formatQueueStatus(chatID string) string {
	state := b.getChatState(chatID)
	state.mu.Lock()
	processing := state.Processing
	currentMsgID := state.MsgID
	state.mu.Unlock()

	pending := []*feishu.Message(nil)
	b.queuesMu.Lock()
	q := b.chatQueues[chatID]
	b.queuesMu.Unlock()
	if q != nil {
		q.mu.Lock()
		pending = append(pending, q.pending...)
		q.mu.Unlock()
	}

	lines := []string{}
	if processing && currentMsgID != "" {
		lines = append(lines, fmt.Sprintf("正在处理：%s", currentMsgID))
	} else {
		lines = append(lines, "正在处理：无")
	}
	lines = append(lines, fmt.Sprintf("待处理：%d", len(pending)))
	for i, m := range pending {
		if m == nil {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			content = "(空)"
		}
		if len(content) > 80 {
			content = content[:80] + "..."
		}
		lines = append(lines, fmt.Sprintf("%d) %s", i+1, content))
	}
	return strings.Join(lines, "\n")
}

func (b *Bridge) dropPendingMessage(chatID, msgID string) {
	b.queuesMu.Lock()
	q := b.chatQueues[chatID]
	b.queuesMu.Unlock()
	if q == nil {
		return
	}
	q.mu.Lock()
	q.pending = removePendingByMsgID(q.pending, msgID)
	q.mu.Unlock()
}

func (b *Bridge) dropPendingMessageAllChats(msgID string) {
	b.queuesMu.Lock()
	qs := make([]*chatQueue, 0, len(b.chatQueues))
	for _, q := range b.chatQueues {
		qs = append(qs, q)
	}
	b.queuesMu.Unlock()

	for _, q := range qs {
		if q == nil {
			continue
		}
		q.mu.Lock()
		q.pending = removePendingByMsgID(q.pending, msgID)
		q.mu.Unlock()
	}
}

func (b *Bridge) clearChatContextByMsgID(msgID string) {
	b.chatStatesMu.RLock()
	defer b.chatStatesMu.RUnlock()
	for chatID, st := range b.chatStates {
		st.mu.Lock()
		current := st.MsgID
		st.mu.Unlock()
		if current == msgID {
			go b.clearChatContext(chatID)
		}
	}
}
func removePendingByMsgID(pending []*feishu.Message, msgID string) []*feishu.Message {
	if len(pending) == 0 {
		return pending
	}
	n := 0
	for _, m := range pending {
		if m == nil || m.MsgID != msgID {
			pending[n] = m
			n++
		}
	}
	return pending[:n]
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
