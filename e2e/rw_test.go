package e2e

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/terayu/joyWaApi/rpc"
)

func TestPing(t *testing.T) {
	ok, err := srv.ping()
	if err != nil || !ok {
		t.Fatalf("RPing = %v, %v", ok, err)
	}
}

// TestReadAllTags タグリストの全タグを RRead で読み、PLC から直接読んだ値と比べる
// （運転中に変わる値もあるので、違ったものは数回読み直す）
func TestReadAllTags(t *testing.T) {
	var targets []Tag
	for _, tag := range tags.All {
		if tag.Type == typeShort || tag.Type == typeLong || tag.Type == typeBit || tag.Type == typeString {
			targets = append(targets, tag)
		}
	}

	compare := func(ts []Tag) (ng []Tag, msgs []string) {
		const chunk = 200
		for i := 0; i < len(ts); i += chunk {
			part := ts[i:min(i+chunk, len(ts))]
			vals, err := read(names(part...)...)
			if err != nil {
				t.Fatalf("read %d tags: %v", len(part), err)
			}
			if len(vals) != len(part) {
				t.Fatalf("read returned %d values, want %d", len(vals), len(part))
			}
			for j, tag := range part {
				r, err := plc.Read(tag)
				if err != nil {
					t.Fatal(err)
				}
				if want := expected(tag, r); !sameValue(tag, vals[j], want) {
					ng = append(ng, tag)
					msgs = append(msgs, fmt.Sprintf("%v: read=%q PLC=%q", tag, vals[j], want))
				}
			}
		}
		return
	}

	ng, msgs := compare(targets)
	for retry := 0; retry < 3 && len(ng) > 0; retry++ {
		time.Sleep(time.Second)
		ng, msgs = compare(ng)
	}
	for _, m := range msgs {
		t.Error(m)
	}
	t.Logf("compared %d tags (short/long/bit/string)", len(targets))
}

// TestWriteNumeric 数値の書き込み（スケール変換と四捨五入）を PLC で確認し、読み戻す
func TestWriteNumeric(t *testing.T) {
	s01, s1 := fx.short01[0], fx.short1[0]
	cases := []struct {
		tag  Tag
		val  string
		want int16
	}{
		{s01, "12.3", 123},
		{s01, "12.34", 123},
		{s01, "12.36", 124},
		{s01, "-1.5", -15},
		{s01, "3276.7", 32767},
		{s01, "-3276.8", -32768},
		{s01, "0", 0},
		{s1, "100", 100},
		{s1, "1.4", 1},
		{s1, "1.6", 2},
		{s1, "-1.6", -2},
		{s1, "32767", 32767},
		{s1, "-32768", -32768},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s=%s", c.tag.Name, c.val), func(t *testing.T) {
			if err := write([]string{c.tag.Full()}, []string{c.val}); err != nil {
				t.Fatal(err)
			}
			waitPLC(t, c.tag, shortRaw(c.want))
			waitRead(t, c.tag, strconv.FormatFloat(float64(c.want)*c.tag.PA, 'f', -1, 64))
		})
	}
}

// TestWriteNumericRPC RWriteF と RReadF
func TestWriteNumericRPC(t *testing.T) {
	c := client(t)
	ctx := context.Background()
	tag := fx.short01[1]
	if _, err := c.RWriteF(ctx, &rpc.TagValsMessage{Tags: names(tag), Vals: []string{"45.6"}}); err != nil {
		t.Fatal(err)
	}
	waitPLC(t, tag, shortRaw(456))
	eventually(t, 5*time.Second, func() error {
		r, err := c.RReadF(ctx, &rpc.TagsMessage{Tags: names(tag)})
		if err != nil {
			return err
		}
		if r.Vals[0] != "45.6000" {
			return fmt.Errorf("RReadF = %q, want 45.6000", r.Vals[0])
		}
		return nil
	})
}

// TestWriteLong long（32bit）の書き込み
func TestWriteLong(t *testing.T) {
	tag := fx.long
	for _, v := range []int32{123456, -2, 2147483647, -2147483648, 0} {
		t.Run(strconv.Itoa(int(v)), func(t *testing.T) {
			if err := write(names(tag), []string{strconv.Itoa(int(v))}); err != nil {
				t.Fatal(err)
			}
			waitPLC(t, tag, Raw{Words: []uint16{uint16(uint32(v)), uint16(uint32(v) >> 16)}})
			waitRead(t, tag, strconv.Itoa(int(v)))
		})
	}
}

// TestWriteBit BIT の書き込み（RWrite は "true"/"1" が ON、それ以外は OFF）
func TestWriteBit(t *testing.T) {
	tag := fx.bits[0]
	for _, c := range []struct {
		val  string
		want bool
	}{{"true", true}, {"false", false}, {"1", true}, {"0", false}} {
		t.Run(c.val, func(t *testing.T) {
			if err := write(names(tag), []string{c.val}); err != nil {
				t.Fatal(err)
			}
			waitPLC(t, tag, Raw{Bit: c.want})
			waitRead(t, tag, strconv.FormatBool(c.want))
		})
	}
}

// TestWriteBitRPC RWriteB / RReadB / RReadF（BIT は 1/0）
func TestWriteBitRPC(t *testing.T) {
	c := client(t)
	ctx := context.Background()
	ts := fx.bits[1:3]
	if _, err := c.RWriteB(ctx, &rpc.TagBitMessage{Tags: names(ts...), Vals: []bool{true, false}}); err != nil {
		t.Fatal(err)
	}
	waitPLC(t, ts[0], Raw{Bit: true})
	waitPLC(t, ts[1], Raw{Bit: false})
	eventually(t, 5*time.Second, func() error {
		b, err := c.RReadB(ctx, &rpc.TagBitMessage{Tags: names(ts...)})
		if err != nil {
			return err
		}
		f, err := c.RReadF(ctx, &rpc.TagsMessage{Tags: names(ts...)})
		if err != nil {
			return err
		}
		if !b.Vals[0] || b.Vals[1] || f.Vals[0] != "1.0000" || f.Vals[1] != "0.0000" {
			return fmt.Errorf("RReadB=%v RReadF=%v", b.Vals, f.Vals)
		}
		return nil
	})
}

// TestPulse RWriteB のパルス（指定秒数後に OFF になる）
func TestPulse(t *testing.T) {
	c := client(t)
	tag := fx.bits[3]
	start := time.Now()
	if _, err := c.RWriteB(context.Background(), &rpc.TagBitMessage{Tags: names(tag), Vals: []bool{true}, Pulse: []int32{1}}); err != nil {
		t.Fatal(err)
	}
	waitPLC(t, tag, Raw{Bit: true})
	if d := time.Since(start); d > 900*time.Millisecond {
		t.Logf("ON was observed late (%v); skip the early-OFF check", d)
	} else if r, _ := plc.Read(tag); !r.Bit {
		t.Fatal("pulse turned OFF too early")
	}
	waitPLC(t, tag, Raw{Bit: false})
	if d := time.Since(start); d < 900*time.Millisecond {
		t.Errorf("pulse OFF after %v, want about 1s", d)
	}
}

// TestReadFromPLC PLC に直接書いた値を RRead で読む（PLC → JoyWatcher の向き）
func TestReadFromPLC(t *testing.T) {
	num, bit := fx.short01[2], fx.bits[4]
	for _, v := range []int16{4321, -77} {
		if err := plc.Write(num, shortRaw(v)); err != nil {
			t.Fatal(err)
		}
		waitRead(t, num, strconv.FormatFloat(float64(v)*num.PA, 'f', -1, 64))
	}
	for _, v := range []bool{true, false} {
		if err := plc.Write(bit, Raw{Bit: v}); err != nil {
			t.Fatal(err)
		}
		waitRead(t, bit, strconv.FormatBool(v))
	}
}

// TestReadManyTagsAtOnce 1 回の Read で多数のタグを読む
func TestReadManyTagsAtOnce(t *testing.T) {
	var ns []string
	for _, tag := range tags.All {
		ns = append(ns, tag.Full())
	}
	vals, err := read(ns...)
	if err != nil {
		t.Fatal(err)
	}
	if len(vals) != len(ns) {
		t.Fatalf("got %d values, want %d", len(vals), len(ns))
	}
	t.Logf("read %d tags in one call", len(ns))
}
