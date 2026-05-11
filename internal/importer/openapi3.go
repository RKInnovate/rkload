package importer

import (
	"fmt"

	"github.com/RKInnovate/rkload/internal/config"
)

// openAPI3 is the minimal subset of the OpenAPI 3.x schema rkload needs.
// We deliberately do not model components, parameters, responses, or
// schema definitions — they don't affect what request the load tester
// will send.
type openAPI3 struct {
	OpenAPI  string                      `json:"openapi"`
	Servers  []openAPI3Server            `json:"servers"`
	Paths    map[string]openAPI3PathItem `json:"paths"`
	Security []map[string][]string       `json:"security"` // global; per-op overrides this
}

type openAPI3Server struct {
	URL string `json:"url"`
}

// openAPI3PathItem models the supported HTTP methods explicitly. Any
// other path-level fields (parameters, summary, description, etc.) are
// dropped on the floor — they don't influence the generated endpoints.
type openAPI3PathItem struct {
	Get     *openAPI3Op `json:"get,omitempty"`
	Post    *openAPI3Op `json:"post,omitempty"`
	Put     *openAPI3Op `json:"put,omitempty"`
	Patch   *openAPI3Op `json:"patch,omitempty"`
	Delete  *openAPI3Op `json:"delete,omitempty"`
	Head    *openAPI3Op `json:"head,omitempty"`
	Options *openAPI3Op `json:"options,omitempty"`
}

// methodOps returns (uppercase-method, op) pairs in a stable order so
// generated configs match across runs.
func (p openAPI3PathItem) methodOps() []openAPI3MethodOp {
	return []openAPI3MethodOp{
		{"GET", p.Get},
		{"POST", p.Post},
		{"PUT", p.Put},
		{"PATCH", p.Patch},
		{"DELETE", p.Delete},
		{"HEAD", p.Head},
		{"OPTIONS", p.Options},
	}
}

type openAPI3MethodOp struct {
	method string
	op     *openAPI3Op
}

type openAPI3Op struct {
	OperationID string                `json:"operationId"`
	Summary     string                `json:"summary"`
	Tags        []string              `json:"tags"`
	Security    []map[string][]string `json:"security"` // nil means "inherit global"
	RequestBody *openAPI3RequestBody  `json:"requestBody"`
}

type openAPI3RequestBody struct {
	Content map[string]openAPI3MediaType `json:"content"`
}

type openAPI3MediaType struct {
	Example interface{} `json:"example"`
}

// toConfig converts the parsed spec into a rkload Config under the
// supplied OpenAPIOptions. Iteration is deterministic (paths sorted
// lexically, methods in the order returned by methodOps), so two runs
// of the importer on the same spec produce byte-identical output.
func (s *openAPI3) toConfig(opts OpenAPIOptions) (*config.Config, error) {
	if len(s.Servers) == 0 {
		return nil, fmt.Errorf("importer: openapi spec has no servers[] — cannot construct full URLs")
	}
	base := s.Servers[0].URL
	if base == "" {
		return nil, fmt.Errorf("importer: openapi spec servers[0].url is empty")
	}

	cfg := &config.Config{
		Schema:  SchemaURL,
		Version: config.SchemaVersion,
	}

	for _, path := range sortedKeys(s.Paths) {
		if opts.PathPrefix != "" && !pathMatchesPrefix(path, opts.PathPrefix) {
			continue
		}
		item := s.Paths[path]
		for _, mo := range item.methodOps() {
			if mo.op == nil {
				continue
			}
			if opts.TagFilter != "" && !contains(mo.op.Tags, opts.TagFilter) {
				continue
			}

			ep := config.Endpoint{
				Name:        endpointName(mo.op.OperationID, mo.method, path),
				URL:         joinURL(base, path),
				Concurrency: opts.DefaultConcurrency,
				Requests:    opts.DefaultRequests,
				Timeout:     opts.DefaultTimeout,
			}

			// Effective security: per-op when set, otherwise global.
			sec := mo.op.Security
			if sec == nil {
				sec = s.Security
			}
			if len(sec) > 0 {
				ensureHeaders(&ep)["Authorization"] = AuthPlaceholder
			}

			if body := jsonExampleFromOp3(mo.op); body != "" {
				ensureHeaders(&ep)["Content-Type"] = "application/json"
				ep.Body = body
			}

			appendEndpoint(cfg, mo.method, ep)
		}
	}
	return cfg, nil
}

// jsonExampleFromOp3 pulls a JSON-encoded example body out of an
// OpenAPI 3 operation, if one exists. Falls back to "" so the caller
// can decide whether to emit a Body / Content-Type pair.
func jsonExampleFromOp3(op *openAPI3Op) string {
	if op.RequestBody == nil {
		return ""
	}
	mt, ok := op.RequestBody.Content["application/json"]
	if !ok {
		return ""
	}
	return jsonExampleBody(mt.Example)
}

// ensureHeaders lazily allocates the Headers map. Avoids the
// `if ep.Headers == nil { ep.Headers = …}` boilerplate at every call
// site that wants to add a header.
func ensureHeaders(ep *config.Endpoint) map[string]string {
	if ep.Headers == nil {
		ep.Headers = make(map[string]string)
	}
	return ep.Headers
}

// pathMatchesPrefix is the prefix check for --path-prefix. Pulled out
// so future smarts (e.g. matching "/{anything}/users" against
// "/v1/users") can plug in without touching toConfig.
func pathMatchesPrefix(path, prefix string) bool {
	return len(path) >= len(prefix) && path[:len(prefix)] == prefix
}
