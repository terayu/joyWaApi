package e2e

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/terayu/joyWaApi/rpc"
	"golang.org/x/text/encoding/japanese"
)

func cp932(t testing.TB, s string) []byte {
	t.Helper()
	b, err := japanese.ShiftJIS.NewEncoder().Bytes([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestWriteString 文字列タグ（fx.str、12 バイト）への書き込み
//
// 実機で確認した仕様: 1 ワードに 2 文字（下位バイトが先）。STRLENGTH を超えた分は切り捨て、残りは 0 で埋める。
// joyWaApi は 16 バイトを超える文字列をエラーにする。
func TestWriteString(t *testing.T) {
	tag := fx.str
	cases := []struct {
		name string
		val  string
		want string // PLC に入る文字列（STRLENGTH で切り捨て）
	}{
		{"short", "AB12cd", "AB12cd"},
		{"odd", "ABC", "ABC"},
		{"full", "ABCDEFGHIJKL", "ABCDEFGHIJKL"},
		{"16bytes", "ABCDEFGHIJKLMNOP", "ABCDEFGHIJKL"},
		{"shorter", "XY", "XY"}, // 前の値が残らない
		{"hankaku-kana", "ﾛｯﾄ001", "ﾛｯﾄ001"},
		{"zenkaku", "ロット", "ロット"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := write(names(tag), []string{c.val}); err != nil {
				t.Fatal(err)
			}
			want := make([]byte, tag.words()*2)
			copy(want, cp932(t, c.want))
			eventually(t, 5*time.Second, func() error {
				r, err := plc.Read(tag)
				if err != nil {
					return err
				}
				if got := rawBytes(r); !bytes.Equal(got, want) {
					return fmt.Errorf("PLC = %x, want %x", got, want)
				}
				return nil
			})
			waitRead(t, tag, c.want)
		})
	}
}

// TestWriteStringRPC RWriteS と、長すぎる文字列のエラー
func TestWriteStringRPC(t *testing.T) {
	c := client(t)
	ctx := context.Background()
	tag := fx.str
	if _, err := c.RWriteS(ctx, &rpc.TagValsMessage{Tags: names(tag), Vals: []string{"RPC-01"}}); err != nil {
		t.Fatal(err)
	}
	waitRead(t, tag, "RPC-01")

	for _, v := range []string{"ABCDEFGHIJKLMNOPQ", "あいうえおかきくけ"} { // 17 バイト、18 バイト（CP932）
		if _, err := c.RWriteS(ctx, &rpc.TagValsMessage{Tags: names(tag), Vals: []string{v}}); err == nil || !strings.Contains(err.Error(), "string too long") {
			t.Errorf("RWriteS(%q) err = %v, want string too long", v, err)
		}
		if err := write(names(tag), []string{v}); err == nil {
			t.Errorf("Write(%q) err = nil, want error", v)
		}
	}
	// エラーのときは書き込まれない
	waitRead(t, tag, "RPC-01")
}
