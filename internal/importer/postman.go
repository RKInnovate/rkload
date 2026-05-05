package importer

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/RKInnovate/rkload/internal/config"
)

// PostmanOptions controls how Postman() converts a collection to a
// Config. Mirrors OpenAPIOptions where the concept overlaps; absent
// fields fall back to config package defaults.
type PostmanOptions struct {
	DefaultConcurrency int
	DefaultRequests    int
	DefaultTimeout     string
	PathPrefix         string            // substring match against the full URL (Postman has no path-only key)
	Vars               map[string]string // user-supplied {{var}} overrides; merged on top of collection variables
}

func (o *PostmanOptions) fillDefaults() {
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

// Postman converts a Postman Collection v2.1 (JSON) into a rkload
// Config. Folder nesting is flattened during the walk so the
// resulting Config is the same flat shape regardless of how the
// source collection was organised.
func Postman(r io.Reader, opts PostmanOptions) (*config.Config, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("importer: reading collection: %w", err)
	}
	opts.fillDefaults()

	var col postmanCollection
	if err := json.Unmarshal(data, &col); err != nil {
		return nil, fmt.Errorf("importer: parsing Postman collection: %w", err)
	}
	if !strings.Contains(col.Info.Schema, "v2.1") {
		return nil, fmt.Errorf("importer: only Postman Collection v2.1 is supported (got %q)", col.Info.Schema)
	}

	// Variable resolution: collection-level vars first, then user-supplied
	// overrides take precedence. Unknown {{var}} references are left as-is
	// so users can grep them in the generated config.
	vars := make(map[string]string, len(col.Variable)+len(opts.Vars))
	for _, v := range col.Variable {
		vars[v.Key] = v.Value
	}
	for k, v := range opts.Vars {
		vars[k] = v
	}

	cfg := &config.Config{
		Schema:  SchemaURL,
		Version: config.SchemaVersion,
	}

	walkPostmanItems(col.Item, vars, opts, cfg)
	return cfg, nil
}

// walkPostmanItems recursively flattens Postman folders into endpoints.
// A folder is any item with non-empty Item[]; a request is any item
// with a non-nil Request. The two are not strictly mutually exclusive
// in the schema, so we handle both on the same item.
func walkPostmanItems(items []postmanItem, vars map[string]string, opts PostmanOptions, cfg *config.Config) {
	for _, it := range items {
		if it.Request != nil {
			ep, method, ok := postmanItemToEndpoint(it, vars, opts)
			if ok {
				appendEndpoint(cfg, method, ep)
			}
		}
		if len(it.Item) > 0 {
			walkPostmanItems(it.Item, vars, opts, cfg)
		}
	}
}

// postmanItemToEndpoint converts one Postman item into a rkload
// Endpoint. Returns ok=false if the item is filtered out (e.g. by
// PathPrefix) so the caller can skip it cleanly.
func postmanItemToEndpoint(it postmanItem, vars map[string]string, opts PostmanOptions) (config.Endpoint, string, bool) {
	req := it.Request
	url := substituteVars(req.URL.canonical(), vars)

	if opts.PathPrefix != "" && !strings.Contains(url, opts.PathPrefix) {
		return config.Endpoint{}, "", false
	}

	ep := config.Endpoint{
		Name:        clampName(it.Name),
		URL:         url,
		Concurrency: opts.DefaultConcurrency,
		Requests:    opts.DefaultRequests,
		Timeout:     opts.DefaultTimeout,
	}

	for _, h := range req.Header {
		if h.Disabled || h.Key == "" {
			continue
		}
		ensureHeaders(&ep)[h.Key] = substituteVars(h.Value, vars)
	}

	if req.Body != nil && req.Body.Mode == "raw" && req.Body.Raw != "" {
		ep.Body = substituteVars(req.Body.Raw, vars)
	}

	return ep, strings.ToUpper(req.Method), true
}

// substituteVars expands {{key}} placeholders. Unknown keys survive
// unchanged so users can grep for them. Cheap O(n*m) string-replace
// loop is fine — collection vars number in the tens at most.
func substituteVars(s string, vars map[string]string) string {
	if !strings.Contains(s, "{{") {
		return s
	}
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
	}
	return s
}

// -- minimal Postman v2.1 schema -----------------------------------

type postmanCollection struct {
	Info     postmanInfo   `json:"info"`
	Item     []postmanItem `json:"item"`
	Variable []postmanVar  `json:"variable"`
}

type postmanInfo struct {
	Name   string `json:"name"`
	Schema string `json:"schema"` // identifies the collection format version
}

// postmanItem is dual-shaped — a folder (Item populated, Request nil)
// or a request (Request populated, Item nil/empty). Both fields are
// modelled so the recursive walker can branch on which is set.
type postmanItem struct {
	Name    string          `json:"name"`
	Item    []postmanItem   `json:"item,omitempty"`
	Request *postmanRequest `json:"request,omitempty"`
}

type postmanRequest struct {
	Method string          `json:"method"`
	Header []postmanHeader `json:"header"`
	URL    postmanURL      `json:"url"`
	Body   *postmanBody    `json:"body,omitempty"`
}

type postmanHeader struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Disabled bool   `json:"disabled,omitempty"`
}

// postmanBody covers "raw" mode only. formdata, urlencoded, and file
// modes are documented as out of scope; they'd need richer Endpoint
// support before being worth extracting.
type postmanBody struct {
	Mode string `json:"mode"`
	Raw  string `json:"raw,omitempty"`
}

type postmanVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// postmanURL handles the Postman v2.1 quirk: url can be either a
// plain string OR an object with raw + host[] + path[]. Custom
// UnmarshalJSON normalises both into the same struct.
type postmanURL struct {
	Raw  string
	Host []string
	Path []string
}

func (u *postmanURL) UnmarshalJSON(data []byte) error {
	// Try string form first — it's the common-case shortcut.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		u.Raw = s
		return nil
	}
	var obj struct {
		Raw  string   `json:"raw"`
		Host []string `json:"host"`
		Path []string `json:"path"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	u.Raw = obj.Raw
	u.Host = obj.Host
	u.Path = obj.Path
	return nil
}

// canonical returns the URL string to use, preferring Raw and falling
// back to host+path reconstruction if Raw is missing.
func (u *postmanURL) canonical() string {
	if u.Raw != "" {
		return u.Raw
	}
	host := strings.Join(u.Host, ".")
	path := strings.Join(u.Path, "/")
	if host == "" {
		return path
	}
	return host + "/" + path
}
