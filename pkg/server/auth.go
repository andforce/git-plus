package server

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
)

const (
	PasswordEnvVar         = "PASSWORD"
	InsecureNoAuthPassword = "insecure-noauth"
)

func apiAuthMiddleware(next http.Handler) http.Handler {
	password := os.Getenv(PasswordEnvVar)
	if password == InsecureNoAuthPassword {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAuthorizedRequest(r, password) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("unauthorized\n"))
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isAuthorizedRequest(r *http.Request, password string) bool {
	if strings.TrimSpace(password) == "" {
		return false
	}

	token := requestAuthToken(r)
	if token == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(token), []byte(password)) == 1
}

func requestAuthToken(r *http.Request) string {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if authorization == "" {
		return ""
	}

	if token, ok := strings.CutPrefix(authorization, "Bearer "); ok {
		return strings.TrimSpace(token)
	}

	if token, ok := strings.CutPrefix(authorization, "bearer "); ok {
		return strings.TrimSpace(token)
	}

	return authorization
}
