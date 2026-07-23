package loader

import (
	"net/http"
	"testing"
)

// ---- interpolate ---------------------------------------------------------

func TestInterpolate_ResolvesExtractedThenEnv(t *testing.T) {
	t.Setenv("API_TOKEN", "env-secret")
	vars := map[string]string{"token": "abc123"}

	if got := interpolate("Bearer ${token}", vars); got != "Bearer abc123" {
		t.Errorf("extracted var not resolved: %q", got)
	}
	if got := interpolate("key=${API_TOKEN}", vars); got != "key=env-secret" {
		t.Errorf("env fallback not resolved: %q", got)
	}
}

func TestInterpolate_ExtractedShadowsEnv(t *testing.T) {
	t.Setenv("token", "from-env")
	vars := map[string]string{"token": "from-extract"}
	if got := interpolate("${token}", vars); got != "from-extract" {
		t.Errorf("extracted var should shadow env, got %q", got)
	}
}

func TestInterpolate_UnknownLeftVerbatim(t *testing.T) {
	if got := interpolate("${missing}/x", nil); got != "${missing}/x" {
		t.Errorf("unknown placeholder should be left verbatim, got %q", got)
	}
}

func TestInterpolate_MultipleAndNone(t *testing.T) {
	vars := map[string]string{"a": "1", "b": "2"}
	if got := interpolate("${a}-${b}", vars); got != "1-2" {
		t.Errorf("multiple placeholders: %q", got)
	}
	if got := interpolate("no placeholders here", vars); got != "no placeholders here" {
		t.Errorf("plain string changed: %q", got)
	}
}

// ---- jsonPath ------------------------------------------------------------

func TestJSONPath_NestedAndArray(t *testing.T) {
	body := []byte(`{"data":{"items":[{"id":42,"name":"a"},{"id":43}]}}`)

	if got, err := jsonPath(body, "data.items.0.name"); err != nil || got != "a" {
		t.Errorf("nested/array string = %q, err %v", got, err)
	}
	// Integral numbers format without a trailing .0.
	if got, err := jsonPath(body, "data.items.1.id"); err != nil || got != "43" {
		t.Errorf("array int = %q, err %v (want 43)", got, err)
	}
}

func TestJSONPath_Errors(t *testing.T) {
	body := []byte(`{"a":{"b":1}}`)
	if _, err := jsonPath(body, "a.missing"); err == nil {
		t.Error("want missing-key error")
	}
	if _, err := jsonPath([]byte("not json"), "a"); err == nil {
		t.Error("want non-JSON error")
	}
	if _, err := jsonPath(body, "a"); err == nil {
		t.Error("want non-scalar error for descending stop on an object")
	}
}

func TestFormatJSONScalar_Types(t *testing.T) {
	body := []byte(`{"s":"hi","b":true,"n":1.5,"z":null}`)
	for _, tc := range []struct{ path, want string }{
		{"s", "hi"}, {"b", "true"}, {"n", "1.5"}, {"z", ""},
	} {
		if got, err := jsonPath(body, tc.path); err != nil || got != tc.want {
			t.Errorf("path %q = %q (err %v), want %q", tc.path, got, err, tc.want)
		}
	}
}

// ---- applyExtract / runExtracts ------------------------------------------

func TestApplyExtract_HeaderStatusRegex(t *testing.T) {
	h := http.Header{}
	h.Set("X-Request-Id", "req-99")
	body := []byte("order id=7788 created")

	if got, _ := applyExtract(ExtractRule{From: "header", Name: "X-Request-Id"}, 0, h, nil); got != "req-99" {
		t.Errorf("header extract = %q", got)
	}
	if got, _ := applyExtract(ExtractRule{From: "status"}, 201, h, nil); got != "201" {
		t.Errorf("status extract = %q", got)
	}
	if got, _ := applyExtract(ExtractRule{From: "regex", Pattern: `id=(\d+)`}, 0, h, body); got != "7788" {
		t.Errorf("regex extract = %q", got)
	}
	if _, err := applyExtract(ExtractRule{From: "regex", Pattern: `nope=(\d+)`}, 0, h, body); err == nil {
		t.Error("want regex-no-match error")
	}
}

func TestRunExtracts_WritesVars(t *testing.T) {
	body := []byte(`{"data":{"accessToken":"tok"}}`)
	h := http.Header{}
	h.Set("X-Request-Id", "r1")
	vars := map[string]string{}

	err := runExtracts([]ExtractRule{
		{Var: "token", From: "json", Path: "data.accessToken"},
		{Var: "rid", From: "header", Name: "X-Request-Id"},
	}, vars, 200, h, body)
	if err != nil {
		t.Fatalf("runExtracts: %v", err)
	}
	if vars["token"] != "tok" || vars["rid"] != "r1" {
		t.Errorf("vars = %v", vars)
	}
}
