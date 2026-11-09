package files

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"omni-folio/services/core/internal/codec"
	"omni-folio/services/core/internal/modules/trading/internal/application"
	"os"
	"regexp"
	"strconv"
	"syscall"
	"time"
	"unicode/utf8"
)

func ReadStrategyArtifact(path string) ([]byte, error) {
	return ReadBoundedRegularFile(path, maxBodyBytes)
}

func ReadBoundedRegularFile(path string, limit int64) ([]byte, error) {
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

func DecodePaperInputBundle(raw []byte) (proposal, bars, research []byte, err error) {
	invalid := errors.New("paper input bundle contract is invalid")
	if len(raw) == 0 || len(raw) > maxPaperBundleBytes || !utf8.Valid(raw) || !ValidJSONSurrogates(raw) {
		return nil, nil, nil, invalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if start, err := d.Token(); err != nil || start != json.Delim('{') {
		return nil, nil, nil, invalid
	}
	fields := make(map[string]json.RawMessage)
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok || fields[name] != nil {
			return nil, nil, nil, invalid
		}
		switch name {
		case "schema_version", "mode", "proposal", "bars_csv", "research_csv":
		default:
			return nil, nil, nil, invalid
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return nil, nil, nil, invalid
		}
		fields[name] = value
	}
	if end, err := d.Token(); err != nil || end != json.Delim('}') || len(fields) != 5 || ensureJSONEOF(d) != nil {
		return nil, nil, nil, invalid
	}
	values := make(map[string]string)
	for _, key := range []string{"schema_version", "mode", "bars_csv", "research_csv"} {
		var value *string
		if json.Unmarshal(fields[key], &value) != nil || value == nil {
			return nil, nil, nil, invalid
		}
		values[key] = *value
	}
	if values["schema_version"] != "paper-input-bundle.v1" || values["mode"] != "paper_bundle_only" ||
		len(values["bars_csv"]) > maxBodyBytes || len(values["research_csv"]) > maxBodyBytes {
		return nil, nil, nil, invalid
	}
	if _, err := DecodePaperProposal(fields["proposal"]); err != nil {
		return nil, nil, nil, invalid
	}
	return fields["proposal"], []byte(values["bars_csv"]), []byte(values["research_csv"]), nil
}

func ValidJSONSurrogates(raw []byte) bool {
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

func DecodePaperProposal(raw []byte) (map[string]any, error) {
	invalid := errors.New("paper proposal contract is invalid")
	if len(raw) == 0 || len(raw) > maxBodyBytes {
		return nil, invalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return nil, invalid
	}
	p := make(map[string]any)
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return nil, invalid
		}
		name, ok := key.(string)
		if _, exists := p[name]; !ok || exists {
			return nil, invalid
		}
		var value any
		if err := d.Decode(&value); err != nil {
			return nil, invalid
		}
		if _, ok := value.(string); !ok && !(name == "target_quantity" && value == nil) {
			return nil, invalid
		}
		p[name] = value
	}
	if end, err := d.Token(); err != nil || end != json.Delim('}') {
		return nil, invalid
	}
	if err := ensureJSONEOF(d); err != nil {
		return nil, invalid
	}
	if !exactKeys(p, "schema_version", "mode", "strategy_result_sha256", "strategy_parameter_sha256", "input_sha256", "symbol", "data_as_of", "signal", "target_quantity", "proposal_sha256") ||
		stringField(p, "schema_version") != "paper-signal-proposal.v1" || stringField(p, "mode") != "paper_proposal_only" || !kiwoomStockPattern.MatchString(stringField(p, "symbol")) {
		return nil, invalid
	}
	for _, field := range []string{"strategy_result_sha256", "strategy_parameter_sha256", "input_sha256", "proposal_sha256"} {
		if !strategySHA256Pattern.MatchString(stringField(p, field)) {
			return nil, invalid
		}
	}
	at := stringField(p, "data_as_of")
	if parsed, err := time.Parse(time.RFC3339, at); err != nil || parsed.UTC().Format(time.RFC3339) != at {
		return nil, invalid
	}
	target := stringField(p, "target_quantity")
	switch stringField(p, "signal") {
	case "golden_cross":
		if len(target) > 64 || !orderIntegerPattern.MatchString(target) {
			return nil, invalid
		}
	case "death_cross":
		if target != "0" {
			return nil, invalid
		}
	case "none":
		if p["target_quantity"] != nil {
			return nil, invalid
		}
	default:
		return nil, invalid
	}
	claimed := stringField(p, "proposal_sha256")
	delete(p, "proposal_sha256")
	body, err := strategyCanonicalJSON(p)
	if err != nil {
		return nil, invalid
	}
	hash := sha256.Sum256(body)
	if hex.EncodeToString(hash[:]) != claimed {
		return nil, invalid
	}
	p["proposal_sha256"] = claimed
	return p, nil
}

const (
	maxBodyBytes        = 1 << 20
	maxPaperBundleBytes = 4 * maxBodyBytes
)

var (
	ensureJSONEOF         = codec.EnsureJSONEOF
	exactKeys             = application.ExactKeys
	stringField           = application.StringField
	strategyCanonicalJSON = application.StrategyCanonicalJSON
	kiwoomStockPattern    = regexp.MustCompile(`^[0-9]{6}$`)
	strategySHA256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	orderIntegerPattern   = regexp.MustCompile(`^[1-9][0-9]*$`)
)
