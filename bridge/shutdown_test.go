package bridge

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/feishu-codex-bridge/feishu"
)

func TestChatWorker_ExitsWhenQueueClosed(t *testing.T) {
	b := &Bridge{
		ctx: context.Background(),
		wg:  sync.WaitGroup{},
	}

	q := make(chan *feishu.Message)

	b.wg.Add(1)
	go b.chatWorker("chat", q)

	close(q)

	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("chat worker did not exit after queue closed")
	}
}

func TestChatWorker_ExitsWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b := &Bridge{
		ctx: ctx,
		wg:  sync.WaitGroup{},
	}

	q := make(chan *feishu.Message)

	b.wg.Add(1)
	go b.chatWorker("chat", q)

	cancel()

	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("chat worker did not exit after context canceled")
	}
}
