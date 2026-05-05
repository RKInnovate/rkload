package importer

import (
	"fmt"

	"github.com/RKInnovate/rkload/internal/config"
)

// swagger2 is the minimal subset of the Swagger 2.0 schema rkload needs.
// Swagger 2.0 splits the URL across host/basePath/schemes instead of a
// single servers[] entry, so URL construction differs from OpenAPI 3.
type swagger2 struct {
	Swagger  string                       `json:"swagger"`
	Host     string                       `json:"host"`
	BasePath string                       `json:"basePath"`
	Schemes  []string                     `json:"schemes"`
	Paths    map[string]swagger2PathItem  `json:"paths"`
	Security []map[string][]string        `json:"security"`
}

type swagger2PathItem struct {
	Get     *swagger2Op `json:"get,omitempty"`
	Post    *swagger2Op `json:"post,omitempty"`
	Put     *swagger2Op `json:"put,omitempty"`
	Patch   *swagger2Op `json:"patch,omitempty"`
	Delete  *swagger2Op `json:"delete,omitempty"`
	Head    *swagger2Op `json:"head,omitempty"`
	Options *swagger2Op `json:"options,omitempty"`
}

func (p swagger2PathItem) methodOps() []swagger2MethodOp {
	return []swagger2MethodOp{
		{"GET", p.Get},
		{"POST", p.Post},
		{"PUT", p.Put},
		{"PATCH", p.Patch},
		{"DELETE", p.Delete},
		{"HEAD", p.Head},
		{"OPTIONS", p.Options},
	}
}

type swagger2MethodOp struct {
	method string
	op     *swagger2Op
}

type swagger2Op struct {
	OperationID string                `json:"operationId"`
	Summary     string                `json:"summary"`
	Tags        []string              `json:"tags"`
	Security    []map[string][]string `json:"security"`
	// Swagger 2 puts the request body in parameters[] with in:"body".
	// We only care about the example payload, if any.
	Parameters []swagger2Parameter `json:"parameters"`
	Consumes   []string            `json:"consumes"` // for setting Content-Type if a body is present
}

type swagger2Parameter struct {
	In       string      `json:"in"`
	Name     string      `json:"name"`
	Required bool        `json:"required"`
	// Swagger 2 doesn't have a standard "example" field on body params,
	// but `x-example` is a widely-used vendor extension. Both are checked.
	XExample interface{} `json:"x-example"`
	Example  interface{} `json:"example"`
}

func (s *swagger2) toConfig(opts OpenAPIOptions) (*config.Config, error) {
	scheme := "https"
	if len(s.Schemes) > 0 {
		scheme = s.Schemes[0]
	}
	if s.Host == "" {
		return nil, fmt.Errorf("importer: swagger 2.0 spec has empty host — cannot construct full URLs")
	}
	base := scheme + "://" + s.Host + s.BasePath

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

			sec := mo.op.Security
			if sec == nil {
				sec = s.Security
			}
			if len(sec) > 0 {
				ensureHeaders(&ep)["Authorization"] = AuthPlaceholder
			}

			if body, mediaType := bodyFromSwagger2Op(mo.op); body != "" {
				ensureHeaders(&ep)["Content-Type"] = mediaType
				ep.Body = body
			}

			appendEndpoint(cfg, mo.method, ep)
		}
	}
	return cfg, nil
}

// bodyFromSwagger2Op finds the in:"body" parameter (if any) and returns
// its example as a JSON string plus the Content-Type to advertise. The
// Content-Type uses the operation's first `consumes` entry or falls
// back to application/json.
func bodyFromSwagger2Op(op *swagger2Op) (body, mediaType string) {
	for _, p := range op.Parameters {
		if p.In != "body" {
			continue
		}
		ex := p.Example
		if ex == nil {
			ex = p.XExample
		}
		if ex == nil {
			continue
		}
		mt := "application/json"
		if len(op.Consumes) > 0 {
			mt = op.Consumes[0]
		}
		return jsonExampleBody(ex), mt
	}
	return "", ""
}
