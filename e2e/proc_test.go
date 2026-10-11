package e2e

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// procStat プロセスのリソース使用量
type procStat struct {
	Handles int
	Threads int
	Private int64 // プライベートメモリ（バイト）
}

func (s procStat) String() string {
	return fmt.Sprintf("handles=%d threads=%d private=%.1fMB", s.Handles, s.Threads, float64(s.Private)/1e6)
}

func powershell(script string) (string, error) {
	b, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	return strings.TrimSpace(string(b)), err
}

func processStat(pid int) (procStat, error) {
	out, err := powershell(fmt.Sprintf(
		`$p = Get-Process -Id %d -ErrorAction Stop; '{0} {1} {2}' -f $p.HandleCount, $p.Threads.Count, $p.PrivateMemorySize64`, pid))
	if err != nil {
		return procStat{}, fmt.Errorf("process %d: %w", pid, err)
	}
	f := strings.Fields(out)
	if len(f) != 3 {
		return procStat{}, fmt.Errorf("unexpected output: %q", out)
	}
	var s procStat
	s.Handles, _ = strconv.Atoi(f[0])
	s.Threads, _ = strconv.Atoi(f[1])
	s.Private, _ = strconv.ParseInt(f[2], 10, 64)
	return s, nil
}

func processExists(pid int) bool {
	_, err := processStat(pid)
	return err == nil
}

// pidOf プロセス名から PID を返す（見つからなければ 0）
func pidOf(name string) int {
	out, err := powershell(fmt.Sprintf(`(Get-Process -Name %s -ErrorAction SilentlyContinue | Select-Object -First 1).Id`, name))
	if err != nil || out == "" {
		return 0
	}
	pid, _ := strconv.Atoi(out)
	return pid
}

// tcpConns addr（サーバー側）に張られている TCP 接続の数（状態ごと）
func tcpConns(addr string) (map[string]int, error) {
	b, err := exec.Command("netstat", "-ano", "-p", "tcp").Output()
	if err != nil {
		return nil, err
	}
	ret := map[string]int{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		// プロトコル ローカル 外部 状態 PID
		if len(f) == 5 && f[1] == addr && f[3] != "LISTENING" {
			ret[f[3]]++
		}
	}
	return ret, nil
}
