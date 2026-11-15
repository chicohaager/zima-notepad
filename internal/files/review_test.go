package files

// Cases from the code and security review of 14.11.2026. Each was red against
// the path-string implementation before the move to os.Root.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// swapper replaces victim by a symlink to target and back, as fast as it can,
// until stop is closed: a container with write access to the folder can do this.
func swapper(t *testing.T, victim, target string, stop <-chan struct{}) *sync.WaitGroup {
	t.Helper()
	dir := filepath.Dir(victim)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		lnk, plain := filepath.Join(dir, ".swap-lnk"), filepath.Join(dir, ".swap-plain")
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = os.Symlink(target, lnk)
			_ = os.Rename(lnk, victim)
			_ = os.WriteFile(plain, []byte("inside\n"), 0o644)
			_ = os.Rename(plain, victim)
		}
	}()
	return &wg
}

func raceFor(d time.Duration, f func()) {
	for end := time.Now().Add(d); time.Now().Before(end); {
		f()
	}
}

func TestASwappedSymlinkCannotRedirectSaveOutside(t *testing.T) {
	r, data, outside := world(t)
	victim := filepath.Join(data, "AppData", "app", "victim.txt")
	write(t, victim, "inside\n", 0o644)
	stop := make(chan struct{})
	wg := swapper(t, victim, filepath.Join(outside, "shadow"), stop)
	raceFor(500*time.Millisecond, func() {
		_, _ = r.Save(SaveRequest{Path: victim, Text: "WRITTEN\n", Encoding: "utf-8", EOL: "lf", Force: true})
	})
	close(stop)
	wg.Wait()
	if b, _ := os.ReadFile(filepath.Join(outside, "shadow")); string(b) != "secret\n" {
		t.Fatalf("Save wrote outside the root: %q", b)
	}
}

func TestASwappedSymlinkCannotRedirectReadOutside(t *testing.T) {
	r, data, outside := world(t)
	victim := filepath.Join(data, "AppData", "app", "victim.txt")
	write(t, victim, "inside\n", 0o644)
	stop := make(chan struct{})
	wg := swapper(t, victim, filepath.Join(outside, "shadow"), stop)
	var leaked atomic.Bool
	raceFor(500*time.Millisecond, func() {
		if d, err := r.Read(victim); err == nil && strings.Contains(d.Text, "secret") {
			leaked.Store(true)
		}
	})
	close(stop)
	wg.Wait()
	if leaked.Load() {
		t.Fatal("Read returned a file outside the root")
	}
}

func TestASwappedSymlinkCannotRedirectCreateMode(t *testing.T) {
	r, data, outside := world(t)
	dir := filepath.Join(data, "AppData", "app")
	secret := filepath.Join(outside, "shadow")
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // rename a symlink over every name Create is about to make
		defer wg.Done()
		lnk := filepath.Join(dir, ".swap-lnk")
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = os.Symlink(secret, lnk)
			_ = os.Rename(lnk, filepath.Join(dir, "new.txt"))
			_ = os.Remove(filepath.Join(dir, "new.txt"))
		}
	}()
	raceFor(500*time.Millisecond, func() {
		_, _ = r.Create(filepath.Join(dir, "new.txt"), false)
	})
	close(stop)
	wg.Wait()
	if st, _ := os.Stat(secret); st.Mode().Perm() != 0o600 {
		t.Fatalf("Create changed the mode of a file outside the root to %o", st.Mode().Perm())
	}
}

func TestSaveRefusesAFileThatGrewPastTheLimit(t *testing.T) {
	r, data, _ := world(t)
	p := filepath.Join(data, "big.log")
	write(t, p, "x\n", 0o644)
	d, _ := r.Read(p)
	if err := os.Truncate(p, MaxText+1); err != nil { // the log grew after it was opened
		t.Fatal(err)
	}
	_, err := r.Save(SaveRequest{Path: p, Text: "y\n", Encoding: d.Encoding, EOL: d.EOL, SHA: d.SHA, Force: true})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
	if st, _ := os.Stat(p); st.Size() != MaxText+1 {
		t.Fatalf("file was rewritten: %d bytes", st.Size())
	}
}

func TestTwoSavesWithTheSameSHAConflict(t *testing.T) {
	r, data, _ := world(t)
	p := filepath.Join(data, "AppData", "app", "config.yaml")
	for i := 0; i < 50; i++ {
		d, err := r.Read(p)
		if err != nil {
			t.Fatal(err)
		}
		var ok atomic.Int32
		var wg sync.WaitGroup
		for _, text := range []string{fmt.Sprintf("a: A%d\n", i), fmt.Sprintf("a: B%d\n", i)} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := r.Save(SaveRequest{Path: p, Text: text, Encoding: d.Encoding, EOL: d.EOL, SHA: d.SHA}); err == nil {
					ok.Add(1)
				}
			}()
		}
		wg.Wait()
		if ok.Load() != 1 {
			t.Fatalf("run %d: %d of 2 saves with the same sha succeeded, want 1", i, ok.Load())
		}
	}
}

func TestSaveRefusesAWriteProtectedFile(t *testing.T) {
	r, data, _ := world(t)
	p := filepath.Join(data, "ro.txt")
	write(t, p, "keep\n", 0o444)
	d, err := r.Read(p)
	if err != nil || !d.ReadOnly {
		t.Fatalf("read: %+v %v", d, err)
	}
	if _, err := r.Save(SaveRequest{Path: p, Text: "x\n", Encoding: d.Encoding, EOL: d.EOL, SHA: d.SHA}); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("want ErrReadOnly, got %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(data, ".zima-notepad-backup-*")); len(left) != 0 {
		t.Fatalf("backup left behind: %v", left)
	}
}

func TestALongFileNameCanBeSaved(t *testing.T) {
	r, data, _ := world(t)
	p := filepath.Join(data, strings.Repeat("n", 240)+".txt")
	write(t, p, "a\n", 0o644)
	d, _ := r.Read(p)
	if _, err := r.Save(SaveRequest{Path: p, Text: "b\n", Encoding: d.Encoding, EOL: d.EOL, SHA: d.SHA}); err != nil {
		t.Fatal(err)
	}
}

func TestTheBackupHasTheModeOfTheFile(t *testing.T) {
	r, data, _ := world(t)
	p := filepath.Join(data, "AppData", "app", "config.yaml") // 0640
	d, _ := r.Read(p)
	defer func() { writeData, writeRestore = writeInPlace, writeInPlace }()
	fail := func(f *os.File, b []byte) error { return errors.New("i/o error") }
	writeData, writeRestore = fail, fail
	_, err := r.Save(SaveRequest{Path: p, Text: "a: 2\n", Encoding: d.Encoding, EOL: d.EOL, SHA: d.SHA})
	var be *BackupError
	if !errors.As(err, &be) {
		t.Fatalf("want BackupError, got %v", err)
	}
	st, err := os.Stat(be.Backup)
	if err != nil || st.Mode().Perm() != 0o640 {
		t.Fatalf("backup %s: %v %v", be.Backup, st, err)
	}
}

func TestSaveNeedsAnEncoding(t *testing.T) {
	r, data, _ := world(t)
	p := filepath.Join(data, "AppData", "app", "config.yaml")
	d, _ := r.Read(p)
	if _, err := r.Save(SaveRequest{Path: p, Text: "a: 2\n", EOL: d.EOL, SHA: d.SHA}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("want ErrBadRequest for a missing encoding, got %v", err)
	}
}

func TestAFIFODoesNotBlockRead(t *testing.T) {
	r, data, _ := world(t)
	p := filepath.Join(data, "pipe")
	if err := syscall.Mkfifo(p, 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := r.Read(p); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrBinary) {
			t.Fatalf("want ErrBinary, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read of a FIFO blocked")
	}
}

func TestRelativeAndLoopingLinksStayInside(t *testing.T) {
	r, data, _ := world(t)
	app := filepath.Join(data, "AppData", "app")
	links := map[string]string{"up": "../../../etc/shadow", "loop-a": "loop-b", "loop-b": "loop-a", "sibling": "../app/config.yaml"}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(app, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"up", "loop-a"} {
		if _, err := r.Read(filepath.Join(app, name)); !errors.Is(err, ErrOutside) {
			t.Errorf("Read(%s): want ErrOutside, got %v", name, err)
		}
	}
	if d, err := r.Read(filepath.Join(app, "sibling")); err != nil || d.Text != "a: 1\n" {
		t.Fatalf("relative link inside: %+v %v", d, err)
	}
}
