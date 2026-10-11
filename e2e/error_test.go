package e2e

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/terayu/joyWaApi/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const unknownTag = "LOCAL$NOPE.NOT_EXIST$VALUE"

// TestInvalidArgument タグ数と値の数が合わないときは InvalidArgument になり、サーバーは落ちない
func TestInvalidArgument(t *testing.T) {
	c := client(t)
	ctx := context.Background()
	two := names(fx.short01[0], fx.short01[1])
	calls := map[string]func() error{
		"RWrite": func() error {
			_, err := c.RWrite(ctx, &rpc.TagValsMessage{Tags: two, Vals: []string{"1"}})
			return err
		},
		"RWriteF": func() error {
			_, err := c.RWriteF(ctx, &rpc.TagValsMessage{Tags: two[:1], Vals: []string{"1", "2"}})
			return err
		},
		"RWriteS": func() error {
			_, err := c.RWriteS(ctx, &rpc.TagValsMessage{Tags: two, Vals: nil})
			return err
		},
		"RWriteB": func() error {
			_, err := c.RWriteB(ctx, &rpc.TagBitMessage{Tags: names(fx.bits[0]), Vals: []bool{true, false}})
			return err
		},
		"RWriteB-pulse": func() error {
			_, err := c.RWriteB(ctx, &rpc.TagBitMessage{Tags: names(fx.bits[0], fx.bits[1]), Vals: []bool{true, true}, Pulse: []int32{1}})
			return err
		},
	}
	for name, call := range calls {
		if err := call(); status.Code(err) != codes.InvalidArgument {
			t.Errorf("%s: err = %v, want InvalidArgument", name, err)
		}
	}
	assertServerAlive(t)
}

// TestUnknownTag 存在しないタグはエラーになる（ID を取れないので DLL の JWRead は呼ばれない）
func TestUnknownTag(t *testing.T) {
	if _, err := read(unknownTag); err == nil || !strings.Contains(err.Error(), "not found TAG") {
		t.Errorf("read err = %v, want not found TAG", err)
	}
	if err := write([]string{unknownTag}, []string{"1"}); err == nil || !strings.Contains(err.Error(), "not found TAG") {
		t.Errorf("write err = %v, want not found TAG", err)
	}
	// 正しいタグと一緒に読んでもエラーになる
	if _, err := read(fx.short01[0].Full(), unknownTag); err == nil {
		t.Error("read with unknown tag: err = nil")
	}
	assertServerAlive(t)
}

// TestBadValue 数値に変換できない値・文字列タグへの数値読み込み
func TestBadValue(t *testing.T) {
	if err := write(names(fx.short01[0]), []string{"abc"}); err == nil {
		t.Error("write abc to numeric tag: err = nil")
	}
	c := client(t)
	if _, err := c.RReadF(context.Background(), &rpc.TagsMessage{Tags: names(fx.str)}); err == nil {
		t.Error("RReadF on string tag: err = nil")
	}
	if _, err := c.RReadB(context.Background(), &rpc.TagBitMessage{Tags: names(fx.str)}); err == nil {
		t.Error("RReadB on string tag: err = nil")
	}
	assertServerAlive(t)
}

// TestDisconnectWithoutConnect Connect していないクライアントが Disconnect しても、他の接続は切れない
func TestDisconnectWithoutConnect(t *testing.T) {
	c := client(t) // この接続を使い続ける
	for i := 0; i < 5; i++ {
		if _, err := c.RDisConnect(context.Background(), &rpc.RequestType{}); err != nil {
			t.Fatal(err)
		}
	}
	// クライアントの分（1）とサーバーの分を超えて切断しても、サーバーの接続は残る
	if _, err := read(fx.short01[0].Full()); err != nil {
		t.Fatalf("read after extra disconnects: %v", err)
	}
	if _, err := c.RRead(context.Background(), &rpc.TagsMessage{Tags: names(fx.short01[0])}); err != nil {
		t.Fatalf("RRead after extra disconnects: %v", err)
	}
	// このクライアントの RConnect の分だけ切断され、サーバーが保持する 1 本は残る
	if n, err := srv.lastConnCount(); err != nil || n != 1 {
		t.Errorf("connCount = %d, %v; want 1", n, err)
	}
}

// TestDisconnectForce RDisConnectForce の後も、新しいクライアントは接続して読み書きできる
// （サーバーが保持する接続も切れるので、ほかのテストに影響しないよう専用のサーバーを起動する）
func TestDisconnectForce(t *testing.T) {
	s, err := startServer(filepath.Join(workDir, "joyWaApi.exe"), filepath.Join(workDir, "joyWaApi-force.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	cn, err := grpc.Dial(s.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer cn.Close()
	c := rpc.NewJoyWaApiClient(cn)
	ctx := context.Background()
	tag := names(fx.short01[0])

	if _, err := c.RDisConnectForce(ctx, &rpc.RequestType{}); err != nil {
		t.Fatal(err)
	}
	// 接続していないので読めない
	if _, err := c.RRead(ctx, &rpc.TagsMessage{Tags: tag}); err == nil {
		t.Error("RRead after force without connect: err = nil")
	}
	// 接続し直せば読み書きできる
	if _, err := c.RConnect(ctx, &rpc.RequestType{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RRead(ctx, &rpc.TagsMessage{Tags: tag}); err != nil {
		t.Fatalf("RRead after reconnect: %v", err)
	}
	if _, err := c.RDisConnect(ctx, &rpc.RequestType{}); err != nil {
		t.Fatal(err)
	}
	if n, err := s.lastConnCount(); err != nil || n != 0 {
		t.Errorf("connCount = %d, %v; want 0", n, err)
	}
}

func assertServerAlive(t *testing.T) {
	t.Helper()
	if !srv.alive() {
		t.Fatal("joyWaApi server process exited")
	}
	if ok, err := srv.ping(); err != nil || !ok {
		t.Fatalf("RPing = %v, %v", ok, err)
	}
	if _, err := read(fx.short01[0].Full()); err != nil {
		t.Fatalf("read after error: %v", err)
	}
}
