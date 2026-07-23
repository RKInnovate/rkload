package loader

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// ExtractRule pulls a value out of a step's response and binds it to a
// variable usable as ${var} in later steps of the same chain. It mirrors
// config.ExtractRule; the loader keeps its own copy so the engine stays
// free of a config-package import (see the Options/Endpoint split).
type ExtractRule struct {
	Var     string
	From    string // json | header | status | regex
	Path    string // from=json: dotted path, e.g. data.items.0.id
	Name    string // from=header: header name
	Pattern string // from=regex: first capture group is bound
}

// interpolateRe matches ${name} placeholders. The name is taken verbatim
// (any non-"}" run) so keys like ${API_TOKEN} and ${token} both resolve.
var interpolateRe = regexp.MustCompile(`\$\{([^}]+)\}`)

// interpolate resolves every ${name} in s against vars first, then the
// process environment. Extracted variables therefore shadow environment
// variables of the same name. An unresolved placeholder is left verbatim
// so the problem is visible rather than silently blanked.
func interpolate(s string, vars map[string]string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	return interpolateRe.ReplaceAllStringFunc(s, func(m string) string {
		key := interpolateRe.FindStringSubmatch(m)[1]
		if v, ok := vars[key]; ok {
			return v
		}
		if v, ok := os.LookupEnv(key); ok {
			return v
		}
		return m
	})
}

// runExtracts applies every rule against a step's response and writes the
// results into vars. The first failing rule aborts and its error is
// returned (which in turn fails the step).
func runExtracts(rules []ExtractRule, vars map[string]string, status int, header http.Header, body []byte) error {
	for _, r := range rules {
		v, err := applyExtract(r, status, header, body)
		if err != nil {
			return fmt.Errorf("extract %q: %w", r.Var, err)
		}
		vars[r.Var] = v
	}
	return nil
}

// applyExtract reads a single value from a response per the rule's source.
func applyExtract(r ExtractRule, status int, header http.Header, body []byte) (string, error) {
	switch r.From {
	case "json":
		return jsonPath(body, r.Path)
	case "header":
		return header.Get(r.Name), nil
	case "status":
		return strconv.Itoa(status), nil
	case "regex":
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return "", fmt.Errorf("invalid regex %q: %w", r.Pattern, err)
		}
		m := re.FindSubmatch(body)
		if m == nil {
			return "", fmt.Errorf("regex %q did not match the response body", r.Pattern)
		}
		if len(m) < 2 {
			return "", fmt.Errorf("regex %q has no capture group to bind", r.Pattern)
		}
		return string(m[1]), nil
	default:
		return "", fmt.Errorf("unknown extract source %q", r.From)
	}
}

// jsonPath walks a dotted path (map keys and array indices) into a JSON
// body and returns the addressed scalar as a string. Example paths:
// "data.accessToken", "items.0.id". It is shared with json-equals
// assertions so both read the body the same way.
func jsonPath(body []byte, path string) (string, error) {
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("response body is not JSON: %w", err)
	}
	cur := doc
	for _, seg := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[seg]
			if !ok {
				return "", fmt.Errorf("json path %q: key %q not found", path, seg)
			}
			cur = v
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil {
				return "", fmt.Errorf("json path %q: %q is not an array index", path, seg)
			}
			if idx < 0 || idx >= len(node) {
				return "", fmt.Errorf("json path %q: index %d out of range (len %d)", path, idx, len(node))
			}
			cur = node[idx]
		default:
			return "", fmt.Errorf("json path %q: cannot descend into %q", path, seg)
		}
	}
	return formatJSONScalar(cur, path)
}

// formatJSONScalar renders a leaf JSON value as a string. Integral numbers
// format without a trailing ".0" (200, not 200.0) so status-like values
// compare cleanly.
func formatJSONScalar(v any, path string) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case bool:
		return strconv.FormatBool(t), nil
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), nil
	case nil:
		return "", nil
	default:
		return "", fmt.Errorf("json path %q resolves to a non-scalar value", path)
	}
}
