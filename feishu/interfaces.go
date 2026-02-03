package feishu

// FeishuClient defines the interface for Feishu operations
type FeishuClient interface {
	OnMessage(handler MessageHandler)
	Start() error
	Stop()
	SendText(chatID, text string) error
	SendRichText(chatID, title string, content [][]map[string]interface{}) error
	AddReaction(messageID, emojiType string) error
	RemoveReaction(messageID, reactionID string) error
	DownloadImage(messageID, imageKey string) (string, error)
	SetDownloadDir(dir string)
	GetChatHistory(chatID string, pageSize int) ([]*HistoryMessage, error)
	GetChatMembers(chatID string) ([]*ChatMember, error)
	GetChatInfo(chatID string) (*ChatInfo, error)
}

// Ensure Client implements FeishuClient
var _ FeishuClient = (*Client)(nil)
