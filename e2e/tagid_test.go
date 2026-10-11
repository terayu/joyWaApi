//go:build server

package e2e

import (
	"testing"

	"github.com/terayu/joyWaApi/core"
)

// TestTagID core から DLL を直接呼び、全タグの ID と型がタグリストの COL_ID・TYPE と一致すること
// （TcomData の配置や JWGetTagIDS2 の引数の確認。-tags server と GOARCH=386 が必要）
func TestTagID(t *testing.T) {
	if err := core.Load(); err != nil {
		t.Fatal(err)
	}
	h := core.New()

	// 接続していないときは ID を取れない（以前は ID 0 を返していた）
	if id, _, err := h.GetTagID(fx.short01[0].Full()); err == nil {
		t.Errorf("GetTagID without connect = %d, want error", id)
	}

	if err := h.Connect(); err != nil {
		t.Fatal(err)
	}
	defer h.DisConnect()

	for _, tag := range tags.All {
		id, typ, err := h.GetTagID(tag.Full())
		if err != nil {
			t.Errorf("%v: %v", tag, err)
			continue
		}
		if id != tag.ColID || int(typ) != tag.Type {
			t.Errorf("%v: id=%d type=%d, want id=%d type=%d", tag, id, typ, tag.ColID, tag.Type)
		}
	}
	if _, _, err := h.GetTagID(unknownTag); err == nil {
		t.Error("GetTagID(unknown): err = nil")
	}
	t.Logf("checked %d tag IDs", len(tags.All))
}
