// Package e2e は joyWaApi サーバーの実機回帰テスト。
//
// JoyWatcher（JoyWSrv2）と PLC が動いている Windows で実行する。
// joyWaApi.exe をビルドして起動し、gRPC と GraphQL で読み書きした結果を、
// plccmd の mcprotocol で PLC から直接読んだ値と照合する。
// テストで書き込むデバイスは開始時に退避し、終了時に PLC へ直接書き戻す。
//
// 実行方法は README.md を参照。
package e2e
