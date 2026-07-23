package loader

import (
	"strings"
	"testing"
)

func TestApplyAssert_Status(t *testing.T) {
	if err := applyAssert(AssertRule{Type: "status", Equals: 200}, 200, nil); err != nil {
		t.Errorf("matching status should pass, got %v", err)
	}
	err := applyAssert(AssertRule{Type: "status", Equals: 200}, 500, nil)
	if err == nil || !strings.Contains(err.Error(), "got 500, want 200") {
		t.Errorf("want status-mismatch error, got %v", err)
	}
}

func TestApplyAssert_BodyContains(t *testing.T) {
	body := []byte(`{"accessToken":"abc"}`)
	if err := applyAssert(AssertRule{Type: "body-contains", Value: "accessToken"}, 200, body); err != nil {
		t.Errorf("substring present should pass, got %v", err)
	}
	err := applyAssert(AssertRule{Type: "body-contains", Value: "refreshToken"}, 200, body)
	if err == nil || !strings.Contains(err.Error(), "does not contain") {
		t.Errorf("want body-contains failure, got %v", err)
	}
}

func TestApplyAssert_JSONEquals(t *testing.T) {
	body := []byte(`{"data":{"tokenType":"Bearer"}}`)
	if err := applyAssert(AssertRule{Type: "json-equals", Path: "data.tokenType", Value: "Bearer"}, 200, body); err != nil {
		t.Errorf("matching json value should pass, got %v", err)
	}
	err := applyAssert(AssertRule{Type: "json-equals", Path: "data.tokenType", Value: "Basic"}, 200, body)
	if err == nil || !strings.Contains(err.Error(), `did not equal "Basic"`) {
		t.Errorf("want json-equals mismatch, got %v", err)
	}
}

// The response value must never appear in the failure message (secrets).
func TestApplyAssert_JSONEqualsDoesNotLeakValue(t *testing.T) {
	body := []byte(`{"secret":"super-secret-token"}`)
	err := applyAssert(AssertRule{Type: "json-equals", Path: "secret", Value: "expected"}, 200, body)
	if err == nil {
		t.Fatal("want mismatch error")
	}
	if strings.Contains(err.Error(), "super-secret-token") {
		t.Errorf("assertion error leaked the response value: %v", err)
	}
}

func TestApplyAssert_JSONEqualsPathError(t *testing.T) {
	err := applyAssert(AssertRule{Type: "json-equals", Path: "missing", Value: "x"}, 200, []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "json-equals assertion failed") {
		t.Errorf("want path error wrapped as json-equals failure, got %v", err)
	}
}

func TestRunAsserts_StopsAtFirstFailure(t *testing.T) {
	body := []byte(`ok`)
	err := runAsserts([]AssertRule{
		{Type: "status", Equals: 200},
		{Type: "body-contains", Value: "MISSING"},
		{Type: "status", Equals: 999}, // would also fail, but we stop before it
	}, 200, body)
	if err == nil || !strings.Contains(err.Error(), "body-contains") {
		t.Errorf("want first failure (body-contains), got %v", err)
	}
}
