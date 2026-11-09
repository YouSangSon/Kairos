package files

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPublicResearchClosedArtifactBoundary(t *testing.T) {
	valid := researchFixture(t)
	if _, err := decodePublicResearch(valid); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"unknown root":    func(v map[string]any) { v["secret"] = "no" },
		"version":         func(v map[string]any) { v["schema_version"] = "v2" },
		"invalid date":    func(v map[string]any) { v["since"] = "2026-02-30" },
		"window":          func(v map[string]any) { v["since"] = "2026-09-01" },
		"future range":    func(v map[string]any) { v["until"] = "2026-11-04" },
		"omitted":         func(v map[string]any) { v["omitted"].(map[string]any)["unscoped_news"] = -1 },
		"null items":      func(v map[string]any) { v["items"] = nil },
		"duplicate id":    func(v map[string]any) { a := v["items"].([]any); v["items"] = append(a, a[0]) },
		"over item limit": func(v map[string]any) { v["items"] = make([]any, 501) },
	} {
		t.Run(name, func(t *testing.T) {
			var v map[string]any
			_ = json.Unmarshal(valid, &v)
			mutate(v)
			raw, _ := json.Marshal(v)
			if _, err := decodePublicResearch(raw); err == nil {
				t.Fatal("invalid artifact accepted")
			}
		})
	}
	for _, tc := range []struct {
		field string
		value any
	}{
		{"unknown", "private"}, {"symbol", "aapl"}, {"market", "GLOBAL"}, {"id", "bad"},
		{"occurred_at", "2026-11-03T00:00:00+00:00"}, {"captured_at", "2026-11-04T00:00:00Z"},
		{"sources", nil}, {"source", "private path"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			var v map[string]any
			_ = json.Unmarshal(valid, &v)
			v["items"].([]any)[0].(map[string]any)[tc.field] = tc.value
			raw, _ := json.Marshal(v)
			if _, err := decodePublicResearch(raw); err == nil {
				t.Fatal("invalid item accepted")
			}
		})
	}
	for _, raw := range [][]byte{
		append(append([]byte{}, valid...), []byte(" {}")...),
		[]byte(strings.Replace(string(valid), `"sample":true`, `"sample":true,"sample":true`, 1)),
		[]byte(strings.Replace(string(valid), `Synthetic filing`, `bad\ud800`, 1)),
		bytesWithInvalidUTF8(valid),
	} {
		if _, err := decodePublicResearch(raw); err == nil {
			t.Fatal("invalid JSON boundary accepted")
		}
	}
}

func bytesWithInvalidUTF8(raw []byte) []byte {
	return []byte(strings.Replace(string(raw), "Synthetic filing", "bad\xff", 1))
}

func TestPublicResearchArticleAndEvidenceBounds(t *testing.T) {
	valid := researchFixture(t)
	for _, tc := range []struct {
		field string
		value any
	}{
		{"url", "https://name:secret@example.test/"}, {"url", "https://example.test/?token=secret"},
		{"url", "javascript:alert(1)"}, {"title", strings.Repeat("한", 171)}, {"summary", "bad\x00"},
		{"title", 1}, {"association", "inferred"},
	} {
		var v map[string]any
		_ = json.Unmarshal(valid, &v)
		for _, item := range v["items"].([]any) {
			m := item.(map[string]any)
			if m["kind"] == "news" {
				m[tc.field] = tc.value
			}
		}
		raw, _ := json.Marshal(v)
		if _, err := decodePublicResearch(raw); err == nil {
			t.Fatalf("accepted article %s=%v", tc.field, tc.value)
		}
	}
	for _, tc := range []struct {
		field string
		value any
	}{
		{"stars", 0}, {"stars", 1.5}, {"confidence", 1.1}, {"reasons", nil},
		{"as_of", "2026-11-02"}, {"disclaimer", ""}, {"direction", "BUY"},
	} {
		var v map[string]any
		_ = json.Unmarshal(valid, &v)
		for _, item := range v["items"].([]any) {
			m := item.(map[string]any)
			if m["kind"] == "insight" {
				m[tc.field] = tc.value
			}
		}
		raw, _ := json.Marshal(v)
		if _, err := decodePublicResearch(raw); err == nil {
			t.Fatalf("accepted insight %s=%v", tc.field, tc.value)
		}
	}
	var v map[string]any
	_ = json.Unmarshal(valid, &v)
	for _, item := range v["items"].([]any) {
		m := item.(map[string]any)
		if m["kind"] == "filing" {
			m["title"], m["summary"] = nil, nil
		}
	}
	raw, _ := json.Marshal(v)
	if _, err := decodePublicResearch(raw); err != nil {
		t.Fatalf("nullable article rejected: %v", err)
	}
	if _, err := benchmarkObject([]byte(`{"title":null}`), "title"); err == nil {
		t.Fatal("existing closed-object caller unexpectedly accepts null")
	}
}

func TestPublicResearchAttributionAndClosedOpenAPI(t *testing.T) {
	valid := researchFixture(t)
	for _, tc := range []struct {
		field string
		value any
	}{
		{"publisher", ""}, {"target", "https://user:secret@example.test/feed"},
		{"source_url", "https://example.test/?api_key=secret"},
		{"rights_source_url", "file:///private/path"}, {"checked_on", "2026-02-30"},
		{"attribution", nil}, {"author", strings.Repeat("x", 2049)},
		{"terms_urls", []string{}}, {"terms_urls", []string{"https://example.test/?password=secret"}},
		{"permissions", []string{"publish"}},
	} {
		var v map[string]any
		_ = json.Unmarshal(valid, &v)
		for _, rawItem := range v["items"].([]any) {
			item := rawItem.(map[string]any)
			if item["kind"] == "insight" {
				item["sources"].([]any)[0].(map[string]any)[tc.field] = tc.value
			}
		}
		raw, _ := json.Marshal(v)
		if _, err := decodePublicResearch(raw); err == nil {
			t.Fatalf("accepted invalid evidence %s", tc.field)
		}
	}
	duplicate := strings.Replace(string(valid), `"publisher":"Synthetic publisher"`, `"publisher":"Synthetic publisher","publisher":"other"`, 1)
	if _, err := decodePublicResearch([]byte(duplicate)); err == nil {
		t.Fatal("nested duplicate key accepted")
	}
	raw, err := os.ReadFile("../../../../../../../../contracts/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	path := spec["paths"].(map[string]any)["/v1/research/feed"].(map[string]any)
	if len(path) != 1 || path["get"] == nil {
		t.Fatal("research endpoint is not GET-only")
	}
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	for _, name := range []string{"PublicResearchView", "MimirPublicResearchFeed", "PublicResearchFiling", "PublicResearchNews", "PublicResearchInsight", "PublicResearchSourceMetadata", "PublicResearchUsageEvidence", "PublicResearchOmitted"} {
		schema := schemas[name].(map[string]any)
		properties := schema["properties"].(map[string]any)
		if schema["additionalProperties"] != false || len(schema["required"].([]any)) != len(properties) {
			t.Fatalf("open public shape: %s", name)
		}
	}
}

func researchFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../../../../../../../contracts/fixtures/public-research.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
