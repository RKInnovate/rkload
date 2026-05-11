// Package importer generates rkload Configs from external API
// specifications: OpenAPI 3.x, Swagger 2.0, and Postman Collection v2.1.
//
// Importers are intentionally permissive about input quality. They extract
// the bits a load test needs (URL, method, request body example, common
// auth header placeholder) and skip everything else (response schemas,
// validation rules, generated code hints). Path templates such as
// /users/{id} are emitted verbatim — substituting them would require
// guessing values, and a wrong guess is worse than a clear placeholder
// the user can grep and edit.
package importer

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/RKInnovate/rkload/internal/config"
)

// SchemaURL is the canonical $schema URL stamped onto generated configs.
// Pinning it explicitly means the editor immediately starts validating
// the output without the user adding a $schema field by hand.
const SchemaURL = "https://raw.githubusercontent.com/RKInnovate/rkload/main/schemas/v1/config.schema.json"

// AuthPlaceholder is emitted as the Authorization header value when a
// spec advertises a security requirement. We can't extract real tokens
// from a spec — this placeholder is meant to be grep-able so users can
// fix every endpoint at once.
const AuthPlaceholder = "REPLACE_ME"

// OpenAPIOptions controls how OpenAPI() converts a spec to a Config.
type OpenAPIOptions struct {
	DefaultConcurrency int    // applied to every endpoint (defaults to config.DefaultConcurrency if 0)
	DefaultRequests    int    // applied to every endpoint (defaults to config.DefaultRequests if 0)
	DefaultTimeout     string // applied to every endpoint (defaults to config.DefaultTimeout if empty)
	TagFilter          string // include only operations whose tags contain this value (empty = no filter)
	PathPrefix         string // include only paths starting with this prefix (empty = no filter)

	// ServerURL overrides the base URL entirely, ignoring servers[]
	// (OpenAPI 3) or host/basePath/schemes (Swagger 2). Most useful
	// when the spec lists a development server first and the user
	// wants to point the generated config at production.
	ServerURL string

	// ServerIndex selects which entry of servers[] to use for
	// OpenAPI 3 specs. Defaults to 0 (the conventional choice).
	// Ignored for Swagger 2 and when ServerURL is set.
	ServerIndex int
}

// OpenAPI converts an OpenAPI 3.x or Swagger 2.0 spec (JSON or YAML)
// into a rkload Config. Format and dialect are both auto-detected:
// YAML inputs are converted to JSON in-memory before parsing, so the
// downstream parsers only ever see canonical JSON.
func OpenAPI(r io.Reader, opts OpenAPIOptions) (*config.Config, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("importer: reading spec: %w", err)
	}
	opts.fillDefaults()

	if !looksLikeJSON(data) {
		data, err = yamlToJSON(data)
		if err != nil {
			return nil, fmt.Errorf("importer: parsing YAML spec: %w", err)
		}
	}

	dialect, err := detectDialect(data)
	if err != nil {
		return nil, err
	}

	switch dialect {
	case "openapi3":
		var s openAPI3
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("importer: parsing OpenAPI 3 spec: %w", err)
		}
		return s.toConfig(opts)
	case "swagger2":
		var s swagger2
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("importer: parsing Swagger 2 spec: %w", err)
		}
		return s.toConfig(opts)
	default:
		return nil, fmt.Errorf("importer: unknown dialect %q", dialect)
	}
}

func (o *OpenAPIOptions) fillDefaults() {
	if o.DefaultConcurrency == 0 {
		o.DefaultConcurrency = config.DefaultConcurrency
	}
	if o.DefaultRequests == 0 {
		o.DefaultRequests = config.DefaultRequests
	}
	if o.DefaultTimeout == "" {
		o.DefaultTimeout = config.DefaultTimeout
	}
}

// detectDialect inspects the top-level keys of a JSON spec to decide
// which parser to use. Cheap (one tiny unmarshal) and always done before
// the full parse so we fail fast on garbage input.
func detectDialect(data []byte) (string, error) {
	var head struct {
		OpenAPI string `json:"openapi"`
		Swagger string `json:"swagger"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return "", fmt.Errorf("importer: spec is not valid JSON: %w", err)
	}
	if head.OpenAPI != "" {
		if !strings.HasPrefix(head.OpenAPI, "3.") {
			return "", fmt.Errorf("importer: unsupported OpenAPI version %q (need 3.x)", head.OpenAPI)
		}
		return "openapi3", nil
	}
	if head.Swagger != "" {
		if head.Swagger != "2.0" {
			return "", fmt.Errorf("importer: unsupported Swagger version %q (need 2.0)", head.Swagger)
		}
		return "swagger2", nil
	}
	return "", fmt.Errorf(`importer: spec missing both "openapi" and "swagger" top-level keys; not a recognised API specification`)
}

// joinURL stitches a server URL onto a path without producing
// duplicate slashes, regardless of which side carries the trailing/
// leading slash.
func joinURL(base, path string) string {
	return strings.TrimSuffix(base, "/") + "/" + strings.TrimPrefix(path, "/")
}

// endpointName picks a human-friendly label for an endpoint. We prefer
// the spec-supplied operationId because that's what the spec author
// chose to call the operation; falling back to method+path keeps the
// output legible for specs that omit operationId.
func endpointName(opID, method, path string) string {
	if opID != "" {
		return clampName(opID)
	}
	name := strings.ToLower(method) + path
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, "{", "")
	name = strings.ReplaceAll(name, "}", "")
	name = strings.Trim(name, "-")
	return clampName(name)
}

// clampName enforces the schema's 80-char limit on Endpoint.Name.
func clampName(s string) string {
	const max = 80
	if len(s) > max {
		return s[:max]
	}
	return s
}

// appendEndpoint slots the endpoint into the right method group on cfg.
// Centralising the switch here means callers don't repeat it per
// dialect.
func appendEndpoint(cfg *config.Config, method string, ep config.Endpoint) {
	switch method {
	case "GET":
		cfg.GET = append(cfg.GET, ep)
	case "POST":
		cfg.POST = append(cfg.POST, ep)
	case "PUT":
		cfg.PUT = append(cfg.PUT, ep)
	case "PATCH":
		cfg.PATCH = append(cfg.PATCH, ep)
	case "DELETE":
		cfg.DELETE = append(cfg.DELETE, ep)
	case "HEAD":
		cfg.HEAD = append(cfg.HEAD, ep)
	case "OPTIONS":
		cfg.OPTIONS = append(cfg.OPTIONS, ep)
	}
}

// jsonExampleBody renders an example payload as a JSON string suitable
// for Endpoint.Body. Strings pass through; everything else is JSON-
// encoded. nil returns "".
func jsonExampleBody(example interface{}) string {
	if example == nil {
		return ""
	}
	if s, ok := example.(string); ok {
		return s
	}
	b, err := json.Marshal(example)
	if err != nil {
		return ""
	}
	return string(b)
}

// sortedKeys returns the keys of m in lexical order so generated
// configs are byte-for-byte stable across runs of the importer.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// contains reports whether s appears in xs. Used for tag matching.
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// looksLikeJSON peeks at the first non-whitespace byte to decide whether
// to treat the input as JSON or YAML. Cheaper and more robust than file-
// extension sniffing — users sometimes pipe specs through stdin.
func looksLikeJSON(data []byte) bool {
	for _, b := range data {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		case '{', '[':
			return true
		default:
			return false
		}
	}
	return false
}

// yamlToJSON parses YAML and re-encodes as JSON. The two-pass approach
// keeps every spec struct in importer/ tagged with json: only — yaml.v3
// is used purely to canonicalise the input.
func yamlToJSON(data []byte) ([]byte, error) {
	var generic interface{}
	if err := yaml.Unmarshal(data, &generic); err != nil {
		return nil, err
	}
	return json.Marshal(generic)
}
