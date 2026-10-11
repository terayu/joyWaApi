package e2e

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/terayu/joyWaApi/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestJoyWatcherRestart JoyWSrv2 が止まったときの joyWaApi の動き
//
//   - 止まっている間、読み書きはエラーになるが joyWaApi は落ちない
//   - 読み書きの失敗が -failtimeout 秒続くと、joyWaApi はログを出して終了する
//     （JoyWSrv2 を再起動すると、止まっていた時間によっては DLL の接続がそのプロセスでは回復しないため。
//     サービスなどから再起動してもらう前提）
//   - JoyWSrv2 の再起動後に起動した joyWaApi は読み書きできる
//
// JOYWA_E2E_RESTART=1 のときだけ実行する（JoyWatcher が一時的に止まるため）
func TestJoyWatcherRestart(t *testing.T) {
	if !runRestart {
		t.Skip("set JOYWA_E2E_RESTART=1 to stop and restart JoyWSrv2")
	}
	pid := pidOf("JoyWSrv2")
	if pid == 0 {
		t.Fatal("JoyWSrv2 is not running")
	}
	// 止める前に、動いているプロセスのパスを控える（JOYWA_E2E_JOYWSRV2 未設定のとき再起動に使う）
	if joywsrv2Exe == "" {
		p, err := powershell(fmt.Sprintf(`(Get-Process -Id %d).Path`, pid))
		if err != nil || p == "" {
			t.Fatalf("cannot get JoyWSrv2 path (set JOYWA_E2E_JOYWSRV2): %v", err)
		}
		joywsrv2Exe = p
	}
	exe := filepath.Join(workDir, "joyWaApi.exe")
	const failTimeout = 8
	s, err := startServer(exe, filepath.Join(workDir, "joyWaApi-restart.log"),
		"-failtimeout", strconv.Itoa(failTimeout), "-watchinterval", "1")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	tag := fx.short1[5]
	if err := writeTo(s.addr, tag, "11"); err != nil {
		t.Fatal(err)
	}

	// JoyWSrv2 を止める
	if out, err := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/F").CombinedOutput(); err != nil {
		t.Fatalf("taskkill: %v %s", err, out)
	}
	stopped := time.Now()
	defer func() {
		if pidOf("JoyWSrv2") == 0 {
			startJoyWSrv2(t)
		}
		// テスト全体のサーバーを起動し直す（失敗したら古い srv のままにして、後のテストはエラーにする）
		srv.Stop()
		eventually(t, 60*time.Second, func() error {
			newSrv, err := startServer(exe, filepath.Join(workDir, "joyWaApi-2.log"))
			if err == nil {
				srv = newSrv
			}
			return err
		})
	}()
	eventually(t, 10*time.Second, func() error {
		if pidOf("JoyWSrv2") != 0 {
			return errString("JoyWSrv2 still running")
		}
		return nil
	})

	// 止まっている間はエラーになるが、joyWaApi は落ちない
	if err := writeTo(s.addr, tag, "22"); err == nil {
		t.Error("write while JoyWSrv2 is down: err = nil")
	}
	if !s.alive() || !srv.alive() {
		t.Fatal("joyWaApi exited right after JoyWSrv2 stopped")
	}
	// テスト全体のサーバーは先に止める。回復しない接続を持ったプロセスが残っていると、
	// JoyWNet2（DLL が起動する、PC で共有の中継プロセス）が終了せず、新しいプロセスも読めないことがあるため
	srv.Stop()

	// 失敗が failTimeout 秒続くと終了する
	select {
	case <-s.done:
		d := time.Since(stopped)
		t.Logf("joyWaApi exited %v after JoyWSrv2 stopped", d.Round(time.Second))
		if d < failTimeout*time.Second {
			t.Errorf("exited too early: %v < %ds", d, failTimeout)
		}
	case <-time.After((failTimeout + 15) * time.Second):
		t.Fatal("joyWaApi did not exit while JoyWSrv2 is down")
	}
	if n := s.logCount("exit to be restarted"); n == 0 {
		t.Errorf("no exit message in %s", s.logPath)
	}

	// JoyWSrv2 を起動し直すと、新しく起動した joyWaApi は読み書きできる
	startJoyWSrv2(t)
	var s2 *serverProc
	eventually(t, 60*time.Second, func() error {
		var err error
		if s2 == nil {
			if s2, err = startServer(exe, filepath.Join(workDir, "joyWaApi-restart2.log")); err != nil {
				s2 = nil
				return err
			}
		}
		return writeTo(s2.addr, tag, "33")
	})
	defer s2.Stop()
	waitPLC(t, tag, shortRaw(33))
}

// writeTo addr の joyWaApi に書き込む
func writeTo(addr string, tag Tag, val string) error {
	cn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer cn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := rpc.NewJoyWaApiClient(cn)
	if _, err := c.RConnect(ctx, &rpc.RequestType{}); err != nil {
		return err
	}
	defer c.RDisConnect(ctx, &rpc.RequestType{})
	_, err = c.RWrite(ctx, &rpc.TagValsMessage{Tags: names(tag), Vals: []string{val}})
	return err
}

func startJoyWSrv2(t *testing.T) {
	t.Helper()
	// テストのプロセスと切り離して起動する
	if _, err := powershell(fmt.Sprintf(`Start-Process -FilePath "%s" -WorkingDirectory "%s"`, joywsrv2Exe, filepath.Dir(joywsrv2Exe))); err != nil {
		t.Fatalf("start JoyWSrv2: %v", err)
	}
	eventually(t, 30*time.Second, func() error {
		if pidOf("JoyWSrv2") == 0 {
			return errString("JoyWSrv2 not started")
		}
		return nil
	})
}

type errString string

func (e errString) Error() string { return string(e) }
