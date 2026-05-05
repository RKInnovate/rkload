package report

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"
)

// slowServer returns an httptest.Server whose handler sleeps for `delay`
// before responding. Used to coerce a real http.Client.Timeout error.
func slowServer(delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(delay)
		w.WriteHeader(http.StatusOK)
	}))
}

func TestClassify_Nil(t *testing.T) {
	if got := Classify(nil); got != "" {
		t.Errorf("Classify(nil) = %q, want empty string", got)
	}
}

func TestClassify_ContextDeadline(t *testing.T) {
	if got := Classify(context.DeadlineExceeded); got != ClassTimeout {
		t.Errorf("Classify(DeadlineExceeded) = %q, want %q", got, ClassTimeout)
	}
}

// fakeNetErr satisfies net.Error and reports Timeout()=true. The real
// http.Client surfaces these as *url.Error wrapping a *net.OpError —
// Classify uses errors.As which traverses that chain.
type fakeNetErr struct{ timeout bool }

func (e fakeNetErr) Error() string   { return "fake net error" }
func (e fakeNetErr) Timeout() bool   { return e.timeout }
func (e fakeNetErr) Temporary() bool { return false }

func TestClassify_NetErrorTimeout(t *testing.T) {
	wrapped := fmt.Errorf("Get \"http://x\": %w", fakeNetErr{timeout: true})
	if got := Classify(wrapped); got != ClassTimeout {
		t.Errorf("Classify(net.Error timeout) = %q, want %q", got, ClassTimeout)
	}
}

func TestClassify_ConnRefused(t *testing.T) {
	// Real shape an http.Client returns: url.Error → net.OpError →
	// os.SyscallError → syscall.Errno. errors.Is on syscall.ECONNREFUSED
	// must unwrap the whole chain.
	wrapped := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED},
	}
	if got := Classify(wrapped); got != ClassConnRefused {
		t.Errorf("Classify(ECONNREFUSED chain) = %q, want %q", got, ClassConnRefused)
	}
}

func TestClassify_DNS(t *testing.T) {
	dnsErr := &net.DNSError{Err: "no such host", Name: "nope.invalid"}
	if got := Classify(dnsErr); got != ClassDNS {
		t.Errorf("Classify(DNSError) = %q, want %q", got, ClassDNS)
	}

	wrapped := fmt.Errorf("dial tcp: lookup nope.invalid: %w", dnsErr)
	if got := Classify(wrapped); got != ClassDNS {
		t.Errorf("Classify(wrapped DNSError) = %q, want %q", got, ClassDNS)
	}
}

func TestClassify_TLS(t *testing.T) {
	cases := []error{
		errors.New("tls: handshake failure"),
		errors.New("x509: certificate signed by unknown authority"),
		fmt.Errorf("Get \"https://x\": remote error: %w", errors.New("tls: bad record MAC")),
	}
	for _, err := range cases {
		if got := Classify(err); got != ClassTLS {
			t.Errorf("Classify(%v) = %q, want %q", err, got, ClassTLS)
		}
	}
}

// TestClassify_TimeoutBeforeConnRefused: an error that satisfies BOTH
// Timeout() and the syscall chain should still classify as timeout —
// timeouts are the user-facing concern; the fact that we got there via
// some syscall is incidental.
func TestClassify_TimeoutBeforeConnRefused(t *testing.T) {
	// fakeNetErr timeout=true; even if we wrapped ECONNREFUSED inside,
	// the timeout check fires first by design.
	got := Classify(fakeNetErr{timeout: true})
	if got != ClassTimeout {
		t.Errorf("Classify priority = %q, want %q", got, ClassTimeout)
	}
}

func TestClassify_Other(t *testing.T) {
	if got := Classify(errors.New("something weird")); got != ClassOther {
		t.Errorf("Classify(unknown) = %q, want %q", got, ClassOther)
	}
}

// Real HTTP timeout — uses an httptest server that hangs longer than
// the client timeout. Verifies Classify sees what http.Client actually
// returns in production.
func TestClassify_RealHTTPTimeout(t *testing.T) {
	// Block the handler past the client's context-derived deadline.
	srv := slowServer(50 * time.Millisecond)
	defer srv.Close()

	client := &http.Client{Timeout: 10 * time.Millisecond}
	_, err := client.Get(srv.URL)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if got := Classify(err); got != ClassTimeout {
		t.Errorf("Classify(real timeout) = %q, want %q (err=%v)", got, ClassTimeout, err)
	}
}
