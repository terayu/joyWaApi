//go:build server
// +build server

package core

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"syscall"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

// TcomData DLL 側の C 構造体 TCOM_DATA1 と同じ配置（32bit MSVC で 32 バイト）
//
//	typedef struct {
//		long col_id;
//		union { double dblVal; char pbVal[16]; };
//		char dtype;
//	} TCOM_DATA1;
//
//	col_id@0, union{double dblVal; char pbVal[16]}@8, dtype@24
type TcomData struct {
	colID  int32
	_      [4]byte
	colVal [16]byte // dblVal / pbVal
	dtype  int8     // JWRead が書き込む型。-2 はエラー
	_      [7]byte
}

// TagType タグの種類（JWGetTagIDS2 の kata / JWRead の dtype）
type TagType int

const (
	TagTypeError   = -2 // JWRead が返すエラー
	TagTypeUnknown = -1
	TagTypeFloat   = 1
	TagTypeBool    = 4
	TagTypeString  = 5
	TagTypeUShort  = 6
	TagTypeLString = 8
)

const (
	tagNameSize = 256 // JWGetTagIDS2 に渡すタグ名 1 件のサイズ
	stringSize  = 16  // 文字列タグの値のサイズ（pbVal[16]）
	noOffset    = ^uintptr(0)
)

// DLL の戻り値（実機で確認したもの）
const (
	rcOK           = 0
	rcNotConnected = -1    // ConnectNet していない
	rcInvalid      = -1003 // ID が不正、または JoyWSrv2 が停止している
)

func dllError(fn string, rc int32) error {
	switch rc {
	case rcNotConnected:
		return fmt.Errorf("%s: not connected (rc=%d)", fn, rc)
	case rcInvalid:
		return fmt.Errorf("%s: invalid tag id or JoyWatcher server is down (rc=%d)", fn, rc)
	default:
		return fmt.Errorf("%s: error (rc=%d)", fn, rc)
	}
}

// Tag タグ情報
type Tag struct {
	ID      int32
	Name    string
	TagType TagType
}

// TagMap タグのキャッシュを格納
type TagMap map[string]Tag

var (
	dll                = syscall.NewLazyDLL("JoyWaApi.dll")
	connectNet         = dll.NewProc("ConnectNet")
	jWGetTagIDS2       = dll.NewProc("JWGetTagIDS2")
	jWRead             = dll.NewProc("JWRead")
	jWWrite            = dll.NewProc("JWWrite")
	disconnectNet      = dll.NewProc("DisconnectNet")
	disconnectNetForce = dll.NewProc("DisconnectNetForce")
	startServer        = dll.NewProc("JWStartServer")
	procs              = []*syscall.LazyProc{connectNet, jWGetTagIDS2, jWRead, jWWrite, disconnectNet, disconnectNetForce, startServer}
)

// dllMu DLL の呼び出しを直列化する
var dllMu sync.Mutex

// Load DLL と各関数を読み込めるか確認する（LazyDLL が最初の呼び出しで panic するのを防ぐ）
func Load() error {
	if err := dll.Load(); err != nil {
		return err
	}
	for _, p := range procs {
		if err := p.Find(); err != nil {
			return err
		}
	}
	return nil
}

// Handle JoyWaAPIHandle JoyWatcher Dll
type Handle struct {
	mu        sync.Mutex // tagCache と connCount・holdCount を守る
	tagCache  TagMap
	connCount int // ConnectNet した回数（DLL は参照カウント方式）
	holdCount int // そのうちサーバー自身が保持している数（Hold）
}

func FromCP932(str []byte) (ret string, err error) {
	v := bytes.Split(str, []byte("\x00"))
	if len(v) == 0 {
		ret = ""
		return
	}

	r, _, err := transform.Bytes(japanese.ShiftJIS.NewDecoder(), v[0])
	if err != nil {
		return
	}

	ret = string(r)
	return
}

func toCP932(str string) (ret string, err error) {
	ret, _, err = transform.String(japanese.ShiftJIS.NewEncoder(), str)
	return
}

func Float64fromBytes(bytes []byte) float64 {
	bits := binary.LittleEndian.Uint64(bytes)
	float := math.Float64frombits(bits)
	return float
}

func float64Bytes(v float64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, math.Float64bits(v))
	return b
}
