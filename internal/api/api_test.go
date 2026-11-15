package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chicohaager/lintux-modkit/httpx"
	"github.com/chicohaager/lintux-modkit/webmod"
	"github.com/chicohaager/zima-notepad/internal/files"
)

// fakeVerify stands in for the ZimaOS token check: only "Bearer good" passes.
func fakeVerify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized", "no token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// server is wired like webmod.Run wires it: the same Handler and the same body limit.
func server(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	data := filepath.Join(t.TempDir(), "DATA")
	if err := os.MkdirAll(filepath.Join(data, "AppData"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "AppData", "win.ini"), []byte("[a]\r\nx=1\r\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	root, err := files.NewRoot(data)
	if err != nil {
		t.Fatal(err)
	}
	h := webmod.Server(Options(root, "test"), fakeVerify, t.TempDir())
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s, data
}

func do(t *testing.T, method, u, body string, hdr map[string]string) (*http.Response, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, u, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	m := map[string]any{}
	_ = json.NewDecoder(res.Body).Decode(&m)
	return res, m
}

var auth = map[string]string{"Authorization": "Bearer good"}

func TestEveryAPIRouteNeedsTheSessionToken(t *testing.T) {
	s, data := server(t)
	p := url.QueryEscape(data + "/AppData/win.ini")
	for _, c := range []struct{ method, path string }{
		{"GET", "/notepad/api/info"},
		{"GET", "/notepad/api/list?path=" + url.QueryEscape(data)},
		{"GET", "/notepad/api/read?path=" + p},
		{"POST", "/notepad/api/write?eol=lf&encoding=utf-8&force=1&path=" + p},
		{"POST", "/notepad/api/create"},
		{"GET", "/notepad/api/unknown"},
	} {
		res, _ := do(t, c.method, s.URL+c.path, `{"path":"`+data+`/x"}`, nil)
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s without token: %d, want 401", c.method, c.path, res.StatusCode)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(data, "AppData", "win.ini")); string(b) != "[a]\r\nx=1\r\n" {
		t.Fatalf("file changed by an unauthenticated request: %q", b)
	}
	if _, err := os.Stat(filepath.Join(data, "x")); err == nil {
		t.Fatal("unauthenticated create made a file")
	}
	res, m := do(t, "GET", s.URL+"/notepad/health", "", nil)
	if res.StatusCode != http.StatusOK || m["status"] != "ok" {
		t.Fatalf("health: %d %v", res.StatusCode, m)
	}
}

func TestReadEditWriteKeepsCRLFOverHTTP(t *testing.T) {
	s, data := server(t)
	p := data + "/AppData/win.ini"
	res, d := do(t, "GET", s.URL+"/notepad/api/read?path="+url.QueryEscape(p), "", auth)
	if res.StatusCode != 200 || d["eol"] != "crlf" || d["text"] != "[a]\nx=1\n" {
		t.Fatalf("read: %d %v", res.StatusCode, d)
	}
	q := url.Values{"path": {p}, "sha": {d["sha"].(string)}, "encoding": {"utf-8"}, "eol": {"crlf"}}
	res, w := do(t, "POST", s.URL+"/notepad/api/write?"+q.Encode(), "[a]\nx=2\n", map[string]string{
		"Authorization": "Bearer good", "Content-Type": "text/plain;charset=UTF-8", "Origin": strings.TrimPrefix(s.URL, "http://"),
	})
	if res.StatusCode != 200 {
		t.Fatalf("write: %d %v", res.StatusCode, w)
	}
	if b, _ := os.ReadFile(p); string(b) != "[a]\r\nx=2\r\n" {
		t.Fatalf("on disk: %q", b)
	}
	// The old sha is stale now: a second write with it is a conflict.
	res, w = do(t, "POST", s.URL+"/notepad/api/write?"+q.Encode(), "[a]\nx=3\n", auth)
	if res.StatusCode != http.StatusConflict || w["code"] != "conflict" {
		t.Fatalf("stale write: %d %v", res.StatusCode, w)
	}
}

func TestCrossOriginWriteIsRejected(t *testing.T) {
	s, data := server(t)
	p := data + "/AppData/win.ini"
	q := url.Values{"path": {p}, "encoding": {"utf-8"}, "eol": {"lf"}, "force": {"1"}}
	res, _ := do(t, "POST", s.URL+"/notepad/api/write?"+q.Encode(), "evil", map[string]string{
		"Authorization": "Bearer good", "Origin": "https://evil.example",
	})
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin write: %d, want 403", res.StatusCode)
	}
	if b, _ := os.ReadFile(p); string(b) != "[a]\r\nx=1\r\n" {
		t.Fatalf("file changed: %q", b)
	}
}

func TestErrorsHaveStableCodes(t *testing.T) {
	s, data := server(t)
	if err := os.WriteFile(data+"/AppData/ro.txt", []byte("keep\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		method, path, body string
		status             int
		code               string
	}{
		{"GET", "/notepad/api/read?path=" + url.QueryEscape(data+"/../etc/passwd"), "", 403, "outside"},
		{"GET", "/notepad/api/read?path=" + url.QueryEscape(data+"/nope"), "", 404, "not_found"},
		{"GET", "/notepad/api/read?path=" + url.QueryEscape(data+"/AppData"), "", 400, "is_dir"},
		{"POST", "/notepad/api/create", `{"path":"` + data + `/AppData/win.ini"}`, 409, "exists"},
		{"POST", "/notepad/api/write?eol=lf&encoding=utf-8&force=1&path=" + url.QueryEscape(data+"/AppData/win.ini"), strings.Repeat("x", files.MaxBody+1), 413, "too_large"},
		{"POST", "/notepad/api/write?eol=xx&encoding=utf-8&force=1&path=" + url.QueryEscape(data+"/AppData/win.ini"), "x", 400, "bad_request"},
		{"POST", "/notepad/api/write?eol=lf&force=1&path=" + url.QueryEscape(data+"/AppData/win.ini"), "x", 400, "bad_request"}, // no encoding
		{"POST", "/notepad/api/write?eol=lf&encoding=utf-8&force=1&path=" + url.QueryEscape(data+"/AppData/ro.txt"), "x", 403, "read_only"},
		{"POST", "/notepad/api/create", `{"path":"` + data + "/" + strings.Repeat("n", 5000) + `"}`, 400, "bad_json"}, // body over 4 KiB
	} {
		res, m := do(t, c.method, s.URL+c.path, c.body, auth)
		if res.StatusCode != c.status || m["code"] != c.code {
			t.Errorf("%s %s: %d %v, want %d %s", c.method, c.path[:min(len(c.path), 60)], res.StatusCode, m["code"], c.status, c.code)
		}
	}
	if b, _ := os.ReadFile(data + "/AppData/win.ini"); string(b) != "[a]\r\nx=1\r\n" {
		t.Fatalf("a failed request changed the file: %q", b)
	}
}

func TestCreateAndList(t *testing.T) {
	s, data := server(t)
	res, m := do(t, "POST", s.URL+"/notepad/api/create", `{"path":"`+data+`/AppData/neu.txt"}`, auth)
	if res.StatusCode != 200 || m["path"] != data+"/AppData/neu.txt" {
		t.Fatalf("create: %d %v", res.StatusCode, m)
	}
	res, m = do(t, "GET", s.URL+"/notepad/api/list?path="+url.QueryEscape(data+"/AppData"), "", auth)
	if res.StatusCode != 200 {
		t.Fatalf("list: %d", res.StatusCode)
	}
	names := []string{}
	for _, e := range m["entries"].([]any) {
		names = append(names, e.(map[string]any)["name"].(string))
	}
	if strings.Join(names, ",") != "neu.txt,win.ini" {
		t.Fatalf("entries %v", names)
	}
}

// A log of 5 MiB (five times the old 1 MiB cap) opens and saves byte for byte.
func TestALargeLogOpensAndSaves(t *testing.T) {
	s, data := server(t)
	p := data + "/AppData/big.log"
	line := "2026-11-14T19:40:00+01:00 INFO something happened\n"
	if err := os.WriteFile(p, []byte(strings.Repeat(line, 5<<20/len(line))), 0o644); err != nil {
		t.Fatal(err)
	}
	res, d := do(t, "GET", s.URL+"/notepad/api/read?path="+url.QueryEscape(p), "", auth)
	if res.StatusCode != 200 {
		t.Fatalf("read 5 MiB: %d %v", res.StatusCode, d["code"])
	}
	text := d["text"].(string) + "appended\n"
	q := url.Values{"path": {p}, "sha": {d["sha"].(string)}, "encoding": {"utf-8"}, "eol": {"lf"}}
	res, w := do(t, "POST", s.URL+"/notepad/api/write?"+q.Encode(), text, auth)
	if res.StatusCode != 200 {
		t.Fatalf("write 5 MiB: %d %v", res.StatusCode, w)
	}
	if b, _ := os.ReadFile(p); string(b) != text {
		t.Fatalf("on disk: %d bytes, want %d", len(b), len(text))
	}
}

// Two texts in flight at most: a third request waits until one finishes.
func TestAtMostTwoTextRequestsRunAtOnce(t *testing.T) {
	s, data := server(t)
	write := s.URL + "/notepad/api/write?eol=lf&encoding=utf-8&force=1&path=" + url.QueryEscape(data+"/AppData/win.ini")
	var pipes []*io.PipeWriter
	done := make(chan int, maxTextRequests)
	for i := 0; i < maxTextRequests; i++ { // writes whose body never ends hold their slot
		pr, pw := io.Pipe()
		pipes = append(pipes, pw)
		t.Cleanup(func() { _ = pw.Close() }) // also when the test fails early: the server waits for these requests
		req, _ := http.NewRequest("POST", write, pr)
		req.Header.Set("Authorization", "Bearer good")
		go func() {
			res, err := http.DefaultClient.Do(req)
			if err == nil {
				_ = res.Body.Close()
				done <- res.StatusCode
			} else {
				done <- 0
			}
		}()
		_, _ = pw.Write([]byte("x"))
	}
	time.Sleep(200 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", s.URL+"/notepad/api/read?path="+url.QueryEscape(data+"/AppData/win.ini"), nil)
	req.Header.Set("Authorization", "Bearer good")
	if res, err := http.DefaultClient.Do(req); err == nil {
		_ = res.Body.Close()
		t.Fatalf("a third text request ran while two were in flight: %d", res.StatusCode)
	}
	for _, pw := range pipes {
		_ = pw.Close()
	}
	for i := 0; i < maxTextRequests; i++ {
		if c := <-done; c != 200 {
			t.Fatalf("held write: %d", c)
		}
	}
	if res, _ := do(t, "GET", s.URL+"/notepad/api/read?path="+url.QueryEscape(data+"/AppData/win.ini"), "", auth); res.StatusCode != 200 {
		t.Fatalf("read after the writes: %d", res.StatusCode)
	}
}
