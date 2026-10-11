package e2e

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"
)

// errList 複数の goroutine から集めるエラー
type errList struct {
	mu   sync.Mutex
	errs []string
}

func (l *errList) add(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errs = append(l.errs, fmt.Sprintf(format, args...))
}

func (l *errList) report(t *testing.T) {
	t.Helper()
	for i, e := range l.errs {
		if i == 20 {
			t.Errorf("... and %d more errors", len(l.errs)-20)
			break
		}
		t.Error(e)
	}
}

// sharedTags 同時読み込みに使うタグ（読み込みのみ）
func sharedTags(n int) []string {
	var ret []string
	for _, tag := range tags.All {
		if len(ret) == n {
			break
		}
		ret = append(ret, tag.Full())
	}
	return ret
}

// TestConcurrentClients 複数のクライアントが同時に Open → Write → Read → Close を繰り返す
// クライアントごとに別のタグへ書き込み、最後に書いた値が PLC に入っていることを確認する
func TestConcurrentClients(t *testing.T) {
	const loops = 30
	clients := append(append([]Tag{}, fx.short01...), fx.short1...) // 16 クライアント
	shared := sharedTags(50)
	var errs errList
	var wg sync.WaitGroup
	for ci, tag := range clients {
		wg.Add(1)
		go func(ci int, tag Tag) {
			defer wg.Done()
			for i := 1; i <= loops; i++ {
				val := strconv.Itoa(ci*100 + i)
				r := repo()
				if err := r.Open(); err != nil {
					errs.add("client %d open: %v", ci, err)
					continue
				}
				if err := r.Write([]string{tag.Full()}, []string{val}); err != nil {
					errs.add("client %d write: %v", ci, err)
				}
				vals, err := r.Read(append([]string{tag.Full()}, shared...))
				if err != nil {
					errs.add("client %d read: %v", ci, err)
				} else if len(vals) != len(shared)+1 {
					errs.add("client %d read: %d values", ci, len(vals))
				}
				if err := r.Close(); err != nil {
					errs.add("client %d close: %v", ci, err)
				}
			}
		}(ci, tag)
	}
	wg.Wait()
	errs.report(t)

	for ci, tag := range clients {
		want := float64(ci*100+loops) / tag.PA
		waitPLC(t, tag, shortRaw(int16(want+0.5)))
	}
	assertConnCount(t, 1)
}

// TestConcurrentSameTag 複数のクライアントが同じタグに同時に書き込み・読み込みする
func TestConcurrentSameTag(t *testing.T) {
	tag := fx.short1[7]
	const writers, loops = 6, 20
	var errs errList
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(2)
		go func(w int) { // 書き込み
			defer wg.Done()
			for i := 0; i < loops; i++ {
				if err := write([]string{tag.Full()}, []string{strconv.Itoa(w*1000 + i)}); err != nil {
					errs.add("writer %d: %v", w, err)
				}
			}
		}(w)
		go func(w int) { // 読み込み
			defer wg.Done()
			for i := 0; i < loops; i++ {
				v, err := read(tag.Full())
				if err != nil {
					errs.add("reader %d: %v", w, err)
					continue
				}
				if _, err := strconv.ParseFloat(v[0], 64); err != nil {
					errs.add("reader %d: bad value %q", w, v[0])
				}
			}
		}(w)
	}
	wg.Wait()
	errs.report(t)

	// 最後の値は、どれかのクライアントが最後に書いた値
	finals := map[int16]bool{}
	for w := 0; w < writers; w++ {
		finals[int16(w*1000+loops-1)] = true
	}
	eventually(t, 5*time.Second, func() error {
		r, err := plc.Read(tag)
		if err != nil {
			return err
		}
		if !finals[int16(r.Words[0])] {
			return fmt.Errorf("final value %d is not a last value of any writer", int16(r.Words[0]))
		}
		return nil
	})
	assertConnCount(t, 1)
}

// TestConcurrentMixed 数値・BIT・文字列の書き込みと、多数のタグの読み込みを同時に行う
func TestConcurrentMixed(t *testing.T) {
	const loops = 15
	var errs errList
	var wg sync.WaitGroup
	run := func(name string, fn func(i int) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < loops; i++ {
				if err := fn(i); err != nil {
					errs.add("%s #%d: %v", name, i, err)
				}
			}
		}()
	}
	all := names(tags.All...)
	run("bits", func(i int) error {
		return write(names(fx.bits[:4]...), []string{strconv.FormatBool(i%2 == 0), "true", "false", strconv.FormatBool(i%3 == 0)})
	})
	run("string", func(i int) error { return write(names(fx.str), []string{fmt.Sprintf("LOT%03d", i)}) })
	run("long", func(i int) error { return write(names(fx.long), []string{strconv.Itoa(-100000 - i)}) })
	run("numeric", func(i int) error {
		return write(names(fx.short01[:4]...), []string{strconv.Itoa(i), "1.5", "-2.5", "3"})
	})
	for r := 0; r < 3; r++ {
		run(fmt.Sprintf("read-all-%d", r), func(int) error {
			v, err := read(all...)
			if err == nil && len(v) != len(all) {
				err = fmt.Errorf("%d values, want %d", len(v), len(all))
			}
			return err
		})
	}
	wg.Wait()
	errs.report(t)

	waitRead(t, fx.str, fmt.Sprintf("LOT%03d", loops-1))
	waitRead(t, fx.long, strconv.Itoa(-100000-(loops-1)))
	assertConnCount(t, 1)
}

// assertConnCount すべてのクライアントが切断した後、サーバーの接続数が want に戻っていること
func assertConnCount(t *testing.T, want int) {
	t.Helper()
	eventually(t, 5*time.Second, func() error {
		n, err := srv.lastConnCount()
		if err != nil {
			return err
		}
		if n != want {
			return fmt.Errorf("connCount = %d, want %d", n, want)
		}
		return nil
	})
}
