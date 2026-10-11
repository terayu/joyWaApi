package e2e

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/terayu/plccmd/mcprotocol"
	"github.com/xuri/excelize/v2"
)

// タグの型（タグリストの TYPE 列 = JWGetTagIDS2 の kata）
const (
	typeShort  = 0
	typeLong   = 1
	typeBit    = 4
	typeString = 5
)

// Tag タグリストの1行
type Tag struct {
	Sheet  string
	Name   string
	Note   string
	Type   int
	Device string
	Dev    mcprotocol.DeviceCode
	DevNo  uint32
	PA, PB float64 // 工学値 = 生値 * PA + PB
	StrLen int     // 文字列の長さ（バイト）
	ColID  int32   // タグ ID
}

// Full JoyWatcher のタグ名（シート ALM・NAME EMG → LOCAL$ALM.EMG$VALUE）
func (t Tag) Full() string { return "LOCAL$" + t.Sheet + "." + t.Name + "$VALUE" }

func (t Tag) String() string { return t.Sheet + "." + t.Name + "(" + t.Device + ")" }

// words PLC 上のワード数（BIT は 0）
func (t Tag) words() int {
	switch t.Type {
	case typeShort:
		return 1
	case typeLong:
		return 2
	case typeString:
		return (t.StrLen + 1) / 2
	}
	return 0
}

// TagList タグリスト
type TagList struct {
	All    []Tag
	byName map[string]Tag // "SHEET.NAME"
}

func (l *TagList) Get(name string) (Tag, error) {
	t, ok := l.byName[name]
	if !ok {
		return Tag{}, fmt.Errorf("tag %q not in tag list", name)
	}
	return t, nil
}

// Reserved NOTE が「将来予約」のタグを、リストの順に n 個返す
func (l *TagList) Reserved(sheet string, typ int, n int) ([]Tag, error) {
	var ret []Tag
	for _, t := range l.All {
		if t.Sheet == sheet && t.Type == typ && t.Note == fixtureConfig.reservedNote {
			ret = append(ret, t)
			if len(ret) == n {
				return ret, nil
			}
		}
	}
	return nil, fmt.Errorf("not enough reserved tags in %s (type %d): %d < %d", sheet, typ, len(ret), n)
}

var deviceCodes = map[string]mcprotocol.DeviceCode{
	"M": mcprotocol.DeviceM,
	"L": mcprotocol.DeviceL,
	"D": mcprotocol.DeviceD,
	"R": mcprotocol.DeviceR,
}

// parseDevice "M12000" → (M, 12000)。10 進のデバイスだけ対応する（SM などは対象外）
func parseDevice(s string) (mcprotocol.DeviceCode, uint32, bool) {
	i := strings.IndexAny(s, "0123456789")
	if i <= 0 {
		return 0, 0, false
	}
	code, ok := deviceCodes[s[:i]]
	if !ok {
		return 0, 0, false
	}
	n, err := strconv.ParseUint(s[i:], 10, 32)
	if err != nil {
		return 0, 0, false
	}
	return code, uint32(n), true
}

// LoadTagList タグリストの xlsx を読む。PLC から直接読めないデバイスのタグは含めない
func LoadTagList(path string) (*TagList, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	l := &TagList{byName: map[string]Tag{}}
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		if err != nil {
			return nil, err
		}
		if len(rows) < 2 {
			continue
		}
		col := map[string]int{}
		for i, h := range rows[0] {
			col[h] = i
		}
		get := func(r []string, name string) string {
			if i, ok := col[name]; ok && i < len(r) {
				return r[i]
			}
			return ""
		}
		for _, r := range rows[1:] {
			t := Tag{Sheet: sheet, Name: get(r, "NAME"), Note: get(r, "NOTE"), Device: get(r, "DEVICENAME")}
			if t.Name == "" {
				continue
			}
			var ok bool
			if t.Dev, t.DevNo, ok = parseDevice(t.Device); !ok {
				continue
			}
			var e1, e2, e3, e4, e5 error
			t.Type, e1 = strconv.Atoi(get(r, "TYPE"))
			t.PA, e2 = strconv.ParseFloat(get(r, "P_A"), 64)
			t.PB, e3 = strconv.ParseFloat(get(r, "P_B"), 64)
			t.StrLen, e4 = strconv.Atoi(get(r, "STRLENGTH"))
			id, e5 := strconv.ParseInt(get(r, "COL_ID"), 10, 32)
			if err := firstErr(e1, e2, e3, e4, e5); err != nil {
				return nil, fmt.Errorf("%s %s: %w", sheet, t.Name, err)
			}
			t.ColID = int32(id)
			l.All = append(l.All, t)
			l.byName[sheet+"."+t.Name] = t
		}
	}
	if len(l.All) == 0 {
		return nil, fmt.Errorf("no tags in %s", path)
	}
	return l, nil
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// fixtureConfig テストで書き込むタグの指定。環境変数で上書きできる（既定は下の値）
//
//	JOYWA_E2E_RESERVED_NOTE  予約タグの NOTE 列の値
//	JOYWA_E2E_SHEET_SHORT01  short・P_A=0.1 の予約タグを探すシート
//	JOYWA_E2E_SHEET_SHORT1   short・P_A=1 の予約タグを探すシート
//	JOYWA_E2E_SHEET_BIT      BIT の予約タグを探すシート
//	JOYWA_E2E_TAG_LONG       long のタグ（シート.NAME）
//	JOYWA_E2E_TAG_STRING     文字列のタグ（シート.NAME。STRLENGTH は 12）
var fixtureConfig = struct {
	reservedNote                                         string
	sheetShort01, sheetShort1, sheetBit, tagLong, tagStr string
}{
	reservedNote: getenv("JOYWA_E2E_RESERVED_NOTE", "将来予約"),
	sheetShort01: getenv("JOYWA_E2E_SHEET_SHORT01", "SV1"),
	sheetShort1:  getenv("JOYWA_E2E_SHEET_SHORT1", "PVS"),
	sheetBit:     getenv("JOYWA_E2E_SHEET_BIT", "STA"),
	tagLong:      getenv("JOYWA_E2E_TAG_LONG", "SVD.SV_USR_ID"),
	tagStr:       getenv("JOYWA_E2E_TAG_STRING", "SVL.SV_LOT"),
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// fixture テストで書き込むタグ。主にタグリストで予約（fixtureConfig.reservedNote）のものを使う
type fixture struct {
	short01 []Tag // short・P_A=0.1（SV1 の将来予約）
	short1  []Tag // short・P_A=1（PVS の将来予約）
	bits    []Tag // BIT（STA の将来予約）
	long    Tag   // long（SVD.SV_USR_ID）
	str     Tag   // 文字列（SVL.SV_LOT、12 バイト）
}

func newFixture(l *TagList) (f fixture, err error) {
	if f.short01, err = l.Reserved(fixtureConfig.sheetShort01, typeShort, 8); err != nil {
		return
	}
	if f.short1, err = l.Reserved(fixtureConfig.sheetShort1, typeShort, 8); err != nil {
		return
	}
	if f.bits, err = l.Reserved(fixtureConfig.sheetBit, typeBit, 8); err != nil {
		return
	}
	if f.long, err = l.Get(fixtureConfig.tagLong); err != nil {
		return
	}
	if f.str, err = l.Get(fixtureConfig.tagStr); err != nil {
		return
	}
	for _, t := range f.short01 {
		if t.PA != 0.1 {
			return f, fmt.Errorf("%v: P_A=%v, want 0.1", t, t.PA)
		}
	}
	for _, t := range f.short1 {
		if t.PA != 1 {
			return f, fmt.Errorf("%v: P_A=%v, want 1", t, t.PA)
		}
	}
	if f.long.Type != typeLong || f.str.Type != typeString {
		return f, fmt.Errorf("unexpected type: %v=%d %v=%d", f.long, f.long.Type, f.str, f.str.Type)
	}
	if f.str.StrLen != 12 {
		return f, fmt.Errorf("%v: STRLENGTH=%d, want 12", f.str, f.str.StrLen)
	}
	return f, nil
}

// writable テストで書き込む可能性があるタグ（開始時に退避して、終了時に戻す）
func (f fixture) writable() []Tag {
	ts := append([]Tag{}, f.short01...)
	ts = append(ts, f.short1...)
	ts = append(ts, f.bits...)
	return append(ts, f.long, f.str)
}
