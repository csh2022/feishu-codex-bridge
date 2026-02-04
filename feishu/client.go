package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

// Message represents a received Feishu message
type Message struct {
	ChatID    string
	MsgID     string
	MsgType   string   // text, image, post
	ChatType  string   // p2p (private), group
	Content   string   // Text content (extracted from all message types)
	ImageKeys []string // Image keys for downloading
	Sender    *Sender  // Message sender info
	Mentions  []string // Mentioned user IDs (including bot)
}

// Sender represents the message sender
type Sender struct {
	SenderID   string // User ID or bot ID
	SenderType string // user, bot
	TenantKey  string
}

// ChatMember represents a member in a chat
type ChatMember struct {
	MemberID   string `json:"member_id"`
	MemberType string `json:"member_type"` // user, bot
	Name       string `json:"name"`
}

// ChatInfo represents information about a chat
type ChatInfo struct {
	ChatID      string `json:"chat_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ChatType    string `json:"chat_type"` // p2p, group
	OwnerID     string `json:"owner_id"`
	MemberCount int    `json:"user_count"`
}

// HistoryMessage represents a message from chat history
type HistoryMessage struct {
	MsgID      string `json:"message_id"`
	MsgType    string `json:"msg_type"`
	Content    string `json:"content"`
	CreateTime string `json:"create_time"`
	Sender     *Sender
}

// MessageHandler is the callback for received messages
type MessageHandler func(msg *Message)

// MessageRecalled contains recall event info.
type MessageRecalled struct {
	ChatID string
	MsgID  string
}

// MessageRecalledHandler is the callback for recalled messages.
type MessageRecalledHandler func(ev *MessageRecalled)

// Client is the Feishu API client
type Client struct {
	appID       string
	appSecret   string
	larkCli     *lark.Client
	wsCli       *larkws.Client
	onMessage   MessageHandler
	onRecalled  MessageRecalledHandler
	downloadDir string
	ctx         context.Context
	cancel      context.CancelFunc
}

const defaultRequestTimeout = 20 * time.Second

// NewClient creates a new Feishu client
func NewClient(appID, appSecret string) *Client {
	return &Client{
		appID:       appID,
		appSecret:   appSecret,
		downloadDir: "/tmp/feishu-images",
	}
}

func (c *Client) requestContext() (context.Context, context.CancelFunc) {
	base := c.ctx
	if base == nil {
		base = context.Background()
	}
	return context.WithTimeout(base, defaultRequestTimeout)
}

// SetDownloadDir sets the directory for downloading images
func (c *Client) SetDownloadDir(dir string) {
	c.downloadDir = dir
}

// OnMessage sets the message handler
func (c *Client) OnMessage(handler MessageHandler) {
	c.onMessage = handler
}

func (c *Client) OnMessageRecalled(handler MessageRecalledHandler) {
	c.onRecalled = handler
}

// Start connects to Feishu via WebSocket and starts listening for messages
func (c *Client) Start() error {
	c.ctx, c.cancel = context.WithCancel(context.Background())

	// Create Lark API client
	c.larkCli = lark.NewClient(c.appID, c.appSecret)

	// Register event handler
	eventHandler := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			c.handleMessage(event)
			return nil
		}).
		OnP2MessageRecalledV1(func(ctx context.Context, event *larkim.P2MessageRecalledV1) error {
			c.handleRecalled(event)
			return nil
		})

	// Create WebSocket client
	c.wsCli = larkws.NewClient(c.appID, c.appSecret,
		larkws.WithEventHandler(eventHandler),
		larkws.WithLogLevel(larkcore.LogLevelInfo),
	)

	fmt.Println("[Feishu] Starting WebSocket connection...")

	// Start WebSocket (blocking)
	return c.wsCli.Start(c.ctx)
}

// Stop disconnects from Feishu
func (c *Client) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

// handleMessage processes incoming Feishu messages
func (c *Client) handleMessage(event *larkim.P2MessageReceiveV1) {
	rawMsg := event.Event.Message
	if rawMsg == nil {
		return
	}

	msg := &Message{
		ChatID:  *rawMsg.ChatId,
		MsgID:   *rawMsg.MessageId,
		MsgType: *rawMsg.MessageType,
	}

	// Parse chat type
	if rawMsg.ChatType != nil {
		msg.ChatType = *rawMsg.ChatType
	}

	// Parse sender info
	if event.Event.Sender != nil {
		msg.Sender = &Sender{}
		if event.Event.Sender.SenderId != nil {
			if event.Event.Sender.SenderId.OpenId != nil {
				msg.Sender.SenderID = *event.Event.Sender.SenderId.OpenId
			}
		}
		if event.Event.Sender.SenderType != nil {
			msg.Sender.SenderType = *event.Event.Sender.SenderType
		}
		if event.Event.Sender.TenantKey != nil {
			msg.Sender.TenantKey = *event.Event.Sender.TenantKey
		}
	}

	// Parse mentions
	if rawMsg.Mentions != nil {
		for _, mention := range rawMsg.Mentions {
			if mention.Id != nil && mention.Id.OpenId != nil {
				msg.Mentions = append(msg.Mentions, *mention.Id.OpenId)
			}
		}
	}

	switch msg.MsgType {
	case "text":
		msg.Content = c.parseTextContent(*rawMsg.Content)
	case "image":
		msg.ImageKeys = c.parseImageContent(*rawMsg.Content)
		msg.Content = "[图片]"
	case "post":
		content, imageKeys := c.parsePostContent(*rawMsg.Content)
		msg.Content = content
		msg.ImageKeys = imageKeys
	default:
		// Unsupported message type
		fmt.Printf("[Feishu] Unsupported message type: %s\n", msg.MsgType)
		return
	}

	fmt.Printf("[Feishu] Received %s from %s chat %s: %s\n", msg.MsgType, msg.ChatType, msg.ChatID, truncate(msg.Content, 50))

	if c.onMessage != nil {
		c.onMessage(msg)
	}
}

func (c *Client) handleRecalled(event *larkim.P2MessageRecalledV1) {
	if event == nil || event.Event == nil {
		return
	}
	if event.Event.MessageId == nil || *event.Event.MessageId == "" {
		return
	}

	chatID := ""
	if event.Event.ChatId != nil {
		chatID = *event.Event.ChatId
	}

	ev := &MessageRecalled{
		ChatID: chatID,
		MsgID:  *event.Event.MessageId,
	}

	if ev.ChatID == "" {
		fmt.Printf("[Feishu] Message recalled: %s\n", ev.MsgID)
	} else {
		fmt.Printf("[Feishu] Message recalled in chat %s: %s\n", ev.ChatID, ev.MsgID)
	}

	if c.onRecalled != nil {
		c.onRecalled(ev)
	}
}

// parseTextContent extracts text from a text message
func (c *Client) parseTextContent(content string) string {
	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return ""
	}
	return parsed.Text
}

// parseImageContent extracts image key from an image message
func (c *Client) parseImageContent(content string) []string {
	var parsed struct {
		ImageKey string `json:"image_key"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil
	}
	if parsed.ImageKey == "" {
		return nil
	}
	return []string{parsed.ImageKey}
}

// parsePostContent extracts text and images from a rich text message
func (c *Client) parsePostContent(content string) (string, []string) {
	var parsed struct {
		Title   string `json:"title"`
		Content [][]struct {
			Tag      string `json:"tag"`
			Text     string `json:"text,omitempty"`
			ImageKey string `json:"image_key,omitempty"`
		} `json:"content"`
	}

	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return "", nil
	}

	var textParts []string
	var imageKeys []string

	if parsed.Title != "" {
		textParts = append(textParts, parsed.Title)
	}

	for _, line := range parsed.Content {
		var lineParts []string
		for _, elem := range line {
			switch elem.Tag {
			case "text":
				if elem.Text != "" {
					lineParts = append(lineParts, elem.Text)
				}
			case "img":
				if elem.ImageKey != "" {
					imageKeys = append(imageKeys, elem.ImageKey)
				}
			}
		}
		if len(lineParts) > 0 {
			textParts = append(textParts, joinStrings(lineParts, ""))
		}
	}

	return joinStrings(textParts, "\n"), imageKeys
}

// DownloadImage downloads an image from Feishu and saves it locally
func (c *Client) DownloadImage(messageID, imageKey string) (string, error) {
	// Ensure download directory exists
	if err := os.MkdirAll(c.downloadDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create download dir: %w", err)
	}

	req := larkim.NewGetMessageResourceReqBuilder().
		MessageId(messageID).
		FileKey(imageKey).
		Type("image").
		Build()

	ctx, cancel := c.requestContext()
	defer cancel()
	resp, err := c.larkCli.Im.MessageResource.Get(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to get image: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("get image error: %s", resp.Msg)
	}

	// Save to file
	filePath := filepath.Join(c.downloadDir, imageKey+".png")
	file, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	_, err = io.Copy(file, resp.File)
	if err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	fmt.Printf("[Feishu] Downloaded image to %s\n", filePath)
	return filePath, nil
}

// SendText sends a text message to a chat
func (c *Client) SendText(chatID, text string) error {
	content := map[string]string{"text": text}
	contentJSON, _ := json.Marshal(content)

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType(larkim.MsgTypeText).
			Content(string(contentJSON)).
			Build()).
		Build()

	ctx, cancel := c.requestContext()
	defer cancel()
	resp, err := c.larkCli.Im.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("send message failed: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("send message error: %s", resp.Msg)
	}

	fmt.Printf("[Feishu] Message sent to %s\n", chatID)
	return nil
}

// ReplyText replies to a specific message with a text message (quote-style reply)
func (c *Client) ReplyText(messageID, text string, replyInThread bool) error {
	content := map[string]string{"text": text}
	contentJSON, _ := json.Marshal(content)

	req := larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType(larkim.MsgTypeText).
			Content(string(contentJSON)).
			ReplyInThread(replyInThread).
			Build()).
		Build()

	ctx, cancel := c.requestContext()
	defer cancel()
	resp, err := c.larkCli.Im.Message.Reply(ctx, req)
	if err != nil {
		return fmt.Errorf("reply message failed: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("reply message error: %s", resp.Msg)
	}

	fmt.Printf("[Feishu] Replied to message %s\n", messageID)
	return nil
}

// SendRichText sends a rich text (post) message to a chat
func (c *Client) SendRichText(chatID, title string, content [][]map[string]interface{}) error {
	post := map[string]interface{}{
		"zh_cn": map[string]interface{}{
			"title":   title,
			"content": content,
		},
	}
	contentJSON, _ := json.Marshal(post)

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType(larkim.MsgTypePost).
			Content(string(contentJSON)).
			Build()).
		Build()

	ctx, cancel := c.requestContext()
	defer cancel()
	resp, err := c.larkCli.Im.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("send rich text failed: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("send rich text error: %s", resp.Msg)
	}

	fmt.Printf("[Feishu] Rich text sent to %s\n", chatID)
	return nil
}

// ReplyRichText replies to a specific message with a rich text (post) message
func (c *Client) ReplyRichText(messageID, title string, content [][]map[string]interface{}, replyInThread bool) error {
	post := map[string]interface{}{
		"zh_cn": map[string]interface{}{
			"title":   title,
			"content": content,
		},
	}
	contentJSON, _ := json.Marshal(post)

	req := larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType(larkim.MsgTypePost).
			Content(string(contentJSON)).
			ReplyInThread(replyInThread).
			Build()).
		Build()

	ctx, cancel := c.requestContext()
	defer cancel()
	resp, err := c.larkCli.Im.Message.Reply(ctx, req)
	if err != nil {
		return fmt.Errorf("reply rich text failed: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("reply rich text error: %s", resp.Msg)
	}

	fmt.Printf("[Feishu] Rich text replied to %s\n", messageID)
	return nil
}

// AddReaction adds an emoji reaction to a message
func (c *Client) AddReaction(messageID, emojiType string) (string, error) {
	req := larkim.NewCreateMessageReactionReqBuilder().
		MessageId(messageID).
		Body(larkim.NewCreateMessageReactionReqBodyBuilder().
			ReactionType(larkim.NewEmojiBuilder().EmojiType(emojiType).Build()).
			Build()).
		Build()

	ctx, cancel := c.requestContext()
	defer cancel()
	resp, err := c.larkCli.Im.MessageReaction.Create(ctx, req)
	if err != nil {
		return "", fmt.Errorf("add reaction failed: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("add reaction error: %s", resp.Msg)
	}

	fmt.Printf("[Feishu] Reaction %s added to message %s\n", emojiType, messageID)
	if resp.Data != nil && resp.Data.ReactionId != nil {
		return *resp.Data.ReactionId, nil
	}
	return "", nil
}

// RemoveReaction removes an emoji reaction from a message
func (c *Client) RemoveReaction(messageID, reactionID string) error {
	req := larkim.NewDeleteMessageReactionReqBuilder().
		MessageId(messageID).
		ReactionId(reactionID).
		Build()

	ctx, cancel := c.requestContext()
	defer cancel()
	resp, err := c.larkCli.Im.MessageReaction.Delete(ctx, req)
	if err != nil {
		return fmt.Errorf("remove reaction failed: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("remove reaction error: %s", resp.Msg)
	}

	fmt.Printf("[Feishu] Reaction removed from message %s\n", messageID)
	return nil
}

// GetChatHistory retrieves recent messages from a chat
// pageSize: number of messages to retrieve (max 50)
func (c *Client) GetChatHistory(chatID string, pageSize int) ([]*HistoryMessage, error) {
	if pageSize > 50 {
		pageSize = 50
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	req := larkim.NewListMessageReqBuilder().
		ContainerIdType("chat").
		ContainerId(chatID).
		PageSize(pageSize).
		Build()

	ctx, cancel := c.requestContext()
	defer cancel()
	resp, err := c.larkCli.Im.Message.List(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("get chat history failed: %w", err)
	}
	if !resp.Success() {
		return nil, fmt.Errorf("get chat history error: %s", resp.Msg)
	}

	var messages []*HistoryMessage
	for _, item := range resp.Data.Items {
		msg := &HistoryMessage{
			MsgID:      *item.MessageId,
			MsgType:    *item.MsgType,
			CreateTime: *item.CreateTime,
		}

		// Parse content based on message type
		if item.Body != nil && item.Body.Content != nil {
			msg.Content = *item.Body.Content
		}

		// Parse sender
		if item.Sender != nil {
			msg.Sender = &Sender{}
			if item.Sender.Id != nil {
				msg.Sender.SenderID = *item.Sender.Id
			}
			if item.Sender.SenderType != nil {
				msg.Sender.SenderType = *item.Sender.SenderType
			}
			if item.Sender.TenantKey != nil {
				msg.Sender.TenantKey = *item.Sender.TenantKey
			}
		}

		messages = append(messages, msg)
	}

	fmt.Printf("[Feishu] Retrieved %d messages from chat %s\n", len(messages), chatID)
	return messages, nil
}

// GetChatMembers retrieves members of a chat (group)
func (c *Client) GetChatMembers(chatID string) ([]*ChatMember, error) {
	req := larkim.NewGetChatMembersReqBuilder().
		ChatId(chatID).
		Build()

	ctx, cancel := c.requestContext()
	defer cancel()
	resp, err := c.larkCli.Im.ChatMembers.Get(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("get chat members failed: %w", err)
	}
	if !resp.Success() {
		return nil, fmt.Errorf("get chat members error: %s", resp.Msg)
	}

	var members []*ChatMember
	for _, item := range resp.Data.Items {
		member := &ChatMember{}
		if item.MemberId != nil {
			member.MemberID = *item.MemberId
		}
		if item.MemberIdType != nil {
			member.MemberType = *item.MemberIdType
		}
		if item.Name != nil {
			member.Name = *item.Name
		}
		members = append(members, member)
	}

	fmt.Printf("[Feishu] Retrieved %d members from chat %s\n", len(members), chatID)
	return members, nil
}

// GetChatInfo retrieves information about a chat
func (c *Client) GetChatInfo(chatID string) (*ChatInfo, error) {
	req := larkim.NewGetChatReqBuilder().
		ChatId(chatID).
		Build()

	ctx, cancel := c.requestContext()
	defer cancel()
	resp, err := c.larkCli.Im.Chat.Get(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("get chat info failed: %w", err)
	}
	if !resp.Success() {
		return nil, fmt.Errorf("get chat info error: %s", resp.Msg)
	}

	info := &ChatInfo{
		ChatID: chatID,
	}
	if resp.Data.Name != nil {
		info.Name = *resp.Data.Name
	}
	if resp.Data.Description != nil {
		info.Description = *resp.Data.Description
	}
	if resp.Data.ChatMode != nil {
		info.ChatType = *resp.Data.ChatMode
	}
	if resp.Data.OwnerId != nil {
		info.OwnerID = *resp.Data.OwnerId
	}
	if resp.Data.UserCount != nil {
		var count int
		fmt.Sscanf(*resp.Data.UserCount, "%d", &count)
		info.MemberCount = count
	}

	fmt.Printf("[Feishu] Got chat info for %s: %s (%d members)\n", chatID, info.Name, info.MemberCount)
	return info, nil
}

// FormatHistoryAsContext formats chat history as context string for AI
func FormatHistoryAsContext(messages []*HistoryMessage, maxMessages int) string {
	if len(messages) == 0 {
		return ""
	}

	if maxMessages > 0 && len(messages) > maxMessages {
		messages = messages[:maxMessages]
	}

	var parts []string
	// Messages are usually newest first, so reverse for chronological order
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		senderType := "User"
		if msg.Sender != nil && msg.Sender.SenderType == "bot" {
			senderType = "Bot"
		}

		// Extract text from content JSON
		content := msg.Content
		if msg.MsgType == "text" {
			var parsed struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(content), &parsed); err == nil {
				content = parsed.Text
			}
		}

		parts = append(parts, fmt.Sprintf("[%s]: %s", senderType, content))
	}

	return joinStrings(parts, "\n")
}

// Helper functions

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += sep + parts[i]
	}
	return result
}
