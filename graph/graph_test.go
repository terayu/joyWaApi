package graph_test

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/99designs/gqlgen/client"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/terayu/joyWaApi/graph"
	rpc "github.com/terayu/joyWaApi/rpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeServer rpc.Server（DLL を呼ぶ gRPC サーバーの処理）の代わり（タグの値をメモリに持つ）
type fakeServer struct {
	rpc.UnimplementedJoyWaApiServer
	mu    sync.Mutex
	vals  map[string]string
	pulse map[string]int32
	calls []string
}

func (s *fakeServer) call(name string) {
	s.mu.Lock()
	s.calls = append(s.calls, name)
	s.mu.Unlock()
}

func (s *fakeServer) get(tag string) (string, error) {
	v, ok := s.vals[tag]
	if !ok {
		return "", status.Errorf(codes.Unknown, "tag not found: %v", tag)
	}
	return v, nil
}

func (s *fakeServer) RPing(context.Context, *rpc.RequestType) (*rpc.BoolMessage, error) {
	s.call("RPing")
	return &rpc.BoolMessage{Val: true}, nil
}

func (s *fakeServer) RRead(_ context.Context, m *rpc.TagsMessage) (*rpc.ValsMessage, error) {
	s.call("RRead")
	s.mu.Lock()
	defer s.mu.Unlock()
	ret := &rpc.ValsMessage{}
	for _, t := range m.Tags {
		v, err := s.get(t)
		if err != nil {
			return nil, err
		}
		ret.Vals = append(ret.Vals, v)
	}
	return ret, nil
}

func (s *fakeServer) RReadF(_ context.Context, m *rpc.TagsMessage) (*rpc.ValsMessage, error) {
	s.call("RReadF")
	s.mu.Lock()
	defer s.mu.Unlock()
	ret := &rpc.ValsMessage{}
	for _, t := range m.Tags {
		v, err := s.get(t)
		if err != nil {
			return nil, err
		}
		f, _ := strconv.ParseFloat(v, 64)
		ret.Vals = append(ret.Vals, strconv.FormatFloat(f, 'f', 4, 64))
	}
	return ret, nil
}

func (s *fakeServer) RReadB(_ context.Context, m *rpc.TagBitMessage) (*rpc.BitMessage, error) {
	s.call("RReadB")
	s.mu.Lock()
	defer s.mu.Unlock()
	ret := &rpc.BitMessage{}
	for _, t := range m.Tags {
		v, err := s.get(t)
		if err != nil {
			return nil, err
		}
		ret.Vals = append(ret.Vals, v != "0")
	}
	return ret, nil
}

func (s *fakeServer) set(name string, tags, vals []string) (*rpc.NoReturnMessage, error) {
	s.call(name)
	if len(tags) != len(vals) {
		return nil, status.Errorf(codes.InvalidArgument, "number of tags(%d) and vals(%d) mismatch", len(tags), len(vals))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range tags {
		s.vals[tags[i]] = vals[i]
	}
	return &rpc.NoReturnMessage{}, nil
}

func (s *fakeServer) RWrite(_ context.Context, m *rpc.TagValsMessage) (*rpc.NoReturnMessage, error) {
	return s.set("RWrite", m.Tags, m.Vals)
}

func (s *fakeServer) RWriteS(_ context.Context, m *rpc.TagValsMessage) (*rpc.NoReturnMessage, error) {
	return s.set("RWriteS", m.Tags, m.Vals)
}

func (s *fakeServer) RWriteF(_ context.Context, m *rpc.TagValsMessage) (*rpc.NoReturnMessage, error) {
	return s.set("RWriteF", m.Tags, m.Vals)
}

func (s *fakeServer) RWriteB(_ context.Context, m *rpc.TagBitMessage) (*rpc.NoReturnMessage, error) {
	vals := make([]string, len(m.Vals))
	for i, v := range m.Vals {
		vals[i] = map[bool]string{true: "1", false: "0"}[v]
	}
	s.mu.Lock()
	for i, p := range m.Pulse {
		s.pulse[m.Tags[i]] = p
	}
	s.mu.Unlock()
	return s.set("RWriteB", m.Tags, vals)
}

func (s *fakeServer) Set(tag, val string) {
	s.mu.Lock()
	s.vals[tag] = val
	s.mu.Unlock()
}

func (s *fakeServer) Val(tag string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vals[tag]
}

func (s *fakeServer) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

// setup fakeServer を起動し、それにつなぐ GraphQL クライアントを返す
func setup(t *testing.T, maxTags int) (*fakeServer, *client.Client) {
	t.Helper()
	fs := &fakeServer{
		vals:  map[string]string{"A": "1", "B": "abc", "F": "1.5", "BIT": "0"},
		pulse: map[string]int32{},
	}
	srv := handler.New(graph.NewExecutableSchema(graph.Config{Resolvers: &graph.Resolver{API: fs, MaxTags: maxTags}}))
	srv.AddTransport(transport.Websocket{})
	srv.AddTransport(transport.POST{})
	srv.SetErrorPresenter(graph.ErrorPresenter)
	return fs, client.New(srv)
}

type tagValue struct {
	Tag   string
	Value string
}

func TestRead(t *testing.T) {
	_, c := setup(t, 0)
	var resp struct {
		Ping      bool
		Read      []tagValue
		ReadFloat []struct {
			Tag   string
			Value float64
		}
		ReadBit []struct {
			Tag   string
			Value bool
		}
	}
	c.MustPost(`{ ping read(tags:["A","B"]){tag value} readFloat(tags:["F"]){tag value} readBit(tags:["BIT","A"]){tag value} }`, &resp)

	if !resp.Ping {
		t.Error("ping = false")
	}
	if want := []tagValue{{"A", "1"}, {"B", "abc"}}; !equal(resp.Read, want) {
		t.Errorf("read = %v, want %v", resp.Read, want)
	}
	if len(resp.ReadFloat) != 1 || resp.ReadFloat[0].Value != 1.5 {
		t.Errorf("readFloat = %v", resp.ReadFloat)
	}
	if len(resp.ReadBit) != 2 || resp.ReadBit[0].Value || !resp.ReadBit[1].Value {
		t.Errorf("readBit = %v", resp.ReadBit)
	}
}

func TestWrite(t *testing.T) {
	fs, c := setup(t, 0)
	var resp map[string]bool
	c.MustPost(`mutation {
		write(input:[{tag:"A",value:"10"}])
		writeString(input:[{tag:"B",value:"xyz"}])
		writeFloat(input:[{tag:"F",value:2.25}])
		writeBit(input:[{tag:"BIT",value:true,pulseSec:3},{tag:"A",value:false}])
	}`, &resp)

	for _, k := range []string{"write", "writeString", "writeFloat", "writeBit"} {
		if !resp[k] {
			t.Errorf("%s = false", k)
		}
	}
	for tag, want := range map[string]string{"A": "0", "B": "xyz", "F": "2.25", "BIT": "1"} {
		if got := fs.Val(tag); got != want {
			t.Errorf("%s = %q, want %q", tag, got, want)
		}
	}
	if fs.pulse["BIT"] != 3 || fs.pulse["A"] != 0 {
		t.Errorf("pulse = %v", fs.pulse)
	}
	want := []string{"RWrite", "RWriteS", "RWriteF", "RWriteB"}
	if got := fs.Calls(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("calls = %v, want %v（Mutation は順番に実行される）", got, want)
	}
}

// TestWriteBitNoPulse pulseSec を指定しなければ Pulse を送らない
func TestWriteBitNoPulse(t *testing.T) {
	fs, c := setup(t, 0)
	var resp map[string]bool
	c.MustPost(`mutation { writeBit(input:[{tag:"BIT",value:true}]) }`, &resp)
	if len(fs.pulse) != 0 {
		t.Errorf("pulse = %v", fs.pulse)
	}
}

func TestErrors(t *testing.T) {
	_, c := setup(t, 2)
	tests := []struct {
		name  string
		query string
		code  string
	}{
		{"too many tags", `{ read(tags:["A","B","F"]){tag} }`, graph.CodeBadUserInput},
		{"too many inputs", `mutation { write(input:[{tag:"A",value:"1"},{tag:"A",value:"1"},{tag:"A",value:"1"}]) }`, graph.CodeBadUserInput},
		{"negative pulse", `mutation { writeBit(input:[{tag:"A",value:true,pulseSec:-1}]) }`, graph.CodeBadUserInput},
		{"unknown tag", `{ read(tags:["NONE"]){tag} }`, graph.CodeJoyWatcher},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp any
			err := c.Post(tt.query, &resp)
			if err == nil {
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), `"code":"`+tt.code+`"`) {
				t.Errorf("error = %v, want code %v", err, tt.code)
			}
		})
	}
}

func TestTagChanged(t *testing.T) {
	fs, c := setup(t, 0)
	sub := c.Websocket(`subscription { tagChanged(tags:["A","B"], intervalMs:100){tag value} }`)
	defer sub.Close()

	next := func() []tagValue {
		t.Helper()
		var resp struct{ TagChanged []tagValue }
		if err := sub.Next(&resp); err != nil {
			t.Fatal(err)
		}
		return resp.TagChanged
	}

	// 最初は全タグ
	if got, want := next(), []tagValue{{"A", "1"}, {"B", "abc"}}; !equal(got, want) {
		t.Errorf("first = %v, want %v", got, want)
	}
	// 変わったタグだけ
	fs.Set("B", "changed")
	if got, want := next(), []tagValue{{"B", "changed"}}; !equal(got, want) {
		t.Errorf("changed = %v, want %v", got, want)
	}
}

func TestTagChangedInvalid(t *testing.T) {
	_, c := setup(t, 0)
	for _, q := range []string{
		`subscription { tagChanged(tags:["A"], intervalMs:10){tag} }`,
		`subscription { tagChanged(tags:[]){tag} }`,
	} {
		sub := c.Websocket(q)
		var resp any
		if err := sub.Next(&resp); err == nil || !strings.Contains(err.Error(), graph.CodeBadUserInput) {
			t.Errorf("%s: error = %v, want %v", q, err, graph.CodeBadUserInput)
		}
		sub.Close()
	}
}

func equal(a, b []tagValue) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
