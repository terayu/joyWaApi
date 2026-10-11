//go:build server
// +build server

package core

import (
	"sync"
	"time"
)

// DLL の読み書きが失敗し続けている時間を記録する
//
// JoyWSrv2 を再起動すると、止まっていた時間によっては、このプロセスの DLL の接続が回復しないことがある
// （接続し直しても、DLL を読み込み直しても直らず、プロセスを再起動すると直る）。
// サーバーはこの時間を見て、回復しないときは終了する（cmd/server.go）。
var health struct {
	mu           sync.Mutex
	failingSince time.Time // 失敗が続いている最初の時刻（成功していれば zero）
}

func recordResult(err error) {
	health.mu.Lock()
	defer health.mu.Unlock()
	if err == nil {
		health.failingSince = time.Time{}
	} else if health.failingSince.IsZero() {
		health.failingSince = time.Now()
	}
}

// FailingFor DLL の読み書きが失敗し続けている時間（最後に成功していれば 0）
func (h *Handle) FailingFor() time.Duration {
	health.mu.Lock()
	defer health.mu.Unlock()
	if health.failingSince.IsZero() {
		return 0
	}
	return time.Since(health.failingSince)
}
