package config

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
)

// readSchemaDoc reads and JSON-decodes a published schema file. The
// schema files are editor-facing (the runtime validates in Go), so these
// tests are the only thing that catches a stray trailing comma or a
// version/path drift before it ships.
func readSchemaDoc(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%s is not valid JSON: %v", path, err)
	}
	return doc
}

func TestSchemaFiles_WellFormedAndConsistent(t *testing.T) {
	cases := []struct {
		path    string
		version float64 // JSON numbers decode to float64
	}{
		{"../../schemas/v1/config.schema.json", 1},
		{"../../schemas/v2/config.schema.json", 2},
	}
	for _, tc := range cases {
		doc := readSchemaDoc(t, tc.path)

		if doc["additionalProperties"] != false {
			t.Errorf("%s: top-level additionalProperties should be false", tc.path)
		}
		props, _ := doc["properties"].(map[string]any)
		ver, _ := props["version"].(map[string]any)
		if ver["const"] != tc.version {
			t.Errorf("%s: version const = %v, want %v", tc.path, ver["const"], tc.version)
		}
		id, _ := doc["$id"].(string)
		if want := "/schemas/v" + strconv.Itoa(int(tc.version)) + "/"; !strings.Contains(id, want) {
			t.Errorf("%s: $id %q does not pin %s", tc.path, id, want)
		}
	}
}

// TestSchemaV2_IsSupersetOfV1 guards the superset invariant: v1 must stay
// frozen without scenarios, and v2 must add the top-level scenarios array.
func TestSchemaV2_IsSupersetOfV1(t *testing.T) {
	v1Props, _ := readSchemaDoc(t, "../../schemas/v1/config.schema.json")["properties"].(map[string]any)
	v2Props, _ := readSchemaDoc(t, "../../schemas/v2/config.schema.json")["properties"].(map[string]any)

	if _, ok := v1Props["scenarios"]; ok {
		t.Error("v1 schema must not declare scenarios (it is frozen)")
	}
	if _, ok := v2Props["scenarios"]; !ok {
		t.Error("v2 schema must declare a top-level scenarios property")
	}
	// Every v1 top-level method key must still exist in v2 (superset).
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} {
		if _, ok := v2Props[m]; !ok {
			t.Errorf("v2 schema dropped method %q (must be a superset of v1)", m)
		}
	}
}
