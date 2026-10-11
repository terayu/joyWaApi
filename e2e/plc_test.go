package e2e

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/terayu/plccmd/mcprotocol"
)

// PLC plccmd の mcprotocol で PLC を直接読み書きする（UDP）
type PLC struct {
	addr string
	mu   sync.Mutex
	c    *mcprotocol.UDPClient
}

// Raw PLC 上の生の値
type Raw struct {
	Words []uint16
	Bit   bool
}

func NewPLC(addr string) *PLC { return &PLC{addr: addr} }

// do 1 回失敗したら接続を作り直してもう一度試す
func (p *PLC) do(fn func(ctx context.Context, c *mcprotocol.UDPClient) error) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var err error
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if p.c == nil {
			cfg := mcprotocol.DefaultConfig(p.addr)
			cfg.ReadTimeout = 2 * time.Second
			if p.c, err = mcprotocol.NewUDPClient(ctx, cfg); err != nil {
				cancel()
				p.c = nil
				continue
			}
		}
		err = fn(ctx, p.c)
		cancel()
		if err == nil {
			return nil
		}
		p.c.Close()
		p.c = nil
	}
	return err
}

func (p *PLC) Read(t Tag) (r Raw, err error) {
	err = p.do(func(ctx context.Context, c *mcprotocol.UDPClient) error {
		if t.Type == typeBit {
			v, err := c.ReadBits(ctx, t.Dev, t.DevNo, 1)
			if err == nil {
				r.Bit = v[0]
			}
			return err
		}
		var err error
		r.Words, err = c.ReadWords(ctx, t.Dev, t.DevNo, uint16(t.words()))
		return err
	})
	if err != nil {
		err = fmt.Errorf("PLC read %v: %w", t, err)
	}
	return
}

func (p *PLC) Write(t Tag, r Raw) error {
	err := p.do(func(ctx context.Context, c *mcprotocol.UDPClient) error {
		if t.Type == typeBit {
			return c.WriteBits(ctx, t.Dev, t.DevNo, []bool{r.Bit})
		}
		return c.WriteWords(ctx, t.Dev, t.DevNo, r.Words)
	})
	if err != nil {
		err = fmt.Errorf("PLC write %v: %w", t, err)
	}
	return err
}

// Snapshot / Restore テスト前の値を退避して、終了時に戻す
type snapshot struct {
	tag Tag
	raw Raw
}

func (p *PLC) Snapshot(ts []Tag) ([]snapshot, error) {
	var s []snapshot
	for _, t := range ts {
		r, err := p.Read(t)
		if err != nil {
			return nil, err
		}
		s = append(s, snapshot{t, r})
	}
	return s, nil
}

func (p *PLC) Restore(s []snapshot) error {
	var errs []string
	for _, v := range s {
		if err := p.Write(v.tag, v.raw); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// rawNumber 生の値（short は符号付き 16bit、long は符号付き 32bit）
func rawNumber(t Tag, r Raw) int64 {
	switch t.Type {
	case typeShort:
		return int64(int16(r.Words[0]))
	case typeLong:
		return int64(int32(uint32(r.Words[0]) | uint32(r.Words[1])<<16))
	}
	panic(fmt.Sprintf("rawNumber: %v is not number", t))
}

// rawBytes 文字列タグの生のバイト列（1 ワードに 2 文字、下位バイトが先）
func rawBytes(r Raw) []byte {
	b := make([]byte, 0, len(r.Words)*2)
	for _, w := range r.Words {
		b = append(b, byte(w), byte(w>>8))
	}
	return b
}

// expected PLC の生の値から、JoyWatcher が返すはずの値（RRead の文字列）を作る
func expected(t Tag, r Raw) string {
	switch t.Type {
	case typeBit:
		return strconv.FormatBool(r.Bit)
	case typeString:
		b := rawBytes(r)
		if i := strings.IndexByte(string(b), 0); i >= 0 {
			b = b[:i]
		}
		return string(b) // ASCII のみ想定（CP932 の比較は呼び出し側で行う）
	default:
		return strconv.FormatFloat(float64(rawNumber(t, r))*t.PA+t.PB, 'f', -1, 64)
	}
}

// sameValue RRead の値と PLC から作った値が同じか（数値は誤差を許す）
func sameValue(t Tag, got, want string) bool {
	if t.Type == typeBit || t.Type == typeString {
		return got == want
	}
	g, err1 := strconv.ParseFloat(got, 64)
	w, err2 := strconv.ParseFloat(want, 64)
	if err1 != nil || err2 != nil {
		return false
	}
	return math.Abs(g-w) <= 1e-6*math.Max(1, math.Abs(w))
}

// shortRaw short タグの生の値
func shortRaw(v int16) Raw { return Raw{Words: []uint16{uint16(v)}} }
