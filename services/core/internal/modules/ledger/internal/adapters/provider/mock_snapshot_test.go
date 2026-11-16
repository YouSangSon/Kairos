package provider

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestFactoryClearsRawReturnedWithError(t *testing.T) {
	raw := []byte("synthetic-private-data")
	factory := MockSnapshots{ReadSecret: func(context.Context) ([]byte, error) { return raw, errors.New("private source failure") }}
	reader, err := factory.Open(context.Background())
	if reader != nil || err == nil || err.Error() != "kiwoom mock sync Keychain record is unavailable" || !bytes.Equal(raw, make([]byte, len(raw))) {
		t.Fatalf("failed reader not sanitized/cleared: %v", err)
	}
}
