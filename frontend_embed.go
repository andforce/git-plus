//go:build embed

package main

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/ImSingee/git-plus/pkg/frontend"
)

//go:embed all:frontend/dist
var embeddedFrontendFiles embed.FS

func newFrontendHandler() (http.Handler, error) {
	distFS, err := fs.Sub(embeddedFrontendFiles, "frontend/dist")
	if err != nil {
		return nil, err
	}

	return frontend.NewSPAHandler(distFS), nil
}
