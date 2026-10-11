# joyWaApi 実機回帰テスト

joyWaApi サーバーを実機（JoyWatcher + PLC）で動かして、gRPC・GraphQL の読み書き・同時アクセス・リソースの解放を確認するテストです。
読み書きした結果は、plccmd の `mcprotocol` で PLC から直接読んだ値と照合します。

## 前提

- Windows で JoyWatcher（JoyWSrv2）が動いていて、PLC に接続できること
- `JoyWaApi.dll`（32bit）が読み込めること（例: `C:\Windows\SysWOW64\JoyWaApi.dll`）
- タグリスト（xlsx）があること。シート名と NAME 列からタグ名を作る（シート `ALM`、NAME `EMG` → `LOCAL$ALM.EMG$VALUE`）。PLC のデバイスは DEVICENAME 列
- gcc は不要

## 実行

```sh
cd e2e
JOYWA_E2E=1 GOARCH=386 go test -tags server -count=1 -timeout 30m -v .
```

PowerShell の場合:

```powershell
cd e2e
$env:JOYWA_E2E = "1"; $env:GOARCH = "386"
go test -tags server -count=1 -timeout 30m -v .
```

- `-tags server` と `GOARCH=386` は `TestTagID`（テストのプロセスから DLL を直接呼ぶ）に必要です。`-tags server` を付けなければ `TestTagID` だけ実行されません。`-tags server` だけ付けて `GOARCH=386` を付けないと、32bit の DLL を読み込めず `TestTagID` は失敗します。
- テストは joyWaApi.exe を一時ディレクトリにビルドし、空いているポートで joyWaApi サーバーを起動します。常駐している joyWaApi には影響しません。
- 所要時間は約 2〜3 分です。

| 環境変数 | 意味 |
|---|---|
| `JOYWA_E2E=1` | テストを実行する（未設定ならすべてスキップ） |
| `JOYWA_E2E_PLC` | PLC のアドレス（UDP）。**必須**。例 `192.168.0.10:5000` |
| `JOYWA_E2E_TAGLIST` | タグリストの xlsx のパス。**必須** |
| `JOYWA_E2E_RESTART=1` | JoyWSrv2 を停止・再起動するテストも実行する（**JoyWatcher が十数秒止まります**） |
| `JOYWA_E2E_JOYWSRV2` | JoyWSrv2.exe のパス（`JOYWA_E2E_RESTART=1` のとき使う。未設定なら動いているプロセスのパス） |
| `JOYWA_E2E_KEEP=1` | 成功しても作業ディレクトリ（exe とサーバーのログ）を消さない。失敗したときは常に残す |

## 書き込むタグ

主にタグリストで NOTE が「将来予約」のタグに書き込みます。開始時に値を退避して、終了時に PLC へ直接書き戻します。
書き込むタグはタグリストの内容で決まります。タグリストが違う場合は、次の環境変数で指定します（既定値は下の表）。

| 用途 | 既定のタグ | 環境変数 |
|---|---|---|
| 予約タグの NOTE | `将来予約` | `JOYWA_E2E_RESERVED_NOTE` |
| short（P_A=0.1） | シート `SV1` の予約から 8 個 | `JOYWA_E2E_SHEET_SHORT01` |
| short（P_A=1） | シート `PVS` の予約から 8 個 | `JOYWA_E2E_SHEET_SHORT1` |
| BIT | シート `STA` の予約から 8 個 | `JOYWA_E2E_SHEET_BIT` |
| long | `SVD.SV_USR_ID` | `JOYWA_E2E_TAG_LONG` |
| 文字列（STRLENGTH 12） | `SVL.SV_LOT` | `JOYWA_E2E_TAG_STRING` |

タグリストは、シートに `NAME`・`NOTE`・`DEVICENAME`・`TYPE`・`P_A`・`P_B`・`STRLENGTH`・`COL_ID` の列があること。
short の予約タグの P_A は `SV1` 側が 0.1、`PVS` 側が 1 であること（違うと開始時にエラーになります）。

## テストの内容

| ファイル | テスト | 内容 |
|---|---|---|
| rw_test.go | TestPing | RPing が通る |
| | TestReadAllTags | タグリストの全タグを RRead で読み、PLC の値（生の値 × P_A + P_B）と比べる |
| | TestWriteNumeric / NumericRPC / Long | 数値の書き込み（スケール変換・四捨五入・範囲の端）を PLC で確認して読み戻す |
| | TestWriteBit / BitRPC / Pulse | BIT の書き込み、RReadB・RReadF、パルス（指定秒数後に OFF） |
| | TestReadFromPLC | PLC に直接書いた値を RRead で読む |
| | TestReadManyTagsAtOnce | 1 回の Read で全タグを読む |
| string_test.go | TestWriteString / StringRPC | 文字列（ASCII・半角カナ・全角・切り捨て・空）と、16 バイトを超えたときのエラー |
| error_test.go | TestInvalidArgument など | タグ数と値の数の不一致、存在しないタグ、変換できない値。どれもサーバーは落ちない |
| | TestDisconnectWithoutConnect | RConnect より多く RDisConnect しても、サーバーが保持する接続は残る |
| | TestDisconnectForce | 強制切断の後も接続し直せる（専用のサーバーで実行） |
| graphql_test.go | TestGraphQL | joyWaApi の GraphQL（`-gqladdr`）の Query・Mutation（パルスを含む）・Subscription（PLC に直接書いた変化が届く）、トークンの確認、エラーコード |
| concurrent_test.go | TestConcurrentClients | 16 クライアントが同時に接続・書き込み・読み込み・切断を繰り返す |
| | TestConcurrentSameTag | 同じタグへの同時書き込みと読み込み |
| | TestConcurrentMixed | 数値・BIT・文字列の書き込みと全タグの読み込みを同時に行う |
| resource_test.go | TestNoResourceLeak | 同じ負荷を 2 回かけて、joyWaApi のハンドル・スレッド・メモリ、JoyWSrv2 のハンドル、TCP 接続、DLL の接続数が増え続けないこと |
| restart_test.go | TestJoyWatcherRestart | JoyWSrv2 が止まると joyWaApi は `-failtimeout` 秒後に終了し、JoyWSrv2 の再起動後に起動した joyWaApi は読み書きできる（`JOYWA_E2E_RESTART=1`） |
| tagid_test.go | TestTagID | 全タグの ID と型が、タグリストの COL_ID・TYPE と一致する。未接続のときは ID を返さない |

## 失敗したとき

作業ディレクトリ（テストの最初に `e2e workdir:` と表示される）に、joyWaApi サーバーのログ（`joyWaApi*.log`、trace レベル）が残ります。
