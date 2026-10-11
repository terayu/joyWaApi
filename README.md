# joyWaApi

JoyWatcher と同じ PC で動かすサーバーです。JoyWaApi.dll（32bit）を呼び、gRPC と GraphQL（`-gqladdr` を指定したとき）でタグの読み書きを受け付けます。

```
クライアント ──gRPC(TCP)──▶ joyWaApi.exe ──JoyWaApi.dll──▶ JoyWatcher(JoyWSrv2) ──▶ PLC
```

## ビルド

`build.bat` → `bin\joyWaApi.exe`。**32bit（GOARCH=386）必須**です（DLL が 32bit のため）。gcc は不要です（CGO_ENABLED=0）。Go 1.26 以上が必要です。

`task build`（または `make build`）でも同じ `bin\joyWaApi.exe` を作れます。Mac などでも Windows の 32bit 向けにクロスビルドします。

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
