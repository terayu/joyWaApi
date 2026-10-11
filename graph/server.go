package graph

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/terayu/joyWaApi/rpc"
)

// ServerConfig GraphQL サーバーの設定
type ServerConfig struct {
	Addr       string // 待ち受けるアドレス
	Token      string // アクセストークン（空なら確認しない）
	Insecure   bool   // トークンなしでループバック以外での待ち受けを許す
	MaxTags    int    // 1 回の要求で扱えるタグ数の上限（0 なら無制限）
	Playground bool   // / で GraphQL Playground を表示する
}

// Server GraphQL の HTTP サーバー（エンドポイントは /query）
type Server struct {
	cfg ServerConfig
	hs  *http.Server
	ln  net.Listener
}

// NewServer api（gRPC サーバーの処理）を GraphQL で公開するサーバーを作る
func NewServer(api rpc.JoyWaApiServer, cfg ServerConfig) (*Server, error) {
	if cfg.Token == "" && !cfg.Insecure && !isLoopback(cfg.Addr) {
		return nil, fmt.Errorf("graphql address %q is not loopback: set token (or insecure to allow access without token)", cfg.Addr)
	}
	s := &Server{cfg: cfg}

	gs := handler.New(NewExecutableSchema(Config{Resolvers: &Resolver{API: api, MaxTags: cfg.MaxTags}}))
	gs.AddTransport(transport.Websocket{
		KeepAlivePingInterval: 10 * time.Second,
		InitFunc:              s.wsInit,
	})
	gs.AddTransport(transport.Options{})
	gs.AddTransport(transport.GET{})
	gs.AddTransport(transport.POST{})
	gs.Use(extension.Introspection{})
	gs.SetErrorPresenter(ErrorPresenter)

	mux := http.NewServeMux()
	mux.Handle("/query", s.auth(gs))
	if cfg.Playground {
		mux.Handle("/", playground.Handler("joyWaApi GraphQL", "/query"))
	}
	s.hs = &http.Server{Addr: cfg.Addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	return s, nil
}

// Listen 待ち受けを始める（ポートが使えないなどのエラーを起動時に返すため Serve と分ける）
func (s *Server) Listen() (err error) {
	if s.ln, err = net.Listen("tcp", s.cfg.Addr); err != nil {
		return fmt.Errorf("graphql listen %v: %w", s.cfg.Addr, err)
	}
	log.Printf("info: joyWaApi:graph: GraphQL server listen=%v", s.ln.Addr())
	return nil
}

// Serve 要求を処理する。Shutdown で止めたら nil を返す
// this operation blocking
func (s *Server) Serve() error {
	if err := s.hs.Serve(s.ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.hs.Shutdown(ctx)
}

type authorizedKey struct{}

func (s *Server) authorized(header string) bool {
	return subtle.ConstantTimeCompare([]byte(header), []byte("Bearer "+s.cfg.Token)) == 1
}

// auth Authorization ヘッダーのトークンを確認する。
// WebSocket はヘッダーを付けられないクライアントもあるので、ヘッダーがなければ connection_init で確認する（wsInit）
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Token == "" {
			next.ServeHTTP(w, r)
			return
		}
		if s.authorized(r.Header.Get("Authorization")) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authorizedKey{}, true)))
			return
		}
		if r.Header.Get("Authorization") == "" && strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			next.ServeHTTP(w, r)
			return
		}
		log.Printf("warning: joyWaApi:graph: unauthorized request from %v", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

// wsInit WebSocket の connection_init の payload {"Authorization": "Bearer <token>"} を確認する
func (s *Server) wsInit(ctx context.Context, p transport.InitPayload) (context.Context, *transport.InitPayload, error) {
	if s.cfg.Token == "" || ctx.Value(authorizedKey{}) == true || s.authorized(p.Authorization()) {
		return ctx, nil, nil
	}
	log.Printf("warning: joyWaApi:graph: unauthorized websocket connection")
	return ctx, nil, errors.New("unauthorized")
}

// isLoopback addr のホストがループバックか（ホストを省略したら全インターフェース）
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
