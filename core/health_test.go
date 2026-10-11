//go:build server
// +build server

package core

import (
	"errors"
	"testing"
	"time"
)

func TestFailingFor(t *testing.T) {
	h := New()
	recordResult(nil)
	if d := h.FailingFor(); d != 0 {
		t.Fatalf("after success: %v, want 0", d)
	}

	recordResult(errors.New("fail"))
	time.Sleep(20 * time.Millisecond)
	recordResult(errors.New("fail")) // 最初の失敗の時刻から数える
	if d := h.FailingFor(); d < 20*time.Millisecond {
		t.Fatalf("after failures: %v, want >= 20ms", d)
	}

	recordResult(nil)
	if d := h.FailingFor(); d != 0 {
		t.Fatalf("after recovery: %v, want 0", d)
	}
}
