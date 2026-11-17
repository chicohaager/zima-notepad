package files

// Cases from the full code and security review of 17.11.2026. Each was red
// before its fix.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A save whose bytes exceed MaxText would leave a file that can neither be
// opened again nor saved over (the conflict check reads it): refuse it.
func TestSaveRefusesBytesOverMaxText(t *testing.T) {
	r, data, _ := world(t)
	p := filepath.Join(data, "big.txt")
	write(t, p, "a\n", 0o644)
	d, err := r.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("x", MaxText/2+1) // as UTF-16 more than MaxText bytes
	_, err = r.Save(SaveRequest{Path: p, Text: text, Encoding: "utf-16le", EOL: "lf", SHA: d.SHA})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "a\n" {
		t.Fatalf("file changed: %d bytes", len(got))
	}
}

// A read during a save must see the old or the new content, never the new
// bytes over the tail of the old ones.
func TestReadDuringSaveIsNotTorn(t *testing.T) {
	r, data, _ := world(t)
	p := filepath.Join(data, "t.txt")
	write(t, p, "OLD-OLD-OLD-OLD-TAIL\n", 0o644)
	d, _ := r.Read(p)
	inWrite, release := make(chan struct{}), make(chan struct{})
	writeData = func(f *os.File, b []byte) error {
		if _, err := f.WriteAt(b, 0); err != nil {
			return err
		}
		close(inWrite)
		<-release
		return writeInPlace(f, b)
	}
	t.Cleanup(func() { writeData = writeInPlace })
	saved := make(chan error, 1)
	go func() {
		_, err := r.Save(SaveRequest{Path: p, Text: "new\n", Encoding: "utf-8", EOL: "lf", SHA: d.SHA})
		saved <- err
	}()
	<-inWrite
	read := make(chan string, 1)
	go func() {
		d, err := r.Read(p)
		if err != nil {
			read <- "error: " + err.Error()
			return
		}
		read <- d.Text
	}()
	select { // give an unserialised read the chance to see the half-written file
	case <-read:
		close(release)
		t.Fatal("read returned while the save was writing")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-saved; err != nil {
		t.Fatal(err)
	}
	if got := <-read; got != "new\n" {
		t.Fatalf("read %q, want the saved content", got)
	}
}

// A BOM file with one invalid sequence used to be "not a text file" with no
// way to open it; it opens view-only in the BOM's encoding instead.
func TestBOMFileWithInvalidBytesOpensLossy(t *testing.T) {
	cases := []struct {
		raw []byte
		enc string
	}{
		{[]byte("\xEF\xBB\xBFname=caf\xe9\n"), "utf-8-bom"},
		{[]byte{0xFF, 0xFE, 'a', 0, 'b'}, "utf-16le"},        // odd length
		{[]byte{0xFE, 0xFF, 0xD8, 0x00, 0, 'a'}, "utf-16be"}, // lone surrogate
	}
	for _, c := range cases {
		r, data, _ := world(t)
		p := filepath.Join(data, "b.txt")
		write(t, p, string(c.raw), 0o644)
		d, err := r.Read(p)
		if err != nil {
			t.Fatalf("%s: %v", c.enc, err)
		}
		if d.Encoding != c.enc || !d.Lossy || !d.ReadOnly {
			t.Fatalf("%s: encoding %s lossy %v readOnly %v", c.enc, d.Encoding, d.Lossy, d.ReadOnly)
		}
	}
	r, data, _ := world(t)
	p := filepath.Join(data, "n.txt")
	write(t, p, "\xEF\xBB\xBFa\x00b", 0o644)
	if _, err := r.Read(p); !errors.Is(err, ErrBinary) {
		t.Fatalf("NUL after a UTF-8 BOM: %v, want ErrBinary", err)
	}
}

// Create reported an error after the entry was made, and a retry then said
// "exists": the entry is removed when setting mode or owner fails.
func TestCreateRemovesTheEntryWhenItFails(t *testing.T) {
	for _, dir := range []bool{false, true} {
		r, data, _ := world(t)
		fixCreated = func(*os.File, os.FileMode, os.FileInfo) error { return syscall.EPERM }
		_, err := r.Create(filepath.Join(data, "new"), dir)
		fixCreated = fixMode
		if err == nil {
			t.Fatalf("dir=%v: no error", dir)
		}
		if _, serr := os.Lstat(filepath.Join(data, "new")); !errors.Is(serr, os.ErrNotExist) {
			t.Fatalf("dir=%v: entry left behind (%v)", dir, serr)
		}
	}
}

// A chain of links whose targets walk d/../d/../… costs one Lstat per part:
// resolve bounds the parts, not only the links (security review 17.11.2026:
// 200 such entries took 3.5 s to list).
func TestResolveBoundsTheWalkedParts(t *testing.T) {
	r, data, _ := world(t)
	if err := os.Mkdir(filepath.Join(data, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("d/../", 800) + "d"
	prev := "d"
	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("l%d", i)
		if err := os.Symlink(long+"/../"+prev, filepath.Join(data, name)); err != nil {
			t.Fatal(err)
		}
		prev = name
	}
	if _, err := r.resolve(prev); !errors.Is(err, ErrOutside) {
		t.Fatalf("resolve of a 48 000-part walk: %v, want ErrOutside", err)
	}
	if got, err := r.resolve("l0"); err != nil || got != "d" { // 1 600 parts: still fine
		t.Fatalf("short walk: %q %v", got, err)
	}
}

// A special file (FIFO here; a device node in a container's bind mount the
// same way) is refused before it is opened for reading or writing: opening a
// device node can have side effects (a watchdog arms, a tty becomes the
// controlling terminal).
func TestSpecialFileIsNotOpenedForData(t *testing.T) {
	r, data, _ := world(t)
	p := filepath.Join(data, "fifo")
	if err := syscall.Mkfifo(p, 0o644); err != nil {
		t.Skip(err)
	}
	opened := 0
	reopen = func(f *os.File, flag int) (*os.File, error) { opened++; return reopenFD(f, flag) }
	t.Cleanup(func() { reopen = reopenFD })
	if _, err := r.Read(p); !errors.Is(err, ErrBinary) {
		t.Fatalf("read: %v", err)
	}
	if _, err := r.Save(SaveRequest{Path: p, Text: "x", Encoding: "utf-8", EOL: "lf"}); !errors.Is(err, ErrBinary) {
		t.Fatalf("save: %v", err)
	}
	if opened != 0 {
		t.Fatalf("FIFO opened for data %d times", opened)
	}
	// a regular file still goes through the reopen
	q := filepath.Join(data, "x.txt")
	write(t, q, "x\n", 0o644)
	if d, err := r.Read(q); err != nil || d.Text != "x\n" || opened != 1 {
		t.Fatalf("regular file: err %v opened=%d", err, opened)
	}
}

// A file system where root cannot chown (CIFS without unix extensions, NFS
// with root_squash) must not block every save: the backup then stays root's,
// mode 0600, and the save goes on.
func TestSaveWorksWhenTheBackupCannotBeChowned(t *testing.T) {
	r, data, _ := world(t)
	p := filepath.Join(data, "c.txt")
	write(t, p, "old\n", 0o644)
	d, _ := r.Read(p)
	ownBackup = func(*os.File, os.FileInfo) error { return syscall.EPERM }
	var mode os.FileMode
	writeData = func(f *os.File, b []byte) error { // the backup exists while the target is written
		m, _ := filepath.Glob(filepath.Join(data, ".zima-notepad-backup-*"))
		if len(m) == 1 {
			if st, err := os.Stat(m[0]); err == nil {
				mode = st.Mode().Perm()
			}
		}
		return writeInPlace(f, b)
	}
	t.Cleanup(func() { ownBackup, writeData = ownBackupDefault, writeInPlace })
	if _, err := r.Save(SaveRequest{Path: p, Text: "new\n", Encoding: "utf-8", EOL: "lf", SHA: d.SHA}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "new\n" {
		t.Fatalf("got %q", got)
	}
	if mode != 0o600 {
		t.Fatalf("backup mode %o when its owner could not be set, want 600", mode)
	}
}
