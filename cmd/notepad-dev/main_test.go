package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHostGuardRefusesForeignHosts(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := hostGuard("127.0.0.1:8471", ok)
	for host, want := range map[string]int{
		"127.0.0.1:8471":                 http.StatusTeapot,
		"localhost:8471":                 http.StatusTeapot,
		"rebind.example.invalid:8471":    http.StatusMisdirectedRequest,
		"127.0.0.1:9999":                 http.StatusMisdirectedRequest,
		"127.0.0.1.example.invalid:8471": http.StatusMisdirectedRequest,
	} {
		req := httptest.NewRequest("GET", "/notepad/api/info", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("Host %s: %d, want %d", host, rec.Code, want)
		}
	}
}
