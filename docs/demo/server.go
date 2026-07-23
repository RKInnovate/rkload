//go:build ignore

// Command server is a tiny local target used only to record the README TUI
// demo GIF (see docs/demo/rkload.tape). The //go:build ignore tag keeps it
// out of `go build ./...`, `go vet`, and staticcheck — run it directly with
// `go run docs/demo/server.go`. It serves a handful of endpoints with
// controlled, jittered latencies (and an occasional 429) so the recorded
// dashboard shows a lively spread of progress, latency bands, and colour.
package main

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"time"
)

func jitter(base, span time.Duration) time.Duration {
	return base + time.Duration(rand.Int63n(int64(span)))
}

func main() {
	http.HandleFunc("/fast", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(jitter(5*time.Millisecond, 20*time.Millisecond))
		_, _ = w.Write([]byte("ok"))
	})
	http.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(jitter(120*time.Millisecond, 120*time.Millisecond))
		if rand.Intn(10) == 0 { // ~10% throttled, for a splash of yellow
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	http.HandleFunc("/slower", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(jitter(350*time.Millisecond, 250*time.Millisecond))
		_, _ = w.Write([]byte("ok"))
	})
	http.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(jitter(40*time.Millisecond, 40*time.Millisecond))
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "demo-token"})
	})
	http.HandleFunc("/pay", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(jitter(80*time.Millisecond, 80*time.Millisecond))
		if r.Header.Get("Authorization") != "Bearer demo-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("paid"))
	})
	_ = http.ListenAndServe("127.0.0.1:8799", nil)
}
