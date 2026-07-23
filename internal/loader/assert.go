package loader

import (
	"bytes"
	"fmt"
)

// AssertRule checks a step's response. It mirrors config.AssertRule; the
// loader keeps its own copy to stay free of a config-package import.
type AssertRule struct {
	Type   string // status | body-contains | json-equals
	Equals int    // type=status: expected status code
	Value  string // type=body-contains / json-equals: expected substring / value
	Path   string // type=json-equals: dotted JSON path
}

// runAsserts evaluates every rule against a step's response and returns
// the first failure. A non-nil error marks the step failed and aborts the
// rest of that chain iteration. Error messages deliberately reference the
// rule and its (user-authored) expectation only — never the response
// value or body, which may carry secrets.
func runAsserts(rules []AssertRule, status int, body []byte) error {
	for _, r := range rules {
		if err := applyAssert(r, status, body); err != nil {
			return err
		}
	}
	return nil
}

func applyAssert(r AssertRule, status int, body []byte) error {
	switch r.Type {
	case "status":
		if status != r.Equals {
			return fmt.Errorf("status assertion failed: got %d, want %d", status, r.Equals)
		}
	case "body-contains":
		if !bytes.Contains(body, []byte(r.Value)) {
			return fmt.Errorf("body-contains assertion failed: response does not contain %q", r.Value)
		}
	case "json-equals":
		got, err := jsonPath(body, r.Path)
		if err != nil {
			return fmt.Errorf("json-equals assertion failed at %q: %w", r.Path, err)
		}
		if got != r.Value {
			// Report the path and expected value, not the actual response
			// value, which may be a secret.
			return fmt.Errorf("json-equals assertion failed: value at %q did not equal %q", r.Path, r.Value)
		}
	default:
		return fmt.Errorf("unknown assert type %q", r.Type)
	}
	return nil
}
