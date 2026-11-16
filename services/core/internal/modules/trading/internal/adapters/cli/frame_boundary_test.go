package cli

import (
	"bufio"
	"bytes"
	"testing"
)

func TestPaperStreamFrameBoundary(t *testing.T) {
	for _, test := range []struct {
		name    string
		raw     []byte
		frames  int
		invalid bool
	}{
		{"empty", nil, 0, false},
		{"ordered", []byte("one\ntwo\n"), 2, false},
		{"incomplete", []byte("one\ntwo"), 1, true},
		{"max", append(bytes.Repeat([]byte{'x'}, maxPaperBundleBytes-1), '\n'), 1, false},
		{"oversized", append(bytes.Repeat([]byte{'x'}, maxPaperBundleBytes), '\n'), 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			scanner := bufio.NewScanner(bytes.NewReader(test.raw))
			scanner.Buffer(make([]byte, 4096), maxPaperBundleBytes+1)
			scanner.Split(splitPaperBundle)
			count := 0
			for scanner.Scan() {
				count++
			}
			if count != test.frames || (scanner.Err() != nil) != test.invalid {
				t.Fatalf("frames=%d err=%v", count, scanner.Err())
			}
		})
	}
}
