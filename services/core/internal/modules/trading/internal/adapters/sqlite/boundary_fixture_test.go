package sqlite

import (
	"encoding/json"
	"testing"
)

func jsonKeySet(value map[string]any) map[string]bool {
	result := make(map[string]bool, len(value))
	for key := range value {
		result[key] = true
	}
	return result
}

func jsonStringSet(value any) map[string]bool {
	items := value.([]any)
	result := make(map[string]bool, len(items))
	for _, item := range items {
		result[item.(string)] = true
	}
	return result
}

func decodeFixture(t *testing.T, name string, dst any) {
	t.Helper()
	if err := json.Unmarshal(fixture(t, name), dst); err != nil {
		t.Fatal(err)
	}
}
