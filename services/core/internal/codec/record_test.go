package codec

import (
	"encoding/json"
	"testing"
)

func TestClosedObjectRejectsAmbiguousRecords(t *testing.T) {
	for _, raw := range []string{`{"a":1,"a":2}`, `{"a":1,"b":2}`, `{"a":null}`, `{"a":1} {"a":2}`} {
		if _, err := ClosedObject([]byte(raw), nil, "a"); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	fields, err := ClosedObject([]byte(`{"a":null}`), map[string]bool{"a": true}, "a")
	if err != nil || string(fields["a"]) != "null" {
		t.Fatalf("nullable contract lost: %v", err)
	}
	if ValidJSONSurrogates([]byte(`"\ud800"`)) || ValidJSONSurrogates([]byte(`"\udc00"`)) || !ValidJSONSurrogates([]byte(`"\ud83d\ude00"`)) {
		t.Fatal("Unicode surrogate contract changed")
	}
	if _, _, err := JSONHash(map[string]any{"bad": json.RawMessage("{")}); err == nil {
		t.Fatal("invalid JSON hash accepted")
	}
}
