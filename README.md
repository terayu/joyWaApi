# joyWaApi

JoyWatcher と同じ PC で動かすサーバーです。JoyWaApi.dll（32bit）を呼び、gRPC と GraphQL（`-gqladdr` を指定したとき）でタグの読み書きを受け付けます。

```
クライアント ──gRPC(TCP)──────▶ joyWaApi.exe ──JoyWaApi.dll──▶ JoyWatcher(JoyWSrv2) ──▶ PLC
クライアント ──GraphQL(HTTP)──▶      （GraphQL は内部で gRPC と同じ処理を呼ぶ）
```

## 目次

- [ビルド](#ビルド)
- [起動（コマンド）](#起動コマンド)
- [gRPC の使い方](#grpc-の使い方)
- [GraphQL の使い方](#graphql-の使い方)
- [構成](#構成)
- [テスト](#テスト)

## ビルド

`build.bat` → `bin\joyWaApi.exe`。**32bit（GOARCH=386）必須**です（DLL が 32bit のため）。gcc は不要です（CGO_ENABLED=0）。Go 1.26 以上が必要です。

`task build`（または `make build`）でも同じ `bin\joyWaApi.exe` を作れます。Mac などでも Windows の 32bit 向けにクロスビルドします。

実行する PC には JoyWatcher（JoyWSrv2）が動いていて、`JoyWaApi.dll`（32bit）が読み込めること（例: `C:\Windows\SysWOW64\JoyWaApi.dll`）が必要です。

## 起動（コマンド）

```
joyWaApi.exe [全体オプション] server [serverオプション]
```

サブコマンドは `server` だけです。全体オプションは `server` の**前**に書きます。

```powershell
# gRPC だけ（127.0.0.1:9999）
.\bin\joyWaApi.exe server

# gRPC を全インターフェースで待ち受け、GraphQL も有効にする（ループバック以外ならトークン必須）
.\bin\joyWaApi.exe -loglevel debug server -rpcaddr 0.0.0.0:9999 -gqladdr 0.0.0.0:8080 -gqltoken s3cret

# 手元の動作確認（GraphQL をループバックだけで開き、Playground を表示）
.\bin\joyWaApi.exe server -gqladdr 127.0.0.1:8080 -gqlplayground
```

### 全体オプション

| オプション | 既定値 | 意味 |
|---|---|---|
| `-loglevel` | `info` | ログレベル（`trace` `debug` `info` `warning` `error` `alert` `panic`）。ログは標準エラー出力 |

### server オプション

| オプション | 既定値 | 意味 |
|---|---|---|
| `-rpcaddr` | `127.0.0.1:9999` | gRPC の待ち受け `IP:Port` |
| `-failtimeout` | `30` | JoyWatcher との読み書きがこの秒数失敗し続けたらプロセスを終了する（`0` で無効） |
| `-watchinterval` | `5` | JoyWatcher との接続を確認する間隔（秒） |
| `-gqladdr` | （空） | GraphQL の待ち受け `IP:Port`。空なら GraphQL は起動しない |
| `-gqltoken` | （空） | GraphQL のアクセストークン（`Authorization: Bearer <token>`）。ループバック以外で待ち受けるときは必須 |
| `-gqlinsecure` | `false` | トークンなしでループバック以外での待ち受けを許す |
| `-gqlmaxtags` | `1000` | GraphQL の 1 回の要求で扱えるタグ数の上限（`0` で無制限） |
| `-gqlplayground` | `false` | `/` で GraphQL Playground を表示する |

### 環境変数

オプションは環境変数でも指定できます（コマンドラインのオプションが優先）。

| 対象 | 環境変数 |
|---|---|
| 全体オプション | `JOYWAAPI_` + 名前の大文字（例 `JOYWAAPI_LOGLEVEL=debug`） |
| server オプション | `JOYWAAPI_SERVER_` + 名前の大文字（例 `JOYWAAPI_SERVER_RPCADDR=0.0.0.0:9999`、`JOYWAAPI_SERVER_GQLTOKEN=s3cret`） |

### 動作の注意

- 起動時にサーバー自身が JoyWatcher へ接続を 1 つ保持します。クライアントが `RDisConnect` しても、この接続は切れません。
- JoyWSrv2 を再起動すると、DLL の接続がこのプロセスでは回復しないことがあります。そのため読み書きが `-failtimeout` 秒失敗し続けるとプロセスを終了します。Windows サービスやタスクスケジューラなどで自動的に再起動する構成にしてください。
- `Ctrl+C`（SIGINT）/ SIGTERM で、gRPC と GraphQL を止めて接続を解放してから終了します。
- gRPC には TLS も認証もありません。信頼できるネットワークの中だけで使うか、`-rpcaddr` を `127.0.0.1` のままにしてください。

## gRPC の使い方

定義は [`rpc/joyWaApi.proto`](rpc/joyWaApi.proto)（パッケージ `rpc`、サービス `JoyWaApi`）です。gRPC のリフレクションは有効にしていないので、`grpcurl` などでは `-proto` を指定します。

### メソッド

| メソッド | リクエスト | レスポンス | 内容 |
|---|---|---|---|
| `RPing` | `RequestType` | `BoolMessage` | JoyWatcher と読み書きできるか。失敗しても gRPC のエラーにはならず `Val=false` を返す |
| `RConnect` | `RequestType` | `NoReturnMessage` | JoyWatcher へ接続（参照カウントを +1） |
| `RDisConnect` | `RequestType` | `NoReturnMessage` | 切断（参照カウントを −1）。`RConnect` した回数より多くは切断しない |
| `RDisConnectForce` | `RequestType` | `NoReturnMessage` | 参照カウントに関係なく強制切断（他のクライアントの接続も切れる） |
| `RStartServer` | `RequestType` | `NoReturnMessage` | JoyWSrv2 の起動を要求する |
| `RRead` | `TagsMessage` | `ValsMessage` | 値を文字列で読む。数値は `'f'` 形式（余分な 0 なし）、BIT は `true`/`false`、文字列は CP932 から変換 |
| `RReadF` | `TagsMessage` | `ValsMessage` | 値を数値で読む（小数点以下 4 桁の文字列で返す）。文字列タグはエラー |
| `RReadB` | `TagBitMessage` | `BitMessage` | 値を BIT（bool）で読む。0 以外が `true`。文字列タグはエラー。`Vals`/`Pulse` は不要（`Tags` だけ指定） |
| `RWrite` | `TagValsMessage` | `NoReturnMessage` | **タグの種類に合わせて**書き込む（文字列タグ→文字列、BIT→`"true"`/`"1"` が ON、それ以外→数値） |
| `RWriteS` | `TagValsMessage` | `NoReturnMessage` | 文字列として書き込む |
| `RWriteF` | `TagValsMessage` | `NoReturnMessage` | 数値として書き込む（`Vals` は数値の文字列） |
| `RWriteB` | `TagBitMessage` | `NoReturnMessage` | BIT を書き込む。`Pulse` に秒数を指定すると、その秒数後に OFF に戻す |

### メッセージ

| メッセージ | フィールド |
|---|---|
| `TagsMessage` | `Tags`（string の配列） |
| `ValsMessage` | `Vals`（string の配列。`Tags` と同じ順） |
| `TagValsMessage` | `Tags`、`Vals`（どちらも string の配列。**同じ長さ**） |
| `TagBitMessage` | `Tags`、`Vals`（bool の配列）、`Pulse`（int32 の配列。省略可。指定するなら `Tags` と同じ長さ。単位は秒、`0` は戻さない） |
| `BitMessage` | `Vals`（bool の配列） |
| `BoolMessage` | `Val`（bool） |

### 共通のルール

- **タグ名**は JoyWatcher のタグ名です（例: `LOCAL$ALM.EMG$VALUE`。タグリストのシート名と NAME 列から作る）。
- 複数タグは、先頭から順に 1 つずつ処理します。途中でエラーになるとそこで止まり、**それ以前のタグへの書き込みは取り消されません**。
- `Tags` と `Vals`（`Pulse`）の数が違うと `InvalidArgument` を返します。存在しないタグや書き込めない値（数値に変換できない、など）はエラーになりますが、サーバーは落ちません。
- 文字列タグは CP932 で **16 バイトまで**です。超えるとエラーになります。
- 一般的な使い方は、利用前に `RConnect`、終了時に `RDisConnect` を呼びます（読み書きだけなら、サーバーが保持する接続を使えます）。

### grpcurl の例

```sh
# 疎通確認
grpcurl -plaintext -import-path rpc -proto joyWaApi.proto \
  -d '{}' 127.0.0.1:9999 rpc.JoyWaApi/RPing

# 読み込み（文字列）
grpcurl -plaintext -import-path rpc -proto joyWaApi.proto \
  -d '{"Tags":["LOCAL$ALM.EMG$VALUE","LOCAL$SV1.TEMP$VALUE"]}' \
  127.0.0.1:9999 rpc.JoyWaApi/RRead

# 書き込み（種類に合わせて）
grpcurl -plaintext -import-path rpc -proto joyWaApi.proto \
  -d '{"Tags":["LOCAL$SV1.TEMP$VALUE"],"Vals":["12.5"]}' \
  127.0.0.1:9999 rpc.JoyWaApi/RWrite

# BIT を ON にして 3 秒後に OFF に戻す
grpcurl -plaintext -import-path rpc -proto joyWaApi.proto \
  -d '{"Tags":["LOCAL$STA.RUN$VALUE"],"Vals":[true],"Pulse":[3]}' \
  127.0.0.1:9999 rpc.JoyWaApi/RWriteB
```

> タグ名の `$` はシェルに展開されないよう、シングルクォートで囲んでください。

### Go のクライアント例

生成コード（`github.com/terayu/joyWaApi/rpc`）をそのまま使えます。

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/terayu/joyWaApi/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	cn, err := grpc.Dial("127.0.0.1:9999", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer cn.Close()
	c := rpc.NewJoyWaApiClient(cn)
	ctx := context.Background()

	if _, err := c.RConnect(ctx, &rpc.RequestType{}); err != nil {
		log.Fatal(err)
	}
	defer c.RDisConnect(ctx, &rpc.RequestType{})

	// 読み込み
	r, err := c.RRead(ctx, &rpc.TagsMessage{Tags: []string{"LOCAL$ALM.EMG$VALUE"}})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(r.Vals)

	// 書き込み（BIT を ON、5 秒後に OFF）
	_, err = c.RWriteB(ctx, &rpc.TagBitMessage{
		Tags:  []string{"LOCAL$STA.RUN$VALUE"},
		Vals:  []bool{true},
		Pulse: []int32{5},
	})
	if err != nil {
		log.Fatal(err)
	}
}
```

他の言語では `rpc/joyWaApi.proto` から `protoc` でクライアントコードを生成してください。

### proto を変えたとき

`rpc/build_rpc.sh` が `protoc` を呼んで `joyWaApi.pb.go` と `joyWaApi_grpc.pb.go` を再生成します（`protoc`、`protoc-gen-go`、`protoc-gen-go-grpc` が必要）。

## GraphQL の使い方

`-gqladdr` を指定したときだけ起動します。スキーマは [`graph/schema.graphqls`](graph/schema.graphqls) です。処理の中身は gRPC と同じ（`rpc.Server`）なので、パルス・文字列長の確認・エラーの条件は gRPC と同じです。

| 項目 | 内容 |
|---|---|
| エンドポイント | `http://<gqladdr>/query`（POST / GET。Subscription は WebSocket） |
| Playground | `-gqlplayground` のとき `http://<gqladdr>/` |
| イントロスペクション | 有効 |
| 認証 | `-gqltoken` を指定したら `Authorization: Bearer <token>` が必要 |

### 認証と待ち受けアドレス

- `-gqladdr` がループバック（`127.0.0.1`、`localhost`、`[::1]`）なら、トークンなしで起動できます。
- ループバック以外（`0.0.0.0:8080` など。ホストを省略した `:8080` も該当）では、`-gqltoken` か `-gqlinsecure` がないと**起動に失敗します**。
- トークンが違うと HTTP 401 になります。
- WebSocket でヘッダーを付けられないクライアントは、`connection_init` の payload に `{"Authorization": "Bearer <token>"}` を入れます。

### Query（読み込み）

| フィールド | 戻り値 | 対応する gRPC | 内容 |
|---|---|---|---|
| `ping` | `Boolean!` | `RPing` | JoyWatcher と読み書きできるか |
| `read(tags: [String!]!)` | `[TagValue!]!` | `RRead` | 文字列で読む |
| `readFloat(tags: [String!]!)` | `[TagFloat!]!` | `RReadF` | 数値で読む（小数点以下 4 桁） |
| `readBit(tags: [String!]!)` | `[TagBit!]!` | `RReadB` | BIT で読む |

```graphql
query {
  ping
  read(tags: ["LOCAL$ALM.EMG$VALUE", "LOCAL$SV1.TEMP$VALUE"]) {
    tag
    value
  }
  readFloat(tags: ["LOCAL$SV1.TEMP$VALUE"]) { tag value }
  readBit(tags: ["LOCAL$STA.RUN$VALUE"]) { tag value }
}
```

### Mutation（書き込み）

戻り値はどれも `Boolean!`（成功なら `true`、失敗は `errors` に入る）です。

| フィールド | 入力 | 対応する gRPC | 内容 |
|---|---|---|---|
| `write(input: [TagValueInput!]!)` | `{tag, value: String}` | `RWrite` | タグの種類に合わせて書き込む |
| `writeString(input: [TagValueInput!]!)` | `{tag, value: String}` | `RWriteS` | 文字列として書き込む |
| `writeFloat(input: [TagFloatInput!]!)` | `{tag, value: Float}` | `RWriteF` | 数値として書き込む |
| `writeBit(input: [TagBitInput!]!)` | `{tag, value: Boolean, pulseSec: Int}` | `RWriteB` | BIT を書き込む。`pulseSec`（秒、`0` 以上）を指定すると、その秒数後に OFF に戻す |

```graphql
mutation {
  write(input: [{ tag: "LOCAL$SV1.TEMP$VALUE", value: "12.5" }])
  writeFloat(input: [{ tag: "LOCAL$SV1.TEMP$VALUE", value: 12.5 }])
  writeString(input: [{ tag: "LOCAL$SVL.SV_LOT$VALUE", value: "LOT-0001" }])
  # ON にして 3 秒後に OFF に戻す
  writeBit(input: [{ tag: "LOCAL$STA.RUN$VALUE", value: true, pulseSec: 3 }])
}
```

### Subscription（変化の監視）

`tagChanged(tags: [String!]!, intervalMs: Int = 1000)` は、`intervalMs` ごとに `tags` を読み、**値が変わったタグだけ**を配信します。

- 購読を始めた直後は、全タグの現在値を配信します。
- `intervalMs` は `100` 以上です（未満は `BAD_USER_INPUT`）。`tags` は 1 つ以上必要です。
- 読めなかった周期は飛ばします（購読は続きます）。
- 値は `read` と同じ文字列です。

```graphql
subscription {
  tagChanged(tags: ["LOCAL$ALM.EMG$VALUE", "LOCAL$SV1.TEMP$VALUE"], intervalMs: 500) {
    tag
    value
  }
}
```

WebSocket のサブプロトコルは `graphql-transport-ws` と `graphql-ws`（旧）に対応します。

### curl の例

```sh
# 読み込み
curl -s http://127.0.0.1:8080/query \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer s3cret' \
  -d '{"query":"query($t:[String!]!){ read(tags:$t){ tag value } }","variables":{"t":["LOCAL$SV1.TEMP$VALUE"]}}'

# 書き込み
curl -s http://127.0.0.1:8080/query \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer s3cret' \
  -d '{"query":"mutation{ writeBit(input:[{tag:\"LOCAL$STA.RUN$VALUE\",value:true,pulseSec:3}]) }"}'
```

### エラー

エラーは `errors[].extensions.code` で見分けられます。

| `code` | 意味 |
|---|---|
| `BAD_USER_INPUT` | 入力の誤り（タグ数が `-gqlmaxtags` を超えた、`tags` が空、`pulseSec` が負、`intervalMs` が小さい、タグ数と値の数の不一致など。gRPC の `InvalidArgument`） |
| `JOYWATCHER_ERROR` | JoyWatcher での読み書きの失敗（存在しないタグ、切断中、値が変換できない、文字列が長すぎる、など） |

### スキーマを変えたとき

`graph/schema.graphqls` を変えたら `make generate`（または `task generate`、`go tool gqlgen generate`）で `graph/generated.go` などを再生成します。

## 構成

| ディレクトリ | 内容 |
|---|---|
| `app` / `cmd` | エントリーポイントと `server` コマンド |
| `core` | JoyWaApi.dll の呼び出し（`-tags server` のときだけビルドされる） |
| `rpc` | gRPC の定義（`joyWaApi.proto`）と生成コード、gRPC サーバーの実装（`server.go`、`-tags server` のときだけビルドされる） |
| `graph` | GraphQL のスキーマ（`schema.graphqls`）と gqlgen の生成コード、HTTP サーバー（`server.go`）。スキーマを変えたら `go tool gqlgen generate`（`make generate`） |
| `e2e` | 実機（JoyWatcher + PLC）の回帰テスト。別モジュール。[e2e/README.md](e2e/README.md) |

## テスト

```powershell
$env:GOARCH = "386"; go test -tags server ./...   # 単体テスト（実機は不要）
cd e2e; $env:JOYWA_E2E = "1"; $env:GOARCH = "386"; go test -tags server -count=1 -timeout 30m -v .   # 実機テスト
```
