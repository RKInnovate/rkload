// Package loader implements the core HTTP load generation engine.
//
// The loader manages a pool of worker goroutines that consume jobs from a
// buffered channel and produce results onto another channel. Concurrency is
// bounded by the number of workers; backpressure is provided implicitly by
// the buffered job channel.
//
// This package is intentionally empty in v0.1.0 — the engine logic still
// lives in cmd/rkload/main.go. Refactoring it into here is the first task
// of v0.2.0.
package loader
