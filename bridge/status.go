package bridge

import "fmt"

func (b *Bridge) formatStatus(chatID string) string {
	state := b.getChatState(chatID)
	state.mu.Lock()
	processing := state.Processing
	lastItem := state.LastItem
	state.mu.Unlock()

	pendingCount := 0
	b.queuesMu.Lock()
	q := b.chatQueues[chatID]
	b.queuesMu.Unlock()
	if q != nil {
		q.mu.Lock()
		pendingCount = len(q.pending)
		q.mu.Unlock()
	}

	if !processing {
		return fmt.Sprintf("状态：空闲\n待处理：%d", pendingCount)
	}

	step := lastItem
	if step == "" {
		step = "生成回复"
	}
	return fmt.Sprintf("状态：处理中\n当前步骤：%s\n待处理：%d", step, pendingCount)
}
