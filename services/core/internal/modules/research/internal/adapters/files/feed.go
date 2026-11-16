package files

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"omni-folio/services/core/internal/modules/research/contracts"
	"omni-folio/services/core/internal/modules/research/internal/application"
)

const publicResearchVersion = "mimir-public-research.v1"
const publicResearchMaxBytes = 1024 * 1024

var researchSymbol = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.-]{0,31}$`)
var researchSource = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
var researchID = regexp.MustCompile(`^[0-9a-f]{64}$`)
var researchTime = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,6})?Z$`)
var errPublicResearch = errors.New("invalid public research feed")

type publicResearchFeed struct {
	SchemaVersion string                          `json:"schema_version"`
	GeneratedAt   string                          `json:"generated_at"`
	Since         string                          `json:"since"`
	Until         string                          `json:"until"`
	Sample        bool                            `json:"sample"`
	Omitted       contracts.PublicResearchOmitted `json:"omitted"`
	Items         []json.RawMessage               `json:"items"`
}

type Reader struct{ Path func() string }

func (r *Reader) Read() (*application.Feed, error) {
	path := r.Path()
	if path == "" {
		return nil, contracts.ErrNotConfigured
	}
	raw, err := readBoundedRegularFile(path, publicResearchMaxBytes)
	if errors.Is(err, os.ErrNotExist) {
		return nil, contracts.ErrFeedMissing
	}
	var feed *publicResearchFeed
	if err == nil {
		feed, err = decodePublicResearch(raw)
	}
	if err != nil {
		return nil, contracts.ErrFeedUnavailable
	}
	return &application.Feed{
		SchemaVersion: feed.SchemaVersion, GeneratedAt: feed.GeneratedAt, Since: feed.Since, Until: feed.Until,
		Sample: feed.Sample, Omitted: feed.Omitted, Items: feed.Items,
	}, nil
}
func decodePublicResearch(raw []byte) (*publicResearchFeed, error) {
	if len(raw) == 0 || len(raw) > publicResearchMaxBytes || !utf8.Valid(raw) || !validJSONSurrogates(raw) {
		return nil, errPublicResearch
	}
	fields, err := benchmarkObject(raw, "schema_version", "generated_at", "since", "until", "sample", "omitted", "items")
	if err != nil {
		return nil, errPublicResearch
	}
	var feed publicResearchFeed
	if json.Unmarshal(raw, &feed) != nil || feed.SchemaVersion != publicResearchVersion || feed.Items == nil || len(feed.Items) > 500 {
		return nil, errPublicResearch
	}
	generated, timeOK := researchTimestamp(feed.GeneratedAt)
	since, sinceOK := researchDate(feed.Since)
	until, untilOK := researchDate(feed.Until)
	if !timeOK || !sinceOK || !untilOK || since.After(until) || until.Sub(since) >= 31*24*time.Hour || feed.Until > feed.GeneratedAt[:10] {
		return nil, errPublicResearch
	}
	if _, err := benchmarkObject(fields["omitted"], "unscoped_news", "missing_source_evidence"); err != nil {
		return nil, errPublicResearch
	}
	seen := map[string]bool{}
	for _, item := range feed.Items {
		id, err := validatePublicResearchItem(item, generated, feed.Since, feed.Until)
		if err != nil || seen[id] {
			return nil, errPublicResearch
		}
		seen[id] = true
	}
	return &feed, nil
}

func validatePublicResearchItem(raw []byte, generated time.Time, since, until string) (string, error) {
	var item struct {
		ID, Kind, Market, Symbol, Source string
		OccurredAt                       string `json:"occurred_at"`
		CapturedAt                       string `json:"captured_at"`
		Sources                          []json.RawMessage
		Title, Summary                   *string
		URL, Association                 string
		AsOf                             string `json:"as_of"`
		Direction                        string
		Stars                            int
		Confidence                       float64
		Reasons                          []string
		Disclaimer                       string
	}
	if json.Unmarshal(raw, &item) != nil {
		return "", errPublicResearch
	}
	keys := []string{"id", "kind", "market", "symbol", "source", "occurred_at", "captured_at", "sources"}
	switch item.Kind {
	case "filing":
		keys = append(keys, "title", "summary", "url")
	case "news":
		keys = append(keys, "title", "summary", "url", "association")
		if item.Association != "explicit_tag" && item.Association != "mention" {
			return "", errPublicResearch
		}
	case "insight":
		keys = append(keys, "as_of", "direction", "stars", "confidence", "reasons", "disclaimer")
	default:
		return "", errPublicResearch
	}
	if _, err := closedJSONObject(raw, map[string]bool{"title": true, "summary": true}, keys...); err != nil {
		return "", errPublicResearch
	}
	occurred, occurredOK := researchTimestamp(item.OccurredAt)
	captured, capturedOK := researchTimestamp(item.CapturedAt)
	if !researchID.MatchString(item.ID) || (item.Market != "KR" && item.Market != "US") ||
		!researchSymbol.MatchString(item.Symbol) || !researchSource.MatchString(item.Source) ||
		!occurredOK || !capturedOK || occurred.After(generated) || captured.After(generated) ||
		item.OccurredAt[:10] < since || item.OccurredAt[:10] > until || item.Sources == nil || len(item.Sources) > 20 {
		return "", errPublicResearch
	}
	for _, source := range item.Sources {
		if !validResearchEvidence(source) {
			return "", errPublicResearch
		}
	}
	if item.Kind == "insight" {
		if _, ok := researchDate(item.AsOf); !ok || item.AsOf != item.OccurredAt[:10] ||
			(item.Direction != "bullish" && item.Direction != "bearish" && item.Direction != "neutral") ||
			item.Stars < 1 || item.Stars > 5 || math.IsNaN(item.Confidence) || math.IsInf(item.Confidence, 0) ||
			item.Confidence < 0 || item.Confidence > 1 || item.Reasons == nil || len(item.Reasons) > 10 || !researchText(item.Disclaimer, 2048, false) {
			return "", errPublicResearch
		}
		for _, reason := range item.Reasons {
			if !researchText(reason, 2048, false) {
				return "", errPublicResearch
			}
		}
	} else if !researchURL(item.URL) || (item.Title != nil && !researchText(*item.Title, 512, true)) ||
		(item.Summary != nil && !researchText(*item.Summary, 2048, true)) {
		return "", errPublicResearch
	}
	return item.ID, nil
}

func validResearchEvidence(raw []byte) bool {
	var source struct {
		Publisher, Target, Evidence, Attribution, Author string
		SourceURL                                        string   `json:"source_url"`
		RightsHolder                                     string   `json:"rights_holder"`
		RightsSourceURL                                  string   `json:"rights_source_url"`
		CheckedOn                                        string   `json:"checked_on"`
		TermsURLs                                        []string `json:"terms_urls"`
	}
	if json.Unmarshal(raw, &source) != nil {
		return false
	}
	keys := []string{"publisher", "target", "source_url", "evidence"}
	switch source.Evidence {
	case "metadata_only", "builtin_discovery_only":
	case "usage_evidence_recorded":
		keys = append(keys, "attribution", "author", "rights_holder", "rights_source_url", "checked_on", "terms_urls")
		if _, ok := researchDate(source.CheckedOn); !ok || !researchText(source.Attribution, 2048, false) ||
			!researchText(source.Author, 2048, false) || !researchText(source.RightsHolder, 2048, false) ||
			!researchURL(source.RightsSourceURL) || len(source.TermsURLs) == 0 || len(source.TermsURLs) > 10 {
			return false
		}
		for _, u := range source.TermsURLs {
			if !researchURL(u) {
				return false
			}
		}
	default:
		return false
	}
	_, err := benchmarkObject(raw, keys...)
	return err == nil && researchText(source.Publisher, 256, false) && researchText(source.Target, 2048, false) &&
		(!strings.Contains(source.Target, "://") || researchURL(source.Target)) && researchURL(source.SourceURL)
}

func researchText(value string, limit int, empty bool) bool {
	if len(value) > limit || (!empty && strings.TrimSpace(value) == "") {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return false
		}
	}
	return true
}

func researchURL(value string) bool {
	if !researchText(value, 2048, false) || strings.ContainsAny(value, "<>\"\\") || strings.IndexFunc(value, unicode.IsSpace) >= 0 {
		return false
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
		return false
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port > 65535 {
			return false
		}
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	for key := range query {
		switch strings.ToLower(key) {
		case "key", "api_key", "apikey", "token", "access_token", "secret", "password":
			return false
		}
	}
	return true
}

func researchDate(value string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", value)
	return t, err == nil && t.Format("2006-01-02") == value && !strings.HasPrefix(value, "0000")
}

func researchTimestamp(value string) (time.Time, bool) {
	if !researchTime.MatchString(value) {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	return t, err == nil && !strings.HasPrefix(value, "0000")
}

func benchmarkObject(raw []byte, keys ...string) (map[string]json.RawMessage, error) {
	return closedJSONObject(raw, nil, keys...)
}

func closedJSONObject(raw []byte, nullable map[string]bool, keys ...string) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return nil, errPublicResearch
	}
	fields := map[string]json.RawMessage{}
	allowed := map[string]bool{}
	for _, key := range keys {
		allowed[key] = true
	}
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || !allowed[key] || fields[key] != nil {
			return nil, errPublicResearch
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || (!nullable[key] && bytes.Equal(bytes.TrimSpace(value), []byte("null"))) {
			return nil, errPublicResearch
		}
		fields[key] = value
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') || len(fields) != len(keys) || ensureJSONEOF(d) != nil {
		return nil, errPublicResearch
	}
	return fields, nil
}

func readBoundedRegularFile(path string, limit int64) ([]byte, error) {
	// Open first without waiting for a FIFO writer, then validate the actual
	// descriptor. A path-only Stat check would race a regular-file replacement.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("strategy evidence path must be a regular file")
	}
	artifact, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(artifact)) > limit {
		return nil, errors.New("input exceeds the byte limit")
	}
	return artifact, nil
}
func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("strategy evidence contains trailing JSON")
	}
	return nil
}

// encoding/json v1 repairs lone UTF-16 surrogates. Reject instead: a byte-bound
// envelope must not silently replace data. The JSON decoder checks other syntax.
func validJSONSurrogates(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) || raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		code, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return false
		}
		if code >= 0xd800 && code <= 0xdbff {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}
