package graph

import (
	"context"
	"errors"
	"log"

	"github.com/99designs/gqlgen/graphql"
	"github.com/terayu/joyWaApi/graph/model"
	"github.com/terayu/joyWaApi/rpc"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

//go:generate go tool gqlgen generate

// Resolver GraphQL の要求を gRPC サーバーの処理（rpc.Server）で実行する。
// gRPC と同じ処理を通すので、パルスや入力の確認も gRPC と同じになる
type Resolver struct {
	API     rpc.JoyWaApiServer
	MaxTags int // 1 回の要求で扱えるタグ数の上限（0 なら無制限）
}

// MinIntervalMs tagChanged の intervalMs の下限
const MinIntervalMs = 100

// GraphQL エラーの extensions.code
const (
	CodeBadUserInput = "BAD_USER_INPUT"   // 入力の誤り（タグ数の上限、gRPC の InvalidArgument）
	CodeJoyWatcher   = "JOYWATCHER_ERROR" // JoyWatcher での読み書きの失敗
)

func badInput(format string, args ...any) error {
	err := gqlerror.Errorf(format, args...)
	err.Extensions = map[string]any{"code": CodeBadUserInput}
	return err
}

// checkTags タグ数が上限以内か確認する
func (r *Resolver) checkTags(n int) error {
	if r.MaxTags > 0 && n > r.MaxTags {
		return badInput("too many tags(%d): max %d", n, r.MaxTags)
	}
	return nil
}

// ErrorPresenter エラーに extensions.code を付けて返す
func ErrorPresenter(ctx context.Context, err error) *gqlerror.Error {
	gerr := graphql.DefaultErrorPresenter(ctx, err)
	if gerr.Extensions["code"] != nil {
		return gerr
	}
	code := CodeJoyWatcher
	if st, ok := status.FromError(unwrap(err)); ok && st.Code() == codes.InvalidArgument {
		code = CodeBadUserInput
	}
	if gerr.Extensions == nil {
		gerr.Extensions = map[string]any{}
	}
	gerr.Extensions["code"] = code
	log.Printf("warning: joyWaApi:graph: path=%v code=%v err=%v", gerr.Path, code, err)
	return gerr
}

// unwrap gqlgen が包んだエラーから resolver が返したエラーを取り出す
func unwrap(err error) error {
	var gerr *gqlerror.Error
	if errors.As(err, &gerr) && gerr.Err != nil {
		return gerr.Err
	}
	return err
}

func splitTagValues(input []*model.TagValueInput) *rpc.TagValsMessage {
	m := &rpc.TagValsMessage{Tags: make([]string, len(input)), Vals: make([]string, len(input))}
	for i, in := range input {
		m.Tags[i], m.Vals[i] = in.Tag, in.Value
	}
	return m
}

func joinTagValues(tags, vals []string) []*model.TagValue {
	ret := make([]*model.TagValue, len(tags))
	for i := range tags {
		ret[i] = &model.TagValue{Tag: tags[i], Value: vals[i]}
	}
	return ret
}
