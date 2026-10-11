//go:build server
// +build server

package core

import (
	"fmt"
	"log"
	"strconv"
	"unsafe"
)

// emptyPassword JWRead/JWWrite に渡すパスワード（空文字列）
var emptyPassword = [1]byte{0}

func New() *Handle {
	var handle Handle
	handle.tagCache = make(map[string]Tag)
	return &handle
}

// Connect JoyWatcher Server
func (h *Handle) Connect() error {
	log.Printf("trace: joyWaApi:Connect")

	h.mu.Lock()
	defer h.mu.Unlock()

	dllMu.Lock()
	r1, r2, err := connectNet.Call()
	dllMu.Unlock()
	log.Printf("trace: joyWaApi:Connect: r1:%v r2:%v err: %v", r1, r2, err)

	if r1 == 0 {
		return fmt.Errorf("joyWaApi:Connect error")
	}
	h.connCount++
	log.Printf("trace: joyWaApi:Connect: connCount=%v", h.connCount)
	return nil
}

func (h *Handle) StartServer() error {
	log.Printf("trace: joyWaApi:StartServer")

	dllMu.Lock()
	r1, r2, err := startServer.Call()
	dllMu.Unlock()
	log.Printf("trace: joyWaApi:startServer: r1:%v r2:%v err: %v", r1, r2, err)

	if r1 != 1 {
		return fmt.Errorf("joyWaApi:startserver error")
	}
	return nil
}

// Ping
func (h *Handle) Ping() (bool, error) {
	log.Println("trace: Ping")

	if _, err := read(0); err != nil {
		return false, err
	}
	return true, nil
}

// GetTagID タグ名からタグのIDを取得する
func (h *Handle) GetTagID(tagname string) (id int32, tagType TagType, err error) {
	log.Println("trace: GetTagID")
	log.Printf("trace: -- tagname=%q", tagname)

	name, err := toCP932(tagname)
	if err != nil {
		return -1, TagTypeUnknown, err
	}
	if len(name) >= tagNameSize {
		return -1, TagTypeUnknown, fmt.Errorf("tag name too long(%v)", tagname)
	}
	var src [tagNameSize]byte
	copy(src[:], name)

	// 未接続のときなど DLL が何も書かない場合に備えて、見つからない状態で初期化する
	td := TcomData{colID: -1, dtype: TagTypeUnknown}

	dllMu.Lock()
	// JWGetTagIDS2 の戻り値は、名前が見つからなくても 1 なので見ない
	_, _, _ = jWGetTagIDS2.Call(
		1,                                /*nTag*/
		uintptr(unsafe.Pointer(&src[0])), /*psrc*/
		tagNameSize,                      /*src_size*/
		0,                                /*name_offs*/
		uintptr(unsafe.Pointer(&td)),     /*pdes*/
		unsafe.Sizeof(td),                /*des_size*/
		unsafe.Offsetof(td.colID),        /*id_offs*/
		unsafe.Offsetof(td.dtype),        /*kata_offs*/
		noOffset)                         /*len_offs: 書き込まない*/
	dllMu.Unlock()

	log.Printf("trace: -- col_id=%v dtype=%v", td.colID, td.dtype)
	if td.colID < 0 {
		return -1, TagTypeUnknown, fmt.Errorf("not found TAG(%v)", tagname)
	}
	return td.colID, TagType(td.dtype), nil
}

// ReadS タグ名指定にてタグの値を読み込み  string
func (h *Handle) ReadS(tagname string) (string, error) {
	log.Println("trace: ReadS")

	val, tType, err := h.Read(tagname)
	if err != nil {
		return "", err
	}
	switch tType {
	case TagTypeString, TagTypeLString:
		return FromCP932(val)
	case TagTypeBool:
		return strconv.FormatBool(val[0] != 0), nil
	default:
		return strconv.FormatFloat(Float64fromBytes(val), 'f', -1, 64), nil
	}
}

// ReadF タグ名指定にてタグの値を読み込み  float
func (h *Handle) ReadF(tagname string) (float64, error) {
	log.Println("trace: ReadF")

	val, tType, err := h.Read(tagname)
	if err != nil {
		return -1, err
	}
	switch tType {
	case TagTypeString, TagTypeLString:
		return -1, fmt.Errorf("TAG(%v) is string", tagname)
	case TagTypeBool:
		if val[0] != 0 {
			return 1, nil
		}
		return 0, nil
	default:
		return Float64fromBytes(val), nil
	}
}

// ReadB タグ名指定にてタグの値を読み込み bool
func (h *Handle) ReadB(tagname string) (bool, error) {
	log.Println("trace: ReadB")

	val, tType, err := h.Read(tagname)
	if err != nil {
		return false, err
	}
	switch tType {
	case TagTypeString, TagTypeLString:
		return false, fmt.Errorf("TAG(%v) is string", tagname)
	case TagTypeBool:
		return val[0] != 0, nil
	default:
		return Float64fromBytes(val) != 0, nil
	}
}

// GetTagCache Tagをキャッシュから取得します。存在しない場合は、作成します。
func (h *Handle) GetTagCache(tagname string) (*Tag, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	t, ok := h.tagCache[tagname]
	if !ok {
		id, tagType, err := h.GetTagID(tagname)
		if err != nil {
			return nil, err
		}
		t = Tag{ID: id, Name: tagname, TagType: tagType}
		h.tagCache[tagname] = t
	}

	// Create a new Tag to avoid returning a pointer to a map value
	result := t
	return &result, nil
}

// forgetTag 読み書きに失敗したタグをキャッシュから消す（次回は ID を取り直す）
func (h *Handle) forgetTag(tagname string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.tagCache, tagname)
}

// read タグのIDを指定して値を読み込む
// ID は JWGetTagIDS2 で取得したものだけを渡すこと（不正な ID を読むと JoyWSrv2 が落ちる）
func read(id int32) (td *TcomData, err error) {
	log.Println("trace: read")
	defer func() { recordResult(err) }()

	td = &TcomData{colID: id, dtype: TagTypeUnknown}

	dllMu.Lock()
	r0, _, _ := jWRead.Call(
		0, /*uid*/
		uintptr(unsafe.Pointer(&emptyPassword[0])), /*pwd*/
		1, /*nTag*/
		uintptr(unsafe.Pointer(td)) /*pDat*/)
	dllMu.Unlock()

	rc := int32(r0)
	log.Printf("trace: -- read: id=%v rc=%v dtype=%v val=%x", id, rc, td.dtype, td.colVal)

	if rc != rcOK {
		return nil, dllError("JWRead", rc)
	}
	if td.dtype == TagTypeError {
		return nil, fmt.Errorf("JWRead: error (id=%v dtype=%v)", id, td.dtype)
	}
	return td, nil
}

// Read タグ名を指定して値を読み込み
func (h *Handle) Read(tagname string) (ret []byte, tagType TagType, err error) {
	log.Println("trace: Read")

	t, err := h.GetTagCache(tagname)
	if err != nil {
		return
	}

	td, err := read(t.ID)
	if err != nil {
		h.forgetTag(tagname)
		err = fmt.Errorf("TAG(%v): %w", tagname, err)
		return
	}
	ret = td.colVal[:]
	tagType = TagType(td.dtype)
	return
}

// WriteFloat タグ名を指定して数値を書き込み
// BIT タグも double で書き込む（DLL は BIT の値も double として扱う。0 以外が ON）
func (h *Handle) WriteFloat(tagname string, value float64) error {
	return h.writeTag(tagname, float64Bytes(value))
}

// WriteString タグ名を指定して文字列を書き込み
func (h *Handle) WriteString(tagname string, value string) error {
	data, err := stringBytes(value)
	if err != nil {
		return err
	}
	return h.writeTag(tagname, data)
}

// Write data Automatic Type
func (h *Handle) Write(tm string, value string) (err error) {
	log.Println("trace: Handle.Write")
	log.Printf("trace: -- tag=%q value=%q", tm, value)

	t, err := h.GetTagCache(tm)
	if err != nil {
		return
	}

	var data []byte
	switch t.TagType {
	case TagTypeString, TagTypeLString:
		log.Println("trace: --- TagTypeString")
		if data, err = stringBytes(value); err != nil {
			return
		}
	case TagTypeBool:
		log.Println("trace: --- TagTypeBool")
		var v float64
		if value == "true" || value == "1" {
			v = 1
		}
		data = float64Bytes(v)
	default:
		log.Println("trace: --- TagTypeFloat")
		var fv float64
		fv, err = strconv.ParseFloat(value, 64)
		if err != nil {
			return
		}
		data = float64Bytes(fv)
	}
	return h.writeID(t, data)
}

func (h *Handle) writeTag(tagname string, data []byte) error {
	t, err := h.GetTagCache(tagname)
	if err != nil {
		return err
	}
	return h.writeID(t, data)
}

func (h *Handle) writeID(t *Tag, data []byte) error {
	if err := write(t.ID, data); err != nil {
		h.forgetTag(t.Name)
		return fmt.Errorf("TAG(%v): %w", t.Name, err)
	}
	return nil
}

// stringBytes 文字列タグに書き込むデータ（CP932、16 バイト固定、NUL 埋め）
func stringBytes(value string) ([]byte, error) {
	s, err := toCP932(value)
	if err != nil {
		return nil, err
	}
	if len(s) > stringSize {
		return nil, fmt.Errorf("string too long(%d bytes > %d): %q", len(s), stringSize, value)
	}
	data := make([]byte, stringSize)
	copy(data, s)
	return data, nil
}

// write タグのIDを指定して書き込みます
func write(tagID int32, data []byte) (err error) {
	log.Println("trace: core.write")
	defer func() { recordResult(err) }()
	log.Printf("trace: -- tagID=%v data=%x", tagID, data)

	ids := [1]int32{tagID}

	dllMu.Lock()
	r0, _, _ := jWWrite.Call(
		0, /*uid*/
		uintptr(unsafe.Pointer(&emptyPassword[0])), /*pwd*/
		0,                                //　常に０
		1,                                // 書き込みタグ数
		uintptr(len(data)),               // PACK したデータバイト数（DLL は見ていない）
		uintptr(unsafe.Pointer(&ids[0])), //書き込みタグを並べた配列の先頭アドレス
		uintptr(unsafe.Pointer(&data[0])) /*pDat*/)
	dllMu.Unlock()

	rc := int32(r0)
	log.Printf("trace: -- rc=%v", rc)
	if rc != rcOK {
		return dllError("JWWrite", rc)
	}

	return nil
}

// Hold サーバー自身が保持する接続を作る。この分はクライアントの DisConnect では切断しない
func (h *Handle) Hold() error {
	if err := h.Connect(); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.holdCount++
	return nil
}

// Release Hold した接続を切断する
func (h *Handle) Release() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.holdCount == 0 {
		return nil
	}
	h.holdCount--
	h.disconnectLocked()
	return nil
}

// DisConnect JoyWatcherのサーバから切断する
// Connect した回数より多くは DisconnectNet を呼ばない。Hold した接続も残す（他のクライアントの接続まで切れるため）
func (h *Handle) DisConnect() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.connCount <= h.holdCount {
		log.Printf("warning: joyWaApi:DisConnect: not connected (connCount=%v)", h.connCount)
		return nil
	}
	h.disconnectLocked()
	return nil
}

func (h *Handle) disconnectLocked() {
	if h.connCount == 0 {
		return
	}
	dllMu.Lock()
	disconnectNet.Call()
	dllMu.Unlock()
	h.connCount--
	log.Printf("trace: joyWaApi:DisConnect: connCount=%v", h.connCount)
}

// DisConnectForce JoyWatcherのサーバから切断する
func (h *Handle) DisConnectForce() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	dllMu.Lock()
	disconnectNetForce.Call()
	dllMu.Unlock()
	h.connCount = 0
	h.holdCount = 0

	return nil
}
