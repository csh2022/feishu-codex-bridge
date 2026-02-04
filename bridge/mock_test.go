package bridge

import (
	"context"

	"github.com/anthropics/feishu-codex-bridge/codex"
	"github.com/anthropics/feishu-codex-bridge/feishu"
)

// MockFeishuClient is a mock implementation of FeishuClient for testing
type MockFeishuClient struct {
	OnMessageHandler  feishu.MessageHandler
	OnRecalledHandler feishu.MessageRecalledHandler
	DebugEnabled      bool
	SentMessages      []MockSentMessage
	Reactions         []MockReaction
	DownloadedImages  []string
	DownloadDir       string
	StartError        error
}

type MockSentMessage struct {
	ChatID  string
	MsgID   string
	Text    string
	IsRich  bool
	Title   string
	Content [][]map[string]interface{}
	IsReply bool
}

type MockReaction struct {
	MessageID  string
	EmojiType  string
	ReactionID string
	IsRemove   bool
}

func (m *MockFeishuClient) OnMessage(handler feishu.MessageHandler) {
	m.OnMessageHandler = handler
}

func (m *MockFeishuClient) OnMessageRecalled(handler feishu.MessageRecalledHandler) {
	m.OnRecalledHandler = handler
}

func (m *MockFeishuClient) SetDebug(enabled bool) {
	m.DebugEnabled = enabled
}

func (m *MockFeishuClient) Start() error {
	return m.StartError
}

func (m *MockFeishuClient) Stop() {}

func (m *MockFeishuClient) SendText(chatID, text string) error {
	m.SentMessages = append(m.SentMessages, MockSentMessage{
		ChatID: chatID,
		Text:   text,
	})
	return nil
}

func (m *MockFeishuClient) SendRichText(chatID, title string, content [][]map[string]interface{}) error {
	m.SentMessages = append(m.SentMessages, MockSentMessage{
		ChatID:  chatID,
		IsRich:  true,
		Title:   title,
		Content: content,
	})
	return nil
}

func (m *MockFeishuClient) ReplyText(messageID, text string, replyInThread bool) error {
	m.SentMessages = append(m.SentMessages, MockSentMessage{
		MsgID:   messageID,
		Text:    text,
		IsReply: true,
	})
	return nil
}

func (m *MockFeishuClient) ReplyRichText(messageID, title string, content [][]map[string]interface{}, replyInThread bool) error {
	m.SentMessages = append(m.SentMessages, MockSentMessage{
		MsgID:   messageID,
		IsRich:  true,
		Title:   title,
		Content: content,
		IsReply: true,
	})
	return nil
}

func (m *MockFeishuClient) AddReaction(messageID, emojiType string) (string, error) {
	reactionID := "mock-reaction-" + emojiType + "-" + messageID
	m.Reactions = append(m.Reactions, MockReaction{
		MessageID:  messageID,
		EmojiType:  emojiType,
		ReactionID: reactionID,
	})
	return reactionID, nil
}

func (m *MockFeishuClient) RemoveReaction(messageID, reactionID string) error {
	m.Reactions = append(m.Reactions, MockReaction{
		MessageID:  messageID,
		ReactionID: reactionID,
		IsRemove:   true,
	})
	return nil
}

func (m *MockFeishuClient) DownloadImage(messageID, imageKey string) (string, error) {
	path := "/tmp/images/" + imageKey + ".png"
	m.DownloadedImages = append(m.DownloadedImages, path)
	return path, nil
}

func (m *MockFeishuClient) SetDownloadDir(dir string) {
	m.DownloadDir = dir
}

// MockCodexClient is a mock implementation of CodexClient for testing
type MockCodexClient struct {
	EventsChan       chan codex.Event
	Running          bool
	Initialized      bool
	StartError       error
	ThreadStartError error
	TurnStartError   error
	CreatedThreads   []string
	StartedTurns     []MockTurn
	NextThreadID     string
	NextTurnID       string
}

type MockTurn struct {
	ThreadID string
	Prompt   string
	Images   []string
}

func NewMockCodexClient() *MockCodexClient {
	return &MockCodexClient{
		EventsChan:   make(chan codex.Event, 100),
		NextThreadID: "mock-thread-123",
		NextTurnID:   "mock-turn-456",
	}
}

func (m *MockCodexClient) Start(ctx context.Context) error {
	if m.StartError != nil {
		return m.StartError
	}
	m.Running = true
	m.Initialized = true
	return nil
}

func (m *MockCodexClient) Stop() error {
	m.Running = false
	close(m.EventsChan)
	return nil
}

func (m *MockCodexClient) Events() <-chan codex.Event {
	return m.EventsChan
}

func (m *MockCodexClient) IsRunning() bool {
	return m.Running && m.Initialized
}

func (m *MockCodexClient) ThreadStart(ctx context.Context, params *codex.ThreadStartParams) (string, error) {
	if m.ThreadStartError != nil {
		return "", m.ThreadStartError
	}
	threadID := m.NextThreadID
	m.CreatedThreads = append(m.CreatedThreads, threadID)
	return threadID, nil
}

func (m *MockCodexClient) ThreadResume(ctx context.Context, threadID string) (*codex.Thread, error) {
	return &codex.Thread{ID: threadID}, nil
}

func (m *MockCodexClient) TurnStart(ctx context.Context, threadID, prompt string, images []string) (string, error) {
	if m.TurnStartError != nil {
		return "", m.TurnStartError
	}
	m.StartedTurns = append(m.StartedTurns, MockTurn{
		ThreadID: threadID,
		Prompt:   prompt,
		Images:   images,
	})
	return m.NextTurnID, nil
}

func (m *MockCodexClient) TurnInterrupt(ctx context.Context, threadID string) error {
	return nil
}

func (m *MockCodexClient) RespondToApproval(requestID int64, decision string) error {
	return nil
}

// SendEvent sends an event through the mock client's event channel
func (m *MockCodexClient) SendEvent(event codex.Event) {
	m.EventsChan <- event
}
