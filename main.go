package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

const defaultListenAddr = ":8080"

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	frontendHandler, err := newFrontendHandler()
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/test", apiTestHandler)
	mux.HandleFunc("/api", notFoundAPIHandler)
	mux.HandleFunc("/api/", notFoundAPIHandler)
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/ready", healthzHandler)
	mux.Handle("/", frontendHandler)

	listenAddr := envOrDefault("PORT", defaultListenAddr)
	log.Printf("listening on http://localhost%s", listenAddr)

	return http.ListenAndServe(listenAddr, mux)
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

func notFoundAPIHandler(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func apiTestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]bool{
		"ok": true,
	})
}
