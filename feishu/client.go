package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

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
	Content   string   // Text content (extracted from all message types)
	ImageKeys []string // Image keys for downloading
}

// MessageHandler is the callback for received messages
type MessageHandler func(msg *Message)

// Client is the Feishu API client
type Client struct {
	appID       string
	appSecret   string
	larkCli     *lark.Client
	wsCli       *larkws.Client
	onMessage   MessageHandler
	downloadDir string
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewClient creates a new Feishu client
func NewClient(appID, appSecret string) *Client {
	return &Client{
		appID:       appID,
		appSecret:   appSecret,
		downloadDir: "/tmp/feishu-images",
	}
}

// SetDownloadDir sets the directory for downloading images
func (c *Client) SetDownloadDir(dir string) {
	c.downloadDir = dir
}

// OnMessage sets the message handler
func (c *Client) OnMessage(handler MessageHandler) {
	c.onMessage = handler
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

	switch msg.MsgType {
	case "text":
		msg.Content = c.parseTextContent(*rawMsg.Content)
	case "image":
		msg.ImageKeys = c.parseImageContent(*rawMsg.Content)
		msg.Content = "[圖片]"
	case "post":
		content, imageKeys := c.parsePostContent(*rawMsg.Content)
		msg.Content = content
		msg.ImageKeys = imageKeys
	default:
		// Unsupported message type
		fmt.Printf("[Feishu] Unsupported message type: %s\n", msg.MsgType)
		return
	}

	fmt.Printf("[Feishu] Received %s from chat %s: %s\n", msg.MsgType, msg.ChatID, truncate(msg.Content, 50))

	if c.onMessage != nil {
		c.onMessage(msg)
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

	resp, err := c.larkCli.Im.MessageResource.Get(context.Background(), req)
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

	resp, err := c.larkCli.Im.Message.Create(context.Background(), req)
	if err != nil {
		return fmt.Errorf("send message failed: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("send message error: %s", resp.Msg)
	}

	fmt.Printf("[Feishu] Message sent to %s\n", chatID)
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

	resp, err := c.larkCli.Im.Message.Create(context.Background(), req)
	if err != nil {
		return fmt.Errorf("send rich text failed: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("send rich text error: %s", resp.Msg)
	}

	fmt.Printf("[Feishu] Rich text sent to %s\n", chatID)
	return nil
}

// AddReaction adds an emoji reaction to a message
func (c *Client) AddReaction(messageID, emojiType string) error {
	req := larkim.NewCreateMessageReactionReqBuilder().
		MessageId(messageID).
		Body(larkim.NewCreateMessageReactionReqBodyBuilder().
			ReactionType(larkim.NewEmojiBuilder().EmojiType(emojiType).Build()).
			Build()).
		Build()

	resp, err := c.larkCli.Im.MessageReaction.Create(context.Background(), req)
	if err != nil {
		return fmt.Errorf("add reaction failed: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("add reaction error: %s", resp.Msg)
	}

	fmt.Printf("[Feishu] Reaction %s added to message %s\n", emojiType, messageID)
	return nil
}

// RemoveReaction removes an emoji reaction from a message
func (c *Client) RemoveReaction(messageID, reactionID string) error {
	req := larkim.NewDeleteMessageReactionReqBuilder().
		MessageId(messageID).
		ReactionId(reactionID).
		Build()

	resp, err := c.larkCli.Im.MessageReaction.Delete(context.Background(), req)
	if err != nil {
		return fmt.Errorf("remove reaction failed: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("remove reaction error: %s", resp.Msg)
	}

	fmt.Printf("[Feishu] Reaction removed from message %s\n", messageID)
	return nil
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
