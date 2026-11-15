// Command notepad-dev runs the Notepad tile on this machine for development and
// the browser tests: the same handler as notepad-web, without the ZimaOS
// gateway, without the session check, on a fixed loopback address. It is not
// part of the module image (build.sh builds only cmd/notepad-web).
package main

import (
	"flag"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/chicohaager/lintux-modkit/webmod"
	"github.com/chicohaager/zima-notepad/internal/api"
	"github.com/chicohaager/zima-notepad/internal/files"
)

func main() {
	root := flag.String("root", "", "folder that plays /DATA (required)")
	web := flag.String("web", "", "folder with the built UI (required)")
	addr := flag.String("addr", "127.0.0.1:8471", "listen address (loopback only)")
	flag.Parse()
	if *root == "" || *web == "" {
		log.Fatal("usage: notepad-dev -root <dir> -web <dir> [-addr 127.0.0.1:8471]")
	}
	if !strings.HasPrefix(*addr, "127.0.0.1:") {
		log.Fatal("notepad-dev has no authentication: listen on 127.0.0.1 only")
	}
	r, err := files.NewRoot(*root)
	if err != nil {
		log.Fatal(err)
	}
	noAuth := func(h http.Handler) http.Handler { return h }
	h := webmod.Server(api.Options(r, "dev"), noAuth, *web)
	log.Printf("notepad-dev: http://%s/modules/notepad/ (root %s, NO authentication)", *addr, r.Dir)
	srv := &http.Server{Addr: *addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
