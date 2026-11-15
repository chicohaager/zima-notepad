// Command notepad-web serves the Notepad tile: a text editor for the files
// under /DATA, behind the ZimaOS gateway and session token (lintux-modkit
// webmod).
package main

import (
	"log"
	"os"

	"github.com/chicohaager/lintux-modkit/webmod"
	"github.com/chicohaager/zima-notepad/internal/api"
	"github.com/chicohaager/zima-notepad/internal/files"
)

// version is set at build time from VERSION (-ldflags -X main.version=...).
var version = "dev"

func main() {
	dir := "/DATA"
	// Tests on a ZimaOS host only: the shipped unit does not set it.
	if d := os.Getenv("NOTEPAD_WEB_ROOT"); d != "" {
		dir = d
	}
	root, err := files.NewRoot(dir)
	if err != nil {
		log.Fatalf("[notepad-web] root %s: %v", dir, err)
	}
	webmod.Run(api.Options(root, version))
}
