package e2e

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/terayu/joyWaApi/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestNoResourceLeak 接続・読み書き・切断を繰り返しても、リソースが増え続けないこと
//
// 同じ負荷を 2 回かけて、1 回目の後と 2 回目の後を比べる（Go のランタイムやキャッシュが最初に増える分は除く）。
//   - joyWaApi サーバー: ハンドル数・スレッド数・プライベートメモリ
//   - JoyWSrv2: ハンドル数（DLL の接続が JoyWatcher 側に残っていないか）
//   - サーバーへの TCP 接続（gRPC の接続が閉じられているか）
//   - DLL の接続数（connCount がサーバー保持の 1 本に戻るか）
func TestNoResourceLeak(t *testing.T) {
	jwPid := pidOf("JoyWSrv2")
	if jwPid == 0 {
		t.Fatal("JoyWSrv2 is not running")
	}
	srvPid := srv.cmd.Process.Pid

	load := func() {
		var wg sync.WaitGroup
		var errs errList
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				tag := fx.short1[g]
				for i := 0; i < 50; i++ {
					if err := write(names(tag), []string{strconv.Itoa(i)}); err != nil {
						errs.add("write: %v", err)
					}
					if _, err := read(tag.Full(), fx.bits[0].Full(), fx.str.Full()); err != nil {
						errs.add("read: %v", err)
					}
				}
			}(g)
		}
		for p := 0; p < 10; p++ { // 1 回ごとに接続する書き込みも（クライアントのプロセスを毎回起動する使い方を想定）
			if err := write(names(fx.short1[4]), []string{strconv.Itoa(p)}); err != nil {
				errs.add("write: %v", err)
			}
		}
		wg.Wait()
		errs.report(t)
		time.Sleep(3 * time.Second) // 閉じた接続が片付くのを待つ
	}
	measure := func(label string) (procStat, procStat) {
		s, err := processStat(srvPid)
		if err != nil {
			t.Fatal(err)
		}
		j, err := processStat(jwPid)
		if err != nil {
			t.Fatal(err)
		}
		conns, err := tcpConns(srv.addr)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: joyWaApi %v / JoyWSrv2 %v / tcp %v", label, s, j, conns)
		if conns["ESTABLISHED"] != 0 {
			t.Errorf("%s: %d established TCP connections remain on %s", label, conns["ESTABLISHED"], srv.addr)
		}
		return s, j
	}

	// TCP 接続を数えられることを確認する（接続中は 1 以上になる）
	func() {
		cn, err := grpc.Dial(srv.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatal(err)
		}
		defer cn.Close()
		if _, err := rpc.NewJoyWaApiClient(cn).RPing(context.Background(), &rpc.RequestType{}); err != nil {
			t.Fatal(err)
		}
		conns, err := tcpConns(srv.addr)
		if err != nil {
			t.Fatal(err)
		}
		if conns["ESTABLISHED"] < 1 {
			t.Fatalf("tcpConns cannot see an open connection: %v", conns)
		}
	}()
	time.Sleep(time.Second)

	s0, j0 := measure("before")
	load()
	s1, j1 := measure("after load #1")
	assertConnCount(t, 1)
	load()
	s2, j2 := measure("after load #2")
	assertConnCount(t, 1)

	check := func(name string, d, limit int64) {
		if d > limit {
			t.Errorf("%s grew by %d in load #2 (limit %d)", name, d, limit)
		}
	}
	check("joyWaApi handles", int64(s2.Handles-s1.Handles), 30)
	check("joyWaApi threads", int64(s2.Threads-s1.Threads), 5)
	check("joyWaApi private memory", s2.Private-s1.Private, 20e6)
	check("JoyWSrv2 handles", int64(j2.Handles-j1.Handles), 30)
	t.Logf("load #1 diff: joyWaApi handles %+d threads %+d / JoyWSrv2 handles %+d",
		s1.Handles-s0.Handles, s1.Threads-s0.Threads, j1.Handles-j0.Handles)
	t.Logf("load #2 diff: joyWaApi handles %+d threads %+d private %+.1fMB / JoyWSrv2 handles %+d",
		s2.Handles-s1.Handles, s2.Threads-s1.Threads, float64(s2.Private-s1.Private)/1e6, j2.Handles-j1.Handles)
	if n := srv.logCount("panic"); n != 0 {
		t.Errorf("server log has %d panic", n)
	}
}
