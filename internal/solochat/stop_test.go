package solochat

import (
	"context"
	"sync/atomic"
	"testing"
)

// TestStopStreamCancelsHandle 直接验证 activeStreams 上的 cancel 联动 ——
// SendMessageStream 不易在单测里跑(需要 OSS / ADK / gin context),所以这
// 里只验证 Swap / Cancel / CompareAndDelete 这套并发原语我们用对了。
func TestStopStreamCancelsHandle(t *testing.T) {
	s := &Service{}

	// 起一个"假 stream":手动 register handle
	ctx1, cancel1 := context.WithCancel(context.Background())
	h1 := &streamHandle{cancel: cancel1}
	if old, loaded := s.activeStreams.Swap(streamKey("u1", "c1"), h1); loaded {
		t.Fatalf("unexpected pre-existing handle: %v", old)
	}

	// StopStream 应触发 cancel1
	s.StopStream("u1", "c1")
	if err := ctx1.Err(); err != context.Canceled {
		t.Fatalf("ctx1 should be cancelled, got %v", err)
	}

	// h1 自己 defer CompareAndDelete
	s.activeStreams.CompareAndDelete(streamKey("u1", "c1"), h1)
	if _, ok := s.activeStreams.Load(streamKey("u1", "c1")); ok {
		t.Fatal("entry should be deleted after CompareAndDelete")
	}

	// Swap 时旧 stream 被新 stream 顶替,旧 cancel 触发
	cancelled := atomic.Bool{}
	ctx2, cancel2 := context.WithCancel(context.Background())
	h2 := &streamHandle{cancel: func() {
		cancel2()
		cancelled.Store(true)
	}}
	s.activeStreams.Swap(streamKey("u1", "c1"), h2)

	ctx3, cancel3 := context.WithCancel(context.Background())
	h3 := &streamHandle{cancel: cancel3}
	if old, loaded := s.activeStreams.Swap(streamKey("u1", "c1"), h3); !loaded {
		t.Fatal("expected loaded=true on second Swap")
	} else {
		old.(*streamHandle).cancel() // 模拟 SendMessageStream 入口的 implicit cancel
	}

	if err := ctx2.Err(); err != context.Canceled {
		t.Fatal("h2 ctx should be cancelled by implicit-cancel-on-new-send")
	}
	if !cancelled.Load() {
		t.Fatal("h2 cancel func should have been invoked")
	}

	// h2 defer CompareAndDelete 此时 key 已被 h3 占;不能误删 h3
	s.activeStreams.CompareAndDelete(streamKey("u1", "c1"), h2)
	if _, ok := s.activeStreams.Load(streamKey("u1", "c1")); !ok {
		t.Fatal("h3 entry must NOT be deleted by h2's stale CompareAndDelete")
	}

	// h3 不会被取消
	if ctx3.Err() != nil {
		t.Fatalf("ctx3 should not be cancelled: %v", ctx3.Err())
	}
	cancel3()
}
