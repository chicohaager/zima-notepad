// Package api is the HTTP API of the Notepad tile under /notepad/api/, built
// into the lintux-modkit webmod frame by Options.
package api

import (
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/chicohaager/lintux-modkit/httpx"
	"github.com/chicohaager/lintux-modkit/webmod"
	"github.com/chicohaager/zima-notepad/internal/files"
)

const route = "/notepad"

// Options describes the module for webmod.Run and webmod.Handler.
func Options(root *files.Root, version string) webmod.Options {
	return webmod.Options{
		Name: "notepad", Service: "notepad-web", Binary: "/usr/bin/notepad-web",
		Route: route, Version: version,
		MaxBody: files.MaxBody,
		API:     func(verify func(http.Handler) http.Handler) http.Handler { return api(root, version, verify) },
	}
}

func api(root *files.Root, version string, verify func(http.Handler) http.Handler) http.Handler {
	mux := http.NewServeMux()
	h := &handlers{root: root, version: version, texts: make(chan struct{}, maxTextRequests)}
	mux.Handle(route+"/api/info", verify(http.HandlerFunc(h.info)))
	mux.Handle(route+"/api/list", verify(http.HandlerFunc(h.list)))
	mux.Handle(route+"/api/read", verify(http.HandlerFunc(h.read)))
	mux.Handle(route+"/api/write", verify(http.HandlerFunc(h.write)))
	mux.Handle(route+"/api/create", verify(http.HandlerFunc(h.create)))
	mux.Handle(route+"/", verify(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { httpx.NotFound(w) })))
	return mux
}

// maxTextRequests bounds reads and writes in flight: one request for a 32 MiB
// text holds several copies of it (raw, decoded, JSON-escaped); review
// 14.11.2026 measured 486 MB peak for one read, 1.9 GB for four in parallel.
const maxTextRequests = 2

// maxCreateBody caps the JSON body of /create, which holds one path.
const maxCreateBody = 4 << 10

type handlers struct {
	root    *files.Root
	version string
	texts   chan struct{} // semaphore of maxTextRequests
}

// text runs f while holding a slot of the text semaphore, or answers 503 when
// the client gives up waiting.
func (h *handlers) text(w http.ResponseWriter, r *http.Request, f func()) {
	select {
	case h.texts <- struct{}{}:
		defer func() { <-h.texts }()
		f()
	case <-r.Context().Done():
		httpx.WriteError(w, http.StatusServiceUnavailable, "busy", "request cancelled while waiting")
	}
}

// fail maps the errors of package files to HTTP answers with stable codes.
func fail(w http.ResponseWriter, err error) {
	var ue *files.UnencodableError
	switch {
	case errors.As(err, &ue):
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"code": "unencodable", "error": err.Error(), "char": string(ue.Char), "line": ue.Line,
		})
	case errors.Is(err, files.ErrOutside):
		httpx.WriteError(w, http.StatusForbidden, "outside", err.Error())
	case errors.Is(err, files.ErrNotFound), errors.Is(err, os.ErrNotExist):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, files.ErrNotDir):
		httpx.WriteError(w, http.StatusBadRequest, "not_dir", err.Error())
	case errors.Is(err, files.ErrIsDir):
		httpx.WriteError(w, http.StatusBadRequest, "is_dir", err.Error())
	case errors.Is(err, files.ErrTooLarge):
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "too_large", err.Error())
	case errors.Is(err, files.ErrBinary):
		httpx.WriteError(w, http.StatusUnsupportedMediaType, "binary", err.Error())
	case errors.Is(err, files.ErrReadOnly):
		httpx.WriteError(w, http.StatusForbidden, "read_only", err.Error())
	case errors.Is(err, files.ErrConflict):
		httpx.WriteError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, files.ErrExists):
		httpx.WriteError(w, http.StatusConflict, "exists", err.Error())
	case errors.Is(err, files.ErrBadRequest):
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
	case errors.Is(err, files.ErrBadName):
		httpx.WriteError(w, http.StatusBadRequest, "bad_name", err.Error())
	case errors.Is(err, os.ErrPermission):
		httpx.WriteError(w, http.StatusForbidden, "permission", err.Error())
	default:
		log.Printf("[notepad-web] %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed", err.Error())
	}
}

func (h *handlers) info(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.MethodNotAllowed(w)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"root": h.root.Dir, "encodings": files.Encodings, "maxText": files.MaxText, "version": h.version,
	})
}

func (h *handlers) list(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.MethodNotAllowed(w)
		return
	}
	p := r.URL.Query().Get("path")
	if p == "" {
		p = h.root.Dir
	}
	shown, entries, err := h.root.List(p)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"path": shown, "entries": entries})
}

func (h *handlers) read(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.MethodNotAllowed(w)
		return
	}
	h.text(w, r, func() {
		d, err := h.root.Read(r.URL.Query().Get("path"))
		if err != nil {
			fail(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, d)
	})
}

// write takes the text as the raw request body (text/plain, UTF-8) so a file
// of nearly the body limit fits; everything else comes in the query.
func (h *handlers) write(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.MethodNotAllowed(w)
		return
	}
	h.text(w, r, func() {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				httpx.WriteError(w, http.StatusRequestEntityTooLarge, "too_large", "text is larger than "+strconv.FormatInt(mbe.Limit, 10)+" bytes")
				return
			}
			httpx.WriteError(w, http.StatusBadRequest, "bad_body", err.Error())
			return
		}
		q := r.URL.Query()
		saved, err := h.root.Save(files.SaveRequest{
			Path: q.Get("path"), Text: string(body), Encoding: q.Get("encoding"), EOL: q.Get("eol"),
			SHA: q.Get("sha"), Force: q.Get("force") == "1",
		})
		if err != nil {
			fail(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, saved)
	})
}

func (h *handlers) create(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.MethodNotAllowed(w)
		return
	}
	var in struct {
		Path string `json:"path"`
		Dir  bool   `json:"dir"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCreateBody)
	if !httpx.Decode(w, r, &in) {
		return
	}
	shown, err := h.root.Create(in.Path, in.Dir)
	if err != nil {
		fail(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"path": shown})
}
