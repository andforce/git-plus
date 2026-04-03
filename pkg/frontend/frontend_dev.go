package frontend

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
)

const defaultFrontendDevServer = "http://127.0.0.1:43210"

func NewDevHandler() (http.Handler, error) {
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

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
