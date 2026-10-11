package e2e

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/terayu/joyWaApi/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// serverProc テスト用に起動した joyWaApi サーバー
type serverProc struct {
	cmd     *exec.Cmd
	addr    string
	logPath string
	logFile *os.File
	done    chan struct{} // プロセスが終了したら close される
}

func freeAddr() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer ln.Close()
	return ln.Addr().String(), nil
}

// startServer joyWaApi サーバーを起動する。args は server サブコマンドに追加する引数
func startServer(exe, logPath string, args ...string) (*serverProc, error) {
	addr, err := freeAddr()
	if err != nil {
		return nil, err
	}
	f, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	s := &serverProc{addr: addr, logPath: logPath, logFile: f, done: make(chan struct{})}
	s.cmd = exec.Command(exe, append([]string{"-loglevel", "trace", "server", "-rpcaddr", addr}, args...)...)
	s.cmd.Stdout = f
	s.cmd.Stderr = f
	if err := s.cmd.Start(); err != nil {
		f.Close()
		return nil, err
	}
	go func() {
		s.cmd.Wait()
		close(s.done)
	}()

	// RPing が通るまで待つ
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if ok, _ := s.ping(); ok {
			return s, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	s.Stop()
	return nil, fmt.Errorf("joyWaApi server did not start (log: %s)", logPath)
}

func (s *serverProc) Stop() {
	s.cmd.Process.Kill()
	<-s.done
	s.logFile.Close()
}

// alive サーバーのプロセスが動いているか
func (s *serverProc) alive() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

func (s *serverProc) ping() (bool, error) {
	cn, err := grpc.Dial(s.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return false, err
	}
	defer cn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r, err := rpc.NewJoyWaApiClient(cn).RPing(ctx, &rpc.RequestType{})
	if err != nil {
		return false, err
	}
	return r.Val, nil
}

var connCountRe = regexp.MustCompile(`connCount=(\d+)`)

// lastConnCount サーバーのログに出た最後の接続数
func (s *serverProc) lastConnCount() (int, error) {
	b, err := os.ReadFile(s.logPath)
	if err != nil {
		return 0, err
	}
	m := connCountRe.FindAllSubmatch(b, -1)
	if len(m) == 0 {
		return 0, fmt.Errorf("no connCount in log")
	}
	return strconv.Atoi(string(m[len(m)-1][1]))
}

// logContains ログに含まれる回数（panic の確認などに使う）
func (s *serverProc) logCount(substr string) int {
	b, _ := os.ReadFile(s.logPath)
	return strings.Count(string(b), substr)
}

// session 1 クライアント分の接続（Open で RConnect、Close で RDisConnect）
type session struct {
	cn *grpc.ClientConn
	c  rpc.JoyWaApiClient
}

func repo() *session { return &session{} }

func (s *session) Open() error {
	cn, err := grpc.Dial(srv.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	s.cn = cn
	s.c = rpc.NewJoyWaApiClient(cn)
	if _, err := s.c.RConnect(context.Background(), &rpc.RequestType{}); err != nil {
		cn.Close()
		return err
	}
	return nil
}

func (s *session) Close() error {
	if _, err := s.c.RDisConnect(context.Background(), &rpc.RequestType{}); err != nil {
		s.cn.Close()
		return err
	}
	return s.cn.Close()
}

func (s *session) Read(tags []string) ([]string, error) {
	r, err := s.c.RRead(context.Background(), &rpc.TagsMessage{Tags: tags})
	if err != nil {
		return nil, err
	}
	return r.Vals, nil
}

func (s *session) Write(tags []string, vals []string) error {
	_, err := s.c.RWrite(context.Background(), &rpc.TagValsMessage{Tags: tags, Vals: vals})
	return err
}

// client gRPC クライアント（RConnect 済み）。終了時に RDisConnect して閉じる
func client(t testing.TB) rpc.JoyWaApiClient {
	t.Helper()
	cn, err := grpc.Dial(srv.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	c := rpc.NewJoyWaApiClient(cn)
	if _, err := c.RConnect(context.Background(), &rpc.RequestType{}); err != nil {
		cn.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c.RDisConnect(context.Background(), &rpc.RequestType{})
		cn.Close()
	})
	return c
}

// read 新しい接続で読む（Open → Read → Close）
func read(names ...string) ([]string, error) {
	r := repo()
	if err := r.Open(); err != nil {
		return nil, err
	}
	defer r.Close()
	return r.Read(names)
}

// write 新しい接続で書く（Open → Write → Close）
func write(names []string, vals []string) error {
	r := repo()
	if err := r.Open(); err != nil {
		return err
	}
	defer r.Close()
	return r.Write(names, vals)
}

func names(ts ...Tag) []string {
	ret := make([]string, len(ts))
	for i, t := range ts {
		ret[i] = t.Full()
	}
	return ret
}

// eventually fn が成功するまで繰り返す（JoyWatcher の書き込みと読み込みは非同期なので）
func eventually(t testing.TB, timeout time.Duration, fn func() error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		err := fn()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout(%v): %v", timeout, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// waitPLC PLC の値が want になるまで待つ
func waitPLC(t testing.TB, tag Tag, want Raw) {
	t.Helper()
	eventually(t, 5*time.Second, func() error {
		r, err := plc.Read(tag)
		if err != nil {
			return err
		}
		if fmt.Sprint(r) != fmt.Sprint(want) {
			return fmt.Errorf("PLC %v = %v, want %v", tag, r, want)
		}
		return nil
	})
}

// waitRead RRead で読んだ値が want になるまで待つ
func waitRead(t testing.TB, tag Tag, want string) {
	t.Helper()
	eventually(t, 5*time.Second, func() error {
		v, err := read(tag.Full())
		if err != nil {
			return err
		}
		if !sameValue(tag, v[0], want) {
			return fmt.Errorf("read %v = %q, want %q", tag, v[0], want)
		}
		return nil
	})
}
