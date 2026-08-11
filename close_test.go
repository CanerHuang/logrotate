package logrotate

import (
	"path/filepath"
	"sync"
	"testing"
)

// Close 必須是冪等的:第二次呼叫不能 panic(close of closed channel)。
func TestCloseIsIdempotent(t *testing.T) {
	Init(Config{Filename: filepath.Join(t.TempDir(), "app.log")})
	if err := AddJSONUDP("127.0.0.1:19999"); err != nil {
		t.Fatal(err)
	}
	Info().Msg("hello")

	if err := Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if err := Close(); err != nil {
		t.Fatalf("third close: %v", err)
	}
}

// Close 之後仍在寫的 log 不能 panic(send on closed channel)。
func TestWriteAfterClose(t *testing.T) {
	Init(Config{Filename: filepath.Join(t.TempDir(), "app.log")})
	if err := AddTextUDP("127.0.0.1:19999"); err != nil {
		t.Fatal(err)
	}
	if err := Close(); err != nil {
		t.Fatal(err)
	}
	Info().Str("k", "v").Msg("after close")
	Error().Msg("still fine")
}

// 併發寫的同時 Close,race detector 下也不能爆。
func TestConcurrentWriteAndClose(t *testing.T) {
	Init(Config{Filename: filepath.Join(t.TempDir(), "app.log")})
	if err := AddJSONUDP("127.0.0.1:19999"); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				Info().Int("j", j).Msg("spam")
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = Close()
		}()
	}
	wg.Wait()
}

// Close 後重新 Init 應該可以繼續使用。
func TestReinitAfterClose(t *testing.T) {
	dir := t.TempDir()
	Init(Config{Filename: filepath.Join(dir, "a.log")})
	Info().Msg("first")
	if err := Close(); err != nil {
		t.Fatal(err)
	}

	Init(Config{Filename: filepath.Join(dir, "b.log")})
	if err := AddJSONUDP("127.0.0.1:19999"); err != nil {
		t.Fatal(err)
	}
	Info().Msg("second")
	if got := Targets(); len(got) != 1 {
		t.Fatalf("targets = %v, want 1", got)
	}
	if err := Close(); err != nil {
		t.Fatal(err)
	}
}
