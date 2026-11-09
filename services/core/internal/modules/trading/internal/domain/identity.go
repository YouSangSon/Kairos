package domain

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func SafeOrderID(raw string) bool {
	if raw == "" || len(raw) > 128 || !utf8.ValidString(raw) {
		return false
	}
	for _, r := range raw {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func OrderAlias(raw, kind string) bool {
	return orderAliasPattern.MatchString(raw) && strings.HasPrefix(raw, "kiwoom_"+kind+"_")
}

func CanonicalUTCTime(raw string) (time.Time, bool) {
	value, err := time.Parse(time.RFC3339Nano, raw)
	return value, err == nil && value.Location() == time.UTC && value.Format(time.RFC3339Nano) == raw
}

func PaperEventID(kind string, parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "paper_" + kind + "_" + hex.EncodeToString(hash[:16])
}

func PaperProviderAlias(kind string, parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "kiwoom_" + kind + "_" + base64.RawURLEncoding.EncodeToString(hash[:18])
}

var orderAliasPattern = regexp.MustCompile(`^kiwoom_(account|order|execution)_[A-Za-z0-9_-]{24}$`)
