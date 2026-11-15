package files

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// world builds a root with a file, a subfolder, a symlink inside and one
// pointing outside, and a secret file outside the root.
func world(t *testing.T) (*Root, string, string) {
	t.Helper()
	base := t.TempDir()
	data := filepath.Join(base, "DATA")
	outside := filepath.Join(base, "etc")
	for _, d := range []string{data, outside, filepath.Join(data, "AppData", "app")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(outside, "shadow"), "secret\n", 0o600)
	write(t, filepath.Join(data, "AppData", "app", "config.yaml"), "a: 1\n", 0o640)
	if err := os.Symlink(filepath.Join(data, "AppData"), filepath.Join(data, "inside-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(data, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "shadow"), filepath.Join(data, "AppData", "shadow-link")); err != nil {
		t.Fatal(err)
	}
	r, err := NewRoot(data)
	if err != nil {
		t.Fatal(err)
	}
	return r, data, outside
}

func write(t *testing.T, p, s string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
}

func TestPathsOutsideTheRootAreRejected(t *testing.T) {
	r, data, outside := world(t)
	for _, p := range []string{
		"/DATA/../etc/shadow",
		data + "/../etc/shadow",
		"../etc/shadow",
		"AppData/../../etc/shadow",
		outside + "/shadow",
		"/etc/shadow",
		data + "/escape/shadow",
		data + "/escape",
		data + "/AppData/shadow-link",
		"escape/shadow",
		"AppData/shadow-link",
		data + "x/shadow", // sibling folder with the root as prefix
		"a\x00b",
	} {
		if _, err := r.Read(p); !errors.Is(err, ErrOutside) && !errors.Is(err, ErrBadName) {
			t.Errorf("Read(%q): want ErrOutside, got %v", p, err)
		}
		if _, err := r.Save(SaveRequest{Path: p, Text: "x", Force: true}); !errors.Is(err, ErrOutside) && !errors.Is(err, ErrBadName) {
			t.Errorf("Save(%q): want ErrOutside, got %v", p, err)
		}
	}
	for _, p := range []string{data + "/escape/new.txt", "../new.txt", data + "/../new.txt"} {
		if _, err := r.Create(p, false); !errors.Is(err, ErrOutside) {
			t.Errorf("Create(%q): want ErrOutside, got %v", p, err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(outside, "shadow")); string(b) != "secret\n" {
		t.Fatalf("file outside the root changed: %q", b)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(data), "new.txt")); err == nil {
		t.Fatal("a file was created outside the root")
	}
}

func TestPathsInsideTheRootWork(t *testing.T) {
	r, data, _ := world(t)
	for _, p := range []string{data + "/AppData/app/config.yaml", "AppData/app/config.yaml", data + "/inside-link/app/config.yaml"} {
		d, err := r.Read(p)
		if err != nil {
			t.Fatalf("Read(%q): %v", p, err)
		}
		if d.Text != "a: 1\n" {
			t.Fatalf("Read(%q) = %q", p, d.Text)
		}
	}
	shown, es, err := r.List(data)
	if err != nil {
		t.Fatal(err)
	}
	if shown != filepath.ToSlash(data) {
		t.Fatalf("shown %q", shown)
	}
	got := map[string]Entry{}
	for _, e := range es {
		got[e.Name] = e
	}
	if !got["AppData"].Dir || !got["inside-link"].Dir || got["inside-link"].Outside {
		t.Fatalf("inside link: %+v", got["inside-link"])
	}
	if !got["escape"].Outside {
		t.Fatalf("escape link not marked outside: %+v", got["escape"])
	}
	if es[0].Dir != true {
		t.Fatal("folders not first")
	}
}

func TestEveryEncodingRoundTripsByteForByte(t *testing.T) {
	cases := map[string][]byte{
		"utf-8":        []byte("grüße: ✓\nzwei\n"),
		"utf-8-bom":    append([]byte{0xEF, 0xBB, 0xBF}, "grüße\r\nzwei\r\n"...),
		"utf-16le":     append([]byte{0xFF, 0xFE}, appendUTF16(nil, "[Config]\r\nName=Größe 😀\r\n", binary.LittleEndian)...),
		"utf-16be":     append([]byte{0xFE, 0xFF}, appendUTF16(nil, "a\nb\n", binary.BigEndian)...),
		"windows-1252": {'G', 'r', 0xF6, 0xDF, 'e', ' ', 0x80, ' ', 0x81, ' ', 0x9D, '\r', 'x'},
	}
	for want, raw := range cases {
		r, data, _ := world(t)
		p := filepath.Join(data, "f.txt")
		write(t, p, string(raw), 0o644)
		d, err := r.Read(p)
		if err != nil {
			t.Fatalf("%s: %v", want, err)
		}
		if d.Encoding != want {
			t.Fatalf("%s: detected %s", want, d.Encoding)
		}
		if strings.Contains(d.Text, "\r") {
			t.Fatalf("%s: text not normalised: %q", want, d.Text)
		}
		if _, err := r.Save(SaveRequest{Path: p, Text: d.Text, Encoding: d.Encoding, EOL: d.EOL, SHA: d.SHA}); err != nil {
			t.Fatalf("%s: save: %v", want, err)
		}
		got, _ := os.ReadFile(p)
		if !bytes.Equal(got, raw) {
			t.Fatalf("%s: bytes changed\nwant %x\ngot  %x", want, raw, got)
		}
	}
}

func TestLineEndings(t *testing.T) {
	for in, want := range map[string]string{"a\nb\n": "lf", "a\r\nb\r\n": "crlf", "a\rb\r": "cr", "a": "lf", "a\r\nb\r\nc\n": "crlf"} {
		got, _ := DetectEOL(in)
		if got != want {
			t.Errorf("DetectEOL(%q) = %s, want %s", in, got, want)
		}
	}
	if _, mixed := DetectEOL("a\r\nb\n"); !mixed {
		t.Error("mixed line endings not reported")
	}
	if _, mixed := DetectEOL("a\r\nb\r\n"); mixed {
		t.Error("uniform CRLF reported as mixed")
	}
	if got := ApplyEOL("a\nb\r\nc", "crlf"); got != "a\r\nb\r\nc" {
		t.Errorf("ApplyEOL crlf = %q", got)
	}
}

func TestSaveKeepsInodeModeAndOwner(t *testing.T) {
	r, data, _ := world(t)
	p := filepath.Join(data, "AppData", "app", "config.yaml")
	if err := os.Chmod(p, 0o640|os.ModeSetgid); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(p)
	d, err := r.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Save(SaveRequest{Path: p, Text: "a: 2\n", Encoding: d.Encoding, EOL: d.EOL, SHA: d.SHA}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(p)
	if after.Sys().(*syscall.Stat_t).Ino != before.Sys().(*syscall.Stat_t).Ino {
		t.Fatal("inode changed: a container that mounts this one file would keep the old content")
	}
	if after.Mode() != before.Mode() {
		t.Fatalf("mode %v -> %v", before.Mode(), after.Mode())
	}
	bs, as := before.Sys().(*syscall.Stat_t), after.Sys().(*syscall.Stat_t)
	if bs.Uid != as.Uid || bs.Gid != as.Gid {
		t.Fatalf("owner %d:%d -> %d:%d", bs.Uid, bs.Gid, as.Uid, as.Gid)
	}
	if b, _ := os.ReadFile(p); string(b) != "a: 2\n" {
		t.Fatalf("content %q", b)
	}
	left, _ := filepath.Glob(filepath.Join(data, "AppData", "app", ".*zima-notepad*"))
	if len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
}

func TestSaveDetectsAConflict(t *testing.T) {
	r, data, _ := world(t)
	p := filepath.Join(data, "AppData", "app", "config.yaml")
	d, _ := r.Read(p)
	write(t, p, "changed outside\n", 0o640)
	_, err := r.Save(SaveRequest{Path: p, Text: "mine\n", Encoding: d.Encoding, EOL: d.EOL, SHA: d.SHA})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "changed outside\n" {
		t.Fatalf("conflicting save overwrote the file: %q", b)
	}
	if _, err := r.Save(SaveRequest{Path: p, Text: "mine\n", Encoding: d.Encoding, EOL: d.EOL, SHA: d.SHA, Force: true}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "mine\n" {
		t.Fatalf("forced save: %q", b)
	}
}

func TestSaveKeepsHardLinks(t *testing.T) {
	r, data, _ := world(t)
	p := filepath.Join(data, "a.txt")
	write(t, p, "one\n", 0o644)
	if err := os.Link(p, filepath.Join(data, "b.txt")); err != nil {
		t.Fatal(err)
	}
	d, _ := r.Read(p)
	if _, err := r.Save(SaveRequest{Path: p, Text: "two\n", Encoding: d.Encoding, EOL: d.EOL, SHA: d.SHA}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(data, "b.txt")); string(b) != "two\n" {
		t.Fatalf("hard link lost the change: %q", b)
	}
}

func TestSaveThroughASymlinkKeepsTheLink(t *testing.T) {
	r, data, _ := world(t)
	link := filepath.Join(data, "config-link.yaml")
	if err := os.Symlink(filepath.Join(data, "AppData", "app", "config.yaml"), link); err != nil {
		t.Fatal(err)
	}
	d, _ := r.Read(link)
	if _, err := r.Save(SaveRequest{Path: link, Text: "a: 3\n", Encoding: d.Encoding, EOL: d.EOL, SHA: d.SHA}); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Lstat(link)
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink replaced by a regular file")
	}
	if b, _ := os.ReadFile(filepath.Join(data, "AppData", "app", "config.yaml")); string(b) != "a: 3\n" {
		t.Fatalf("target: %q", b)
	}
}

func TestUnencodableCharacterFailsInsteadOfLosingIt(t *testing.T) {
	r, data, _ := world(t)
	p := filepath.Join(data, "l.txt")
	write(t, p, string([]byte{'a', 0xE4, '\n'}), 0o644)
	d, _ := r.Read(p)
	if d.Encoding != "windows-1252" {
		t.Fatalf("encoding %s", d.Encoding)
	}
	_, err := r.Save(SaveRequest{Path: p, Text: "aä\n✓\n", Encoding: d.Encoding, EOL: d.EOL, SHA: d.SHA})
	var ue *UnencodableError
	if !errors.As(err, &ue) || ue.Char != '✓' || ue.Line != 2 {
		t.Fatalf("want UnencodableError ✓ line 2, got %v", err)
	}
	if b, _ := os.ReadFile(p); !bytes.Equal(b, []byte{'a', 0xE4, '\n'}) {
		t.Fatalf("file changed: %x", b)
	}
}

func TestBinaryAndLargeFilesAreRefused(t *testing.T) {
	r, data, _ := world(t)
	write(t, filepath.Join(data, "bin"), "ELF\x00\x01", 0o755)
	if _, err := r.Read(filepath.Join(data, "bin")); !errors.Is(err, ErrBinary) {
		t.Fatalf("binary: %v", err)
	}
	big := filepath.Join(data, "big.log")
	write(t, big, strings.Repeat("x", MaxText+1), 0o644)
	if _, err := r.Read(big); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("large: %v", err)
	}
	write(t, big, strings.Repeat("x", MaxText), 0o644)
	if _, err := r.Read(big); err != nil {
		t.Fatalf("file of exactly MaxText: %v", err)
	}
	if _, err := r.Read(data); !errors.Is(err, ErrIsDir) {
		t.Fatalf("folder: %v", err)
	}
}

func TestCreate(t *testing.T) {
	r, data, _ := world(t)
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	shown, err := r.Create(data+"/AppData/new.conf", false)
	if err != nil {
		t.Fatal(err)
	}
	if shown != filepath.ToSlash(data+"/AppData/new.conf") {
		t.Fatalf("shown %q", shown)
	}
	st, _ := os.Stat(data + "/AppData/new.conf")
	if st.Mode().Perm() != 0o644 || st.Size() != 0 {
		t.Fatalf("new file %v %d", st.Mode(), st.Size())
	}
	if _, err := r.Create(data+"/AppData/new.conf", false); !errors.Is(err, ErrExists) {
		t.Fatalf("second create: %v", err)
	}
	if _, err := r.Create(data+"/AppData/sub", true); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(data + "/AppData/sub"); !st.IsDir() || st.Mode().Perm() != 0o755 {
		t.Fatalf("folder not created as 0755: %v", st.Mode())
	}
	for _, bad := range []string{data, data + "/", data + "/AppData/..", data + "/nope/x.txt"} {
		if _, err := r.Create(bad, false); err == nil {
			t.Errorf("Create(%q) succeeded", bad)
		}
	}
}

func TestAFailedWriteRestoresTheOldContent(t *testing.T) {
	r, data, _ := world(t)
	p := filepath.Join(data, "AppData", "app", "config.yaml")
	d, _ := r.Read(p)
	defer func() { writeData = writeInPlace }()
	writeData = func(f *os.File, b []byte) error {
		_, _ = f.WriteAt([]byte("h"), 0) // the disk filled up after one byte
		return errors.New("no space left on device")
	}
	_, err := r.Save(SaveRequest{Path: p, Text: "a: 2\n", Encoding: d.Encoding, EOL: d.EOL, SHA: d.SHA})
	if err == nil || !strings.Contains(err.Error(), "no space") {
		t.Fatalf("want the write error, got %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "a: 1\n" {
		t.Fatalf("old content not restored: %q", b)
	}
	if left, _ := filepath.Glob(filepath.Join(data, "AppData", "app", ".*zima-notepad*")); len(left) != 0 {
		t.Fatalf("backup left after a successful restore: %v", left)
	}
}

func TestAFailedRestoreKeepsTheBackup(t *testing.T) {
	r, data, _ := world(t)
	p := filepath.Join(data, "AppData", "app", "config.yaml")
	d, _ := r.Read(p)
	defer func() { writeData, writeRestore = writeInPlace, writeInPlace }()
	fail := func(f *os.File, b []byte) error {
		_, _ = f.WriteAt([]byte("h"), 0)
		return errors.New("i/o error")
	}
	writeData, writeRestore = fail, fail
	_, err := r.Save(SaveRequest{Path: p, Text: "a: 2\n", Encoding: d.Encoding, EOL: d.EOL, SHA: d.SHA})
	var be *BackupError
	if !errors.As(err, &be) {
		t.Fatalf("want BackupError, got %v", err)
	}
	b, rerr := os.ReadFile(be.Backup)
	if rerr != nil || string(b) != "a: 1\n" {
		t.Fatalf("backup %s: %q %v", be.Backup, b, rerr)
	}
}
