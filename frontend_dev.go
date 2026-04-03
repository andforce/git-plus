//go:build !embed

package main

import (
	"net/http"

	"github.com/ImSingee/git-plus/pkg/frontend"
)

func newFrontendHandler() (http.Handler, error) {
	return frontend.NewDevHandler()
}
