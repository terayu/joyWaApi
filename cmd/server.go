package cmd

import (
	"context"
	"flag"
	"fmt"
	"github.com/terayu/joyWaApi/core"
	"github.com/terayu/joyWaApi/graph"
	"github.com/terayu/joyWaApi/rpc"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type serverCmd struct {
	fs               *flag.FlagSet
	RpcServerAddress string
	FailTimeout      int // 秒。DLL の読み書きがこの時間失敗し続けたら終了する（0 で無効）
	WatchInterval    int // 秒。接続を確認する間隔
	Gql              graph.ServerConfig
}

var ServerCmd serverCmd

func init() {
	ServerCmd.fs = flag.NewFlagSet("server", flag.ExitOnError)
	ServerCmd.fs.StringVar(&ServerCmd.RpcServerAddress, "rpcaddr", "127.0.0.1:9999", "joyWpaServer待受IP:Port")
	ServerCmd.fs.IntVar(&ServerCmd.FailTimeout, "failtimeout", 30, "JoyWatcherとの読み書きがこの秒数失敗し続けたら終了する(0で無効)")
	ServerCmd.fs.IntVar(&ServerCmd.WatchInterval, "watchinterval", 5, "JoyWatcherとの接続を確認する間隔(秒)")
	ServerCmd.fs.StringVar(&ServerCmd.Gql.Addr, "gqladdr", "", "GraphQL待受IP:Port(空なら起動しない) exp. 0.0.0.0:8080")
	ServerCmd.fs.StringVar(&ServerCmd.Gql.Token, "gqltoken", "", "GraphQLのアクセストークン(Authorization: Bearer <token>)。ループバック以外で待ち受けるときは必須")
	ServerCmd.fs.BoolVar(&ServerCmd.Gql.Insecure, "gqlinsecure", false, "トークンなしでループバック以外での待ち受けを許す")
	ServerCmd.fs.IntVar(&ServerCmd.Gql.MaxTags, "gqlmaxtags", 1000, "GraphQLの1回の要求で扱えるタグ数の上限(0で無制限)")
	ServerCmd.fs.BoolVar(&ServerCmd.Gql.Playground, "gqlplayground", false, "/ で GraphQL Playground を表示する")

	ServerCmd.fs.VisitAll(func(f *flag.Flag) {
		if v := os.Getenv("JOYWAAPI_SERVER_" + strings.ToUpper(f.Name)); v != "" {
			if err := f.Value.Set(v); err != nil {
				panic(err)
			}
		}
	})
}
func (r *serverCmd) Run(args []string) error {
	if err := r.fs.Parse(args); err != nil {
		return err
	}

	if err := core.Load(); err != nil {
		return fmt.Errorf("fail load JoyWaApi.dll: %w", err)
	}

	s := rpc.NewServer(rpc.SetNetWork("tcp"), rpc.SetAddress(r.RpcServerAddress))

	// GraphQL は gRPC と同じ処理（rpc.Server）を呼ぶ
	var gs *graph.Server
	if r.Gql.Addr != "" {
		var err error
		if gs, err = graph.NewServer(s, r.Gql); err != nil {
			return err
		}
		if err = gs.Listen(); err != nil {
			return err
		}
	}

	// サーバー自身で接続を1つ保持し、クライアントの切断で接続数が0にならないようにする
	// （DLL は参照カウント方式）
	if err := s.GetAPI().Hold(); err != nil {
		log.Printf("warning: Fail Connect JoyWatcher:%v", err)
	}

	// Create a channel to receive signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Start the server in a goroutine
	errCh := make(chan error, 2)
	go func() {
		log.Println("Starting server...")
		err := s.ServerStart()
		if err != nil {
			log.Printf("err: Fail Server Start:%v", err)
		}
		errCh <- err
	}()
	if gs != nil {
		go func() {
			if err := gs.Serve(); err != nil {
				log.Printf("err: Fail GraphQL Server Start:%v", err)
				errCh <- err
			}
		}()
	}
	stop := func() {
		s.ServerStop()
		if gs != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			gs.Shutdown(ctx)
		}
	}

	// JoyWSrv2 を再起動すると、DLL の接続がこのプロセスでは回復しないことがある（接続し直しても直らない）。
	// 読み書きが失敗し続けたら終了して、サービスなどから再起動してもらう
	watchCh := make(chan error, 1)
	if r.FailTimeout > 0 {
		go watch(s.GetAPI(), time.Duration(r.WatchInterval)*time.Second, time.Duration(r.FailTimeout)*time.Second, watchCh)
	}

	// Wait for a signal or an error
	select {
	case <-sigCh:
		log.Println("Received termination signal, shutting down...")
		// Stop the server
		stop()
		// Disconnect from the JoyWatcher server
		if s.GetAPI() != nil {
			s.GetAPI().Release()
		}
		log.Println("Server stopped")
	case err := <-errCh:
		stop()
		return err
	case err := <-watchCh:
		log.Printf("alert: %v", err)
		stop()
		return err
	}

	return nil
}

// watch 一定間隔で接続を確認し、読み書きが timeout の間失敗し続けたら ch に通知する
func watch(api *core.Handle, interval, timeout time.Duration, ch chan<- error) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for range t.C {
		if _, err := api.Ping(); err != nil {
			log.Printf("warning: joyWaApi:watch: ping failed(%v) failingFor=%v", err, api.FailingFor().Round(time.Second))
		}
		if d := api.FailingFor(); d >= timeout {
			ch <- fmt.Errorf("JoyWatcher read/write has been failing for %v; exit to be restarted", d.Round(time.Second))
			return
		}
	}
}
