//go:build server
// +build server

package core

import (
	"testing"
	"unsafe"
)

// TcomData が C 構造体 TCOM_DATA1 と同じ配置であること
func TestTcomDataLayout(t *testing.T) {
	var td TcomData
	if got := unsafe.Sizeof(td); got != 32 {
		t.Errorf("Sizeof = %d, want 32", got)
	}
	if got := unsafe.Offsetof(td.colID); got != 0 {
		t.Errorf("Offsetof(colID) = %d, want 0", got)
	}
	if got := unsafe.Offsetof(td.colVal); got != 8 {
		t.Errorf("Offsetof(colVal) = %d, want 8", got)
	}
	if got := unsafe.Offsetof(td.dtype); got != 24 {
		t.Errorf("Offsetof(dtype) = %d, want 24", got)
	}
}

func TestStringBytes(t *testing.T) {
	b, err := stringBytes("ABC")
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != stringSize || string(b[:3]) != "ABC" || b[3] != 0 || b[15] != 0 {
		t.Errorf("stringBytes(ABC) = %x", b)
	}
	if _, err := stringBytes("0123456789ABCDEF"); err != nil {
		t.Errorf("16 bytes: %v", err)
	}
	if _, err := stringBytes("0123456789ABCDEFG"); err == nil {
		t.Error("17 bytes: want error")
	}
	// 全角は CP932 で 2 バイト
	if _, err := stringBytes("あいうえおかきくけ"); err == nil {
		t.Error("18 bytes (CP932): want error")
	}
}

func TestFloat64Bytes(t *testing.T) {
	for _, v := range []float64{0, 1, -1.5, 12.34} {
		if got := Float64fromBytes(float64Bytes(v)); got != v {
			t.Errorf("round trip %v = %v", v, got)
		}
	}
}
