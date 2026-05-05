package report

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"
)

// ErrorClass is a coarse bucket for failed requests.
//
// A single string per failed request, picked from a small fixed set,
// is enough to tell "the API is rate-limiting us" from "DNS is broken"
// in a load test report — without dragging the full error string
// through aggregation.
type ErrorClass string

const (
	ClassTimeout     ErrorClass = "timeout"
	ClassConnRefused ErrorClass = "connection refused"
	ClassDNS         ErrorClass = "DNS"
	ClassTLS         ErrorClass = "TLS"
	ClassOther       ErrorClass = "other"
)

// classOrder is the stable rendering order for ErrorClass values.
// Print iterates this rather than ranging the map so output is
// deterministic across runs.
var classOrder = []ErrorClass{
	ClassTimeout,
	ClassConnRefused,
	ClassDNS,
	ClassTLS,
	ClassOther,
}

// Classify maps a request error to one of the ErrorClass buckets.
// nil returns the empty string.
func Classify(err error) ErrorClass {
	if err == nil {
		return ""
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return ClassTimeout
	}
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return ClassTimeout
	}

	// syscall.ECONNREFUSED is defined on both Unix and Windows; errors.Is
	// unwraps the net.OpError → os.SyscallError → syscall.Errno chain.
	if errors.Is(err, syscall.ECONNREFUSED) {
		return ClassConnRefused
	}

	var dnserr *net.DNSError
	if errors.As(err, &dnserr) {
		return ClassDNS
	}

	// TLS / x509 failures surface under several concrete types
	// (tls.RecordHeaderError, x509.UnknownAuthorityError, …) with no
	// shared sentinel. Matching the "tls:" / "x509:" prefixes in the
	// formatted error is the pragmatic catch-all.
	msg := err.Error()
	if strings.Contains(msg, "tls:") || strings.Contains(msg, "x509:") {
		return ClassTLS
	}

	return ClassOther
}
