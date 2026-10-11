package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// 環境変数
//
//	JOYWA_E2E=1            テストを実行する（未設定ならすべてスキップ）
//	JOYWA_E2E_PLC          PLC のアドレス exp. 192.168.0.10:5000（UDP）。必須
//	JOYWA_E2E_TAGLIST      タグリストの xlsx のパス。必須
//	JOYWA_E2E_RESTART=1    JoyWSrv2 を停止・再起動するテストも実行する
//	JOYWA_E2E_JOYWSRV2     JoyWSrv2.exe のパス（未設定なら、動いているプロセスのパスを使う）
//	JOYWA_E2E_KEEP=1       成功しても作業ディレクトリ（ビルドした exe とサーバーのログ）を消さない
//
// 書き込むタグの指定は taglist_test.go の fixtureConfig を参照。
var (
	plcAddr     = os.Getenv("JOYWA_E2E_PLC")
	taglistPath = os.Getenv("JOYWA_E2E_TAGLIST")
	joywsrv2Exe = os.Getenv("JOYWA_E2E_JOYWSRV2")
	runRestart  = os.Getenv("JOYWA_E2E_RESTART") == "1"
)

// テスト全体で共有するもの（TestMain で用意する）
var (
	workDir string      // ビルドした exe やログを置くディレクトリ
	srv     *serverProc // テスト用に起動した joyWaApi サーバー
	tags    *TagList    // タグリスト
	plc     *PLC        // PLC 直接アクセス
	fx      fixture     // テストで使うタグ
)

func TestMain(m *testing.M) {
	if os.Getenv("JOYWA_E2E") != "1" {
		fmt.Println("skip e2e: set JOYWA_E2E=1 to run (requires JoyWatcher and PLC)")
		os.Exit(0)
	}
	os.Exit(run(m))
}

func run(m *testing.M) (code int) {
	var err error
	if workDir, err = os.MkdirTemp("", "joywa-e2e-"); err != nil {
		fmt.Println("e2e setup:", err)
		return 1
	}
	// 成功したら消す。失敗したときはログ（joyWaApi*.log）を見られるように残す
	defer func() {
		if code == 0 && os.Getenv("JOYWA_E2E_KEEP") != "1" {
			os.RemoveAll(workDir)
		} else {
			fmt.Println("e2e: logs are kept in", workDir)
		}
	}()
	fmt.Println("e2e workdir:", workDir)

	if err := setup(); err != nil {
		fmt.Println("e2e setup:", err)
		return 1
	}

	saved, err := plc.Snapshot(fx.writable())
	if err != nil {
		fmt.Println("e2e snapshot:", err)
		return 1
	}
	defer func() {
		// サーバーのパルス OFF などが終わるのを待ってから戻す
		time.Sleep(2 * time.Second)
		if err := plc.Restore(saved); err != nil {
			fmt.Println("e2e restore:", err)
		} else {
			fmt.Printf("e2e: restored %d tags\n", len(saved))
		}
	}()

	if srv, err = startServer(filepath.Join(workDir, "joyWaApi.exe"), filepath.Join(workDir, "joyWaApi.log")); err != nil {
		fmt.Println("e2e start server:", err)
		return 1
	}
	defer func() { srv.Stop() }() // TestJoyWatcherRestart で起動し直すことがあるので、終了時の srv を止める

	return m.Run()
}

func setup() error {
	if plcAddr == "" || taglistPath == "" {
		return fmt.Errorf("JOYWA_E2E_PLC and JOYWA_E2E_TAGLIST are required")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		return err
	}
	// joyWaApi は 32bit の DLL を呼ぶので GOARCH=386 でビルドする
	if err := goBuild(root, filepath.Join(workDir, "joyWaApi.exe"),
		[]string{"GOARCH=386", "CGO_ENABLED=0"}, "-tags", "timetzdata,server", "./app"); err != nil {
		return err
	}

	if tags, err = LoadTagList(taglistPath); err != nil {
		return err
	}
	if fx, err = newFixture(tags); err != nil {
		return err
	}
	plc = NewPLC(plcAddr)
	if _, err := plc.Read(fx.short01[0]); err != nil {
		return fmt.Errorf("PLC(%s) not reachable: %w", plcAddr, err)
	}
	return nil
}

func goBuild(dir, out string, env []string, args ...string) error {
	cmd := exec.Command("go", append([]string{"build", "-o", out}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build %s: %w\n%s", dir, err, b)
	}
	return nil
}
