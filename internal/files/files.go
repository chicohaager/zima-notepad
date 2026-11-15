// Package files is the file access of zima-notepad: every path is confined to
// one root (/DATA on ZimaOS), text is decoded with its encoding and line ending
// remembered, and saving rewrites the file in place (inode, mode, owner and
// group stay) behind a backup copy of the old content.
//
// The daemon runs as root like every Lintux module, so the confinement is the
// security boundary. It is held by an os.Root handle on the resolved root, not
// by checking path strings: a symlink swapped in after a check cannot redirect
// an open outside the root (security review 14.11.2026 wrote /etc files through
// such a swap). Every file is opened once and then read, checked, written,
// chmod-ed and chown-ed through that one descriptor.
package files

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// Errors the API maps to HTTP codes.
var (
	ErrOutside     = errors.New("path is outside the allowed folder")
	ErrNotFound    = errors.New("not found")
	ErrNotDir      = errors.New("not a folder")
	ErrIsDir       = errors.New("is a folder")
	ErrTooLarge    = errors.New("file is too large to edit")
	ErrBinary      = errors.New("not a text file (NUL bytes, invalid encoding or not a regular file)")
	ErrReadOnly    = errors.New("file is write-protected")
	ErrConflict    = errors.New("file changed on disk since it was opened")
	ErrExists      = errors.New("already exists")
	ErrBadName     = errors.New("invalid name")
	ErrUnencodable = errors.New("text contains characters the file's encoding cannot store")
	ErrBadRequest  = errors.New("invalid request")
)

// MaxText is the largest file (and decoded text, as UTF-8) that can be opened.
// Saving sends the text as the raw request body; the API raises the webmod body
// cap to MaxBody for that (lintux-modkit webmod Options.MaxBody).
const MaxText = 32 << 20

// MaxBody is the request body cap of the API: the largest text plus room for
// growth while editing.
const MaxBody = MaxText + 1<<20

// Root confines all access to one folder.
type Root struct {
	// Dir is the root as shown to the user, e.g. "/DATA".
	Dir string
	// real is Dir with symlinks resolved; fs is the handle on it.
	real string
	fs   *os.Root
	// mu serialises Save and Create: two saves with the same sha must not both
	// pass the conflict check (review 14.11.2026: 157 of 300 parallel pairs did).
	mu sync.Mutex
}

// NewRoot opens dir (symlinks in dir itself are resolved once, at start).
func NewRoot(dir string) (*Root, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(real)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, ErrNotDir
	}
	h, err := os.OpenRoot(real)
	if err != nil {
		return nil, err
	}
	return &Root{Dir: abs, real: real, fs: h}, nil
}

// Close releases the root handle.
func (r *Root) Close() error { return r.fs.Close() }

// rel turns a user path ("/DATA/a/b" or "a/b") into a slash path relative to
// the root ("." for the root) and the path shown to the user. A ".." segment
// is refused in either form.
func (r *Root) rel(p string) (rel, shown string, err error) {
	if strings.ContainsRune(p, 0) {
		return "", "", ErrBadName
	}
	p = filepath.ToSlash(p)
	if strings.HasPrefix(p, "/") {
		d := filepath.ToSlash(r.Dir)
		switch {
		case p == d:
			p = ""
		case strings.HasPrefix(p, d+"/"):
			p = p[len(d)+1:]
		default:
			return "", "", ErrOutside
		}
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", "", ErrOutside
		}
	}
	rel = path.Clean("/" + p)[1:]
	if rel == "" {
		rel = "."
	}
	return rel, path.Join(filepath.ToSlash(r.Dir), rel), nil
}

// maxLinks bounds symlink resolution, as the kernel's ELOOP limit does.
const maxLinks = 40

// resolve follows the symlinks of rel inside the root and returns a path
// without symlinks. os.Root refuses every absolute symlink; an absolute link
// whose target lies inside the root (ln -s /DATA/Media …) is common on a NAS,
// so resolve turns it into the relative path. The result is still opened
// through os.Root: a link swapped in after resolve can only fail the open, not
// redirect it outside. A missing or unreadable component is reported (ErrNotFound).
func (r *Root) resolve(rel string) (string, error) {
	done := "."
	parts := strings.Split(rel, "/")
	for links := 0; len(parts) > 0; {
		seg := parts[0]
		parts = parts[1:]
		switch seg {
		case "", ".":
			continue
		case "..": // only from a link target; done has no symlinks, so this is lexical
			if done == "." {
				return "", ErrOutside
			}
			done = path.Dir(done)
			continue
		}
		next := path.Join(done, seg)
		fi, err := r.fs.Lstat(next)
		if err != nil {
			return "", mapErr(err)
		}
		if fi.Mode()&fs.ModeSymlink == 0 {
			done = next
			continue
		}
		if links++; links > maxLinks {
			return "", ErrOutside
		}
		target, err := r.fs.Readlink(next)
		if err != nil {
			return "", mapErr(err)
		}
		if path.IsAbs(target) {
			sub, ok := r.inside(target)
			if !ok {
				return "", ErrOutside
			}
			done = "."
			target = sub
		}
		parts = append(strings.Split(target, "/"), parts...)
	}
	return done, nil
}

// inside maps an absolute path under the real or the shown root to a path
// relative to the root.
func (r *Root) inside(abs string) (string, bool) {
	abs = path.Clean(abs)
	for _, base := range []string{filepath.ToSlash(r.real), filepath.ToSlash(r.Dir)} {
		if abs == base {
			return ".", true
		}
		if strings.HasPrefix(abs, base+"/") {
			return abs[len(base)+1:], true
		}
	}
	return "", false
}

// locate checks a user path and resolves it: the symlink-free relative path to
// open and the path shown to the user.
func (r *Root) locate(p string) (real, shown string, err error) {
	rel, shown, err := r.rel(p)
	if err != nil {
		return "", "", err
	}
	real, err = r.resolve(rel)
	return real, shown, err
}

// mapErr turns errors of os.Root into the package's errors. os.Root reports
// an escape only as text ("path escapes from parent"); an absolute symlink is
// such an escape as well.
func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case strings.Contains(err.Error(), "path escapes from parent"):
		return ErrOutside
	case errors.Is(err, fs.ErrNotExist):
		return ErrNotFound
	case errors.Is(err, syscall.EISDIR):
		return ErrIsDir
	case errors.Is(err, syscall.ENOTDIR):
		return ErrNotDir
	}
	return err
}

// open opens a path inside the root. O_NONBLOCK keeps a FIFO from blocking
// the open; it has no effect on regular files and folders.
func (r *Root) open(rel string, flag int) (*os.File, fs.FileInfo, error) {
	f, err := r.fs.OpenFile(rel, flag|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, mapErr(err)
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return f, st, nil
}

// Entry is one line of a folder listing.
type Entry struct {
	Name  string    `json:"name"`
	Path  string    `json:"path"`
	Dir   bool      `json:"dir"`
	Link  bool      `json:"link,omitempty"`
	Size  int64     `json:"size"`
	Mtime time.Time `json:"mtime"`
	// Outside marks a symlink that cannot be followed inside the root (target
	// outside, absolute or missing): listed, not openable.
	Outside bool `json:"outside,omitempty"`
}

// List returns the entries of a folder, folders first, then by name.
func (r *Root) List(p string) (string, []Entry, error) {
	rel, shown, err := r.locate(p)
	if err != nil {
		return "", nil, err
	}
	f, st, err := r.open(rel, os.O_RDONLY)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = f.Close() }() // read, or written and synced before: a close error adds nothing
	if !st.IsDir() {
		return "", nil, ErrNotDir
	}
	des, err := f.ReadDir(-1)
	if err != nil {
		return "", nil, err
	}
	out := make([]Entry, 0, len(des))
	for _, de := range des {
		info, err := de.Info()
		if err != nil {
			continue // removed while listing
		}
		e := Entry{Name: de.Name(), Path: path.Join(shown, de.Name()), Size: info.Size(), Mtime: info.ModTime(), Dir: info.IsDir()}
		if info.Mode()&fs.ModeSymlink != 0 {
			e.Link = true
			if t, err := r.resolve(path.Join(rel, de.Name())); err != nil {
				e.Outside = true
			} else if ts, err := r.fs.Stat(t); err != nil {
				e.Outside = true
			} else {
				e.Size, e.Mtime, e.Dir = ts.Size(), ts.ModTime(), ts.IsDir()
			}
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return shown, out, nil
}

// Doc is an opened text file.
type Doc struct {
	Path     string    `json:"path"`
	Text     string    `json:"text"` // line endings normalised to "\n"
	Encoding string    `json:"encoding"`
	EOL      string    `json:"eol"` // "lf", "crlf" or "cr"
	MixedEOL bool      `json:"mixedEol,omitempty"`
	SHA      string    `json:"sha"`
	Size     int64     `json:"size"`
	Mtime    time.Time `json:"mtime"`
	Mode     string    `json:"mode"`
	ReadOnly bool      `json:"readOnly,omitempty"`
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// readText checks an opened file and reads at most MaxText bytes of it.
func readText(f *os.File, st fs.FileInfo) ([]byte, error) {
	switch {
	case st.IsDir():
		return nil, ErrIsDir
	case !st.Mode().IsRegular():
		return nil, ErrBinary
	case st.Size() > MaxText:
		return nil, ErrTooLarge
	}
	raw, err := io.ReadAll(io.LimitReader(f, MaxText+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxText { // grew after the stat
		return nil, ErrTooLarge
	}
	return raw, nil
}

func readOnly(st fs.FileInfo) bool { return st.Mode().Perm()&0o222 == 0 }

// Read opens a text file.
func (r *Root) Read(p string) (*Doc, error) {
	rel, shown, err := r.locate(p)
	if err != nil {
		return nil, err
	}
	f, st, err := r.open(rel, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read, or written and synced before: a close error adds nothing
	raw, err := readText(f, st)
	if err != nil {
		return nil, err
	}
	text, enc, err := Decode(raw)
	if err != nil {
		return nil, err
	}
	if len(text) > MaxText {
		return nil, ErrTooLarge
	}
	eol, mixed := DetectEOL(text)
	return &Doc{
		Path: shown, Text: NormalizeEOL(text), Encoding: enc, EOL: eol, MixedEOL: mixed,
		SHA: sum(raw), Size: st.Size(), Mtime: st.ModTime(), Mode: fmt.Sprintf("%04o", st.Mode().Perm()),
		ReadOnly: readOnly(st),
	}, nil
}

// SaveRequest is what the editor sends to store a text.
type SaveRequest struct {
	Path     string
	Text     string // "\n" line endings
	Encoding string
	EOL      string
	// SHA is the sha256 the file had when it was opened.
	SHA   string
	Force bool // overwrite even when the file changed on disk
}

// Saved describes the file after saving.
type Saved struct {
	SHA   string    `json:"sha"`
	Size  int64     `json:"size"`
	Mtime time.Time `json:"mtime"`
}

// Save writes a text into the existing file, in place: the inode stays, so
// mode, owner, group, hard links and — the reason — a container that mounts
// this one file (docker -v file:file binds the inode) all see the new content.
// A rename-based atomic save gave the file a new inode and the container kept
// the old one (tests/bindmount.sh, 14.11.2026). Safety comes from a backup
// copy of the old content, as Vim's 'backupcopy=yes' does: see saveInPlace.
// A write-protected file (no write bit) is refused, although root could write it.
func (r *Root) Save(q SaveRequest) (*Saved, error) {
	rel, shown, err := r.locate(q.Path)
	if err != nil {
		return nil, err
	}
	switch q.EOL {
	case "lf", "crlf", "cr":
	default:
		return nil, fmt.Errorf("%w: line ending %q (lf, crlf or cr)", ErrBadRequest, q.EOL)
	}
	if !utf8.ValidString(q.Text) {
		return nil, fmt.Errorf("%w: text is not valid UTF-8", ErrBadRequest)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	f, st, err := r.open(rel, os.O_RDWR)
	if errors.Is(err, fs.ErrPermission) {
		if st, serr := r.fs.Stat(rel); serr == nil && readOnly(st) {
			return nil, ErrReadOnly // not root: the open already fails on the missing write bit
		}
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read, or written and synced before: a close error adds nothing
	if st.Mode().IsRegular() && readOnly(st) {
		return nil, ErrReadOnly
	}
	cur, err := readText(f, st)
	if err != nil {
		return nil, err
	}
	if !q.Force && sum(cur) != q.SHA {
		return nil, ErrConflict
	}
	data, err := Encode(ApplyEOL(q.Text, q.EOL), q.Encoding)
	if err != nil {
		return nil, err
	}
	if err := r.saveInPlace(f, st, path.Dir(rel), path.Dir(shown), cur, data); err != nil {
		return nil, err
	}
	nst, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return &Saved{SHA: sum(data), Size: nst.Size(), Mtime: nst.ModTime()}, nil
}

// Hooks for tests: they make the in-place write or the restore fail.
var (
	writeData    = writeInPlace
	writeRestore = writeInPlace
)

// BackupError reports a save that failed and could not put the old content
// back; the old content is in Backup.
type BackupError struct {
	Err    error
	Backup string
}

func (e *BackupError) Error() string {
	return fmt.Sprintf("save failed (%v) and the old content could not be restored; it is kept in %s", e.Err, e.Backup)
}

func (e *BackupError) Unwrap() error { return e.Err }

// backupName is short and of fixed length, so a target name near the file
// system's 255-byte limit still gets a backup (review 14.11.2026).
func backupName() string { return ".zima-notepad-backup-" + rand.Text() }

// saveInPlace copies the old content to a backup file next to the target (same
// owner and mode), rewrites the target through its open descriptor and deletes
// the backup. If the rewrite fails, the old content is written back; if that
// fails too, the backup stays and its name is in the error. A full disk fails
// while writing the backup, before the target is touched.
func (r *Root) saveInPlace(f *os.File, st fs.FileInfo, dirRel, dirShown string, old, data []byte) error {
	name := backupName()
	bakRel := path.Join(dirRel, name)
	bak, err := r.fs.OpenFile(bakRel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("backup copy: %w", mapErr(err))
	}
	err = writeBackup(bak, st, old)
	if cerr := bak.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		r.removeBackup(bakRel, dirShown, name)
		return fmt.Errorf("backup copy: %w", err)
	}
	if err := writeData(f, data); err != nil {
		if rerr := writeRestore(f, old); rerr != nil {
			return &BackupError{Err: err, Backup: path.Join(dirShown, name)}
		}
		r.removeBackup(bakRel, dirShown, name)
		return err
	}
	r.removeBackup(bakRel, dirShown, name)
	return nil
}

// removeBackup deletes a backup that is no longer needed. Failing to do so
// must not turn a finished save into an error (the editor would keep the old
// sha and report a false conflict on the next save); it is logged instead.
func (r *Root) removeBackup(rel, dirShown, name string) {
	if err := r.fs.Remove(rel); err != nil {
		log.Printf("[notepad-web] remove backup %s: %v", path.Join(dirShown, name), err)
	}
}

func writeBackup(bak *os.File, st fs.FileInfo, old []byte) error {
	if sys, ok := st.Sys().(*syscall.Stat_t); ok && os.Geteuid() == 0 {
		if err := bak.Chown(int(sys.Uid), int(sys.Gid)); err != nil {
			return err
		}
	}
	if err := bak.Chmod(st.Mode().Perm()); err != nil {
		return err
	}
	if _, err := bak.Write(old); err != nil {
		return err
	}
	return bak.Sync()
}

// writeInPlace overwrites a file's content without replacing the file: write
// from the start, cut off what is left of a longer old content, sync.
func writeInPlace(f *os.File, data []byte) error {
	if _, err := f.WriteAt(data, 0); err != nil {
		return err
	}
	if err := f.Truncate(int64(len(data))); err != nil {
		return err
	}
	return f.Sync()
}

// Create makes an empty file or a folder. It takes owner and group from the
// parent folder, so a file created by the root daemon in a user's share
// belongs to that user. Files get 0644, folders 0755. The new entry is made
// and opened relative to the opened parent folder with O_NOFOLLOW, and mode
// and owner are set on that descriptor: a symlink renamed over the new name
// cannot redirect them (review 14.11.2026 made a file outside the root 0644).
func (r *Root) Create(p string, dir bool) (string, error) {
	rel, shown, err := r.rel(p)
	if err != nil {
		return "", err
	}
	name := path.Base(rel)
	if rel == "." || strings.ContainsRune(name, '\\') {
		return "", ErrBadName
	}
	dirRel, err := r.resolve(path.Dir(rel))
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	parent, pst, err := r.open(dirRel, os.O_RDONLY)
	if err != nil {
		return "", err
	}
	defer func() { _ = parent.Close() }() // read, or written and synced before: a close error adds nothing
	if !pst.IsDir() {
		return "", ErrNotDir
	}
	pfd := int(parent.Fd())
	const at = syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	mode := os.FileMode(0o644)
	var fd int
	if dir {
		mode = 0o755
		if err = syscall.Mkdirat(pfd, name, uint32(mode)); err == nil {
			fd, err = syscall.Openat(pfd, name, syscall.O_RDONLY|syscall.O_DIRECTORY|at, 0)
		}
	} else {
		fd, err = syscall.Openat(pfd, name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|at, uint32(mode))
	}
	if err != nil {
		if errors.Is(err, syscall.EEXIST) {
			return "", ErrExists
		}
		return "", fmt.Errorf("create %s: %w", shown, err)
	}
	f := os.NewFile(uintptr(fd), shown)
	defer func() { _ = f.Close() }() // read, or written and synced before: a close error adds nothing
	// The process umask must not decide the mode (a umask of 077 would make the file unreadable for the share's users).
	if err := f.Chmod(mode); err != nil {
		return "", err
	}
	if sys, ok := pst.Sys().(*syscall.Stat_t); ok && os.Geteuid() == 0 {
		if err := f.Chown(int(sys.Uid), int(sys.Gid)); err != nil {
			return "", fmt.Errorf("set owner: %w", err)
		}
	}
	return shown, nil
}
