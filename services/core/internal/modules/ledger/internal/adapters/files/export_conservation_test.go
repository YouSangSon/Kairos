// Conserved from ledger_export_test.go at 715c95d709dce1aa3e0c7ca93c002231cd9d4eea.
package files

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func TestLedgerExportCSVWriterFailureAndByteLimit(t *testing.T) {
	sentinel := errors.New("private-writer-error")
	if err := writeLedgerExportCSV(context.Background(), ledgerExportFailWriter{sentinel}, nil); !errors.Is(err, sentinel) {
		t.Fatalf("CSV flush hid a write error: %v", err)
	}
	var out bytes.Buffer
	limited := &ledgerExportLimitWriter{writer: &out, remaining: 3}
	if n, err := limited.Write([]byte("1234")); n != 0 || err == nil || out.Len() != 0 {
		t.Fatal("byte limit wrote a partial chunk")
	}
	if n, err := limited.Write([]byte("123")); n != 3 || err != nil || out.String() != "123" {
		t.Fatal("exact byte bound was rejected")
	}
	limited = &ledgerExportLimitWriter{writer: ledgerExportShortWriter{}, remaining: 3}
	if _, err := limited.Write([]byte("123")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal("short writes were hidden")
	}
}

type ledgerExportFailWriter struct{ err error }

func (w ledgerExportFailWriter) Write([]byte) (int, error) { return 0, w.err }

type ledgerExportShortWriter struct{}

func (ledgerExportShortWriter) Write([]byte) (int, error) { return 0, nil }
