//go:build !embed

package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

const defaultFrontendDevServer = "http://127.0.0.1:43210"

func newFrontendHandler() (http.Handler, error) {
	target, err := url.Parse(envOrDefault("FRONTEND_DEV_SERVER", defaultFrontendDevServer))
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
	}

	return proxy, nil
}
