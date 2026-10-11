package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const gqlToken = "e2e-token"

// gqlProc テスト用に起動した、GraphQL を有効にした joyWaApi サーバー
type gqlProc struct {
	srv *serverProc
	url string // http://127.0.0.1:port/query
}

// startGraphQL GraphQL を有効にした joyWaApi サーバーを起動して、GraphQL の ping が通るまで待つ
func startGraphQL(t *testing.T) *gqlProc {
	t.Helper()
	addr, err := freeAddr()
	if err != nil {
		t.Fatal(err)
	}
	s, err := startServer(filepath.Join(workDir, "joyWaApi.exe"), filepath.Join(workDir, "joyWaApi-graphql.log"),
		"-gqladdr", addr, "-gqltoken", gqlToken)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	p := &gqlProc{srv: s, url: "http://" + addr + "/query"}

	eventually(t, 15*time.Second, func() error {
		var resp struct{ Ping bool }
		if err := p.do(`{ ping }`, nil, &resp); err != nil {
			return err
		}
		if !resp.Ping {
			return fmt.Errorf("ping = false")
		}
		return nil
	})
	return p
}

// post GraphQL の要求を送り、HTTP のステータスと応答を返す
func (p *gqlProc) post(token, query string, vars map[string]any) (int, []byte, error) {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, _ := http.NewRequest(http.MethodPost, p.url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	var buf bytes.Buffer
	_, err = buf.ReadFrom(res.Body)
	return res.StatusCode, buf.Bytes(), err
}

// do GraphQL の要求を送り、data を out に入れる。errors があればエラーにする
func (p *gqlProc) do(query string, vars map[string]any, out any) error {
	code, b, err := p.post(gqlToken, query, vars)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("status %d: %s", code, b)
	}
	var r struct {
		Data   json.RawMessage
		Errors []json.RawMessage
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return fmt.Errorf("%w: %s", err, b)
	}
	if len(r.Errors) > 0 {
		return fmt.Errorf("graphql errors: %s", b)
	}
	return json.Unmarshal(r.Data, out)
}

// TestGraphQL joyWaApi の GraphQL で読み書きし、PLC で確認する
func TestGraphQL(t *testing.T) {
	g := startGraphQL(t)
	s01, s1, bit, str := fx.short01[7], fx.short1[6], fx.bits[3], fx.str

	t.Run("mutation", func(t *testing.T) {
		var resp map[string]bool
		err := g.do(`mutation($s01:String!,$s1:String!,$bit:String!,$str:String!) {
			write(input:[{tag:$s01,value:"12.3"}])
			writeFloat(input:[{tag:$s1,value:-45}])
			writeBit(input:[{tag:$bit,value:true}])
			writeString(input:[{tag:$str,value:"GQL123"}])
		}`, map[string]any{"s01": s01.Full(), "s1": s1.Full(), "bit": bit.Full(), "str": str.Full()}, &resp)
		if err != nil {
			t.Fatal(err)
		}
		waitPLC(t, s01, shortRaw(123))
		waitPLC(t, s1, shortRaw(-45))
		waitPLC(t, bit, Raw{Bit: true})
		waitRead(t, str, "GQL123")
	})

	t.Run("query", func(t *testing.T) {
		eventually(t, 5*time.Second, func() error {
			var resp struct {
				Read      []struct{ Tag, Value string }
				ReadFloat []struct {
					Tag   string
					Value float64
				}
				ReadBit []struct {
					Tag   string
					Value bool
				}
			}
			if err := g.do(`query($t:[String!]!,$b:[String!]!) {
				read(tags:$t){tag value} readFloat(tags:$t){tag value} readBit(tags:$b){tag value}
			}`, map[string]any{"t": names(s01, s1), "b": names(bit)}, &resp); err != nil {
				return err
			}
			if len(resp.Read) != 2 || !sameValue(s01, resp.Read[0].Value, "12.3") || !sameValue(s1, resp.Read[1].Value, "-45") {
				return fmt.Errorf("read = %v", resp.Read)
			}
			if len(resp.ReadFloat) != 2 || resp.ReadFloat[0].Value != 12.3 || resp.ReadFloat[1].Value != -45 {
				return fmt.Errorf("readFloat = %v", resp.ReadFloat)
			}
			if len(resp.ReadBit) != 1 || !resp.ReadBit[0].Value {
				return fmt.Errorf("readBit = %v", resp.ReadBit)
			}
			return nil
		})
	})

	t.Run("pulse", func(t *testing.T) {
		tag := fx.bits[4]
		var resp map[string]bool
		if err := g.do(`mutation($t:String!) { writeBit(input:[{tag:$t,value:true,pulseSec:1}]) }`,
			map[string]any{"t": tag.Full()}, &resp); err != nil {
			t.Fatal(err)
		}
		waitPLC(t, tag, Raw{Bit: true})
		waitPLC(t, tag, Raw{Bit: false})
	})

	t.Run("unauthorized", func(t *testing.T) {
		for _, token := range []string{"", "wrong"} {
			code, b, err := g.post(token, `{ ping }`, nil)
			if err != nil {
				t.Fatal(err)
			}
			if code != http.StatusUnauthorized {
				t.Errorf("token=%q: status %d: %s", token, code, b)
			}
		}
	})

	t.Run("error", func(t *testing.T) {
		code, b, err := g.post(gqlToken, `{ read(tags:["LOCAL$NOT.EXIST$VALUE"]){tag value} }`, nil)
		if err != nil {
			t.Fatal(err)
		}
		if code != http.StatusOK || !strings.Contains(string(b), `"code":"JOYWATCHER_ERROR"`) {
			t.Errorf("status %d: %s", code, b)
		}
	})

	t.Run("subscription", func(t *testing.T) {
		tag := fx.short01[7]
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		c, _, err := websocket.Dial(ctx, strings.Replace(g.url, "http://", "ws://", 1),
			&websocket.DialOptions{Subprotocols: []string{"graphql-transport-ws"}})
		if err != nil {
			t.Fatal(err)
		}
		defer c.CloseNow()

		type msg struct {
			ID      string          `json:"id,omitempty"`
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload,omitempty"`
		}
		send := func(m any) {
			if err := wsjson.Write(ctx, c, m); err != nil {
				t.Fatal(err)
			}
		}
		// next の payload の tagChanged を返す（ping/pong は読み飛ばす）
		next := func() []struct{ Tag, Value string } {
			for {
				var m msg
				if err := wsjson.Read(ctx, c, &m); err != nil {
					t.Fatal(err)
				}
				switch m.Type {
				case "ping", "pong", "ka":
					continue
				case "next":
					var p struct {
						Data struct{ TagChanged []struct{ Tag, Value string } }
					}
					if err := json.Unmarshal(m.Payload, &p); err != nil {
						t.Fatal(err)
					}
					return p.Data.TagChanged
				default:
					t.Fatalf("unexpected message: %v %s", m.Type, m.Payload)
				}
			}
		}

		send(map[string]any{"type": "connection_init", "payload": map[string]string{"Authorization": "Bearer " + gqlToken}})
		var ack msg
		if err := wsjson.Read(ctx, c, &ack); err != nil || ack.Type != "connection_ack" {
			t.Fatalf("connection_ack: %v %v", ack, err)
		}
		send(map[string]any{"id": "1", "type": "subscribe", "payload": map[string]any{
			"query":     `subscription($t:[String!]!) { tagChanged(tags:$t, intervalMs:200){tag value} }`,
			"variables": map[string]any{"t": names(tag)},
		}})

		if got := next(); len(got) != 1 || !sameValue(tag, got[0].Value, "12.3") {
			t.Fatalf("first = %v", got)
		}
		// PLC に直接書いた値の変化が届く
		if err := plc.Write(tag, shortRaw(-77)); err != nil {
			t.Fatal(err)
		}
		if got := next(); len(got) != 1 || !sameValue(tag, got[0].Value, "-7.7") {
			t.Fatalf("changed = %v", got)
		}
	})

	t.Run("subscription unauthorized", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c, _, err := websocket.Dial(ctx, strings.Replace(g.url, "http://", "ws://", 1),
			&websocket.DialOptions{Subprotocols: []string{"graphql-transport-ws"}})
		if err != nil {
			t.Fatal(err)
		}
		defer c.CloseNow()
		if err := wsjson.Write(ctx, c, map[string]any{"type": "connection_init", "payload": map[string]string{"Authorization": "Bearer wrong"}}); err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := wsjson.Read(ctx, c, &m); err == nil && m["type"] == "connection_ack" {
			t.Errorf("connection_ack with wrong token")
		}
	})
}
