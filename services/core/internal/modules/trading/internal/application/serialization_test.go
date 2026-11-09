package application

import (
	"bytes"
	"testing"

	"omni-folio/services/core/internal/codec"
	"omni-folio/services/core/internal/modules/trading/contracts"
)

func TestPrivateEvidenceMetadataDoesNotChangePublicJSONOrHash(t *testing.T) {
	value := Evidence{StrategyEvidence: contracts.StrategyEvidence{ResultSHA256: "result", ArtifactSHA256: "artifact", Target: "paper_candidate"}, ArtifactJSON: "private source", ExecutionPolicy: StrategyExecutionPolicy{CanonicalJSON: "private policy"}}
	actual, hash, err := codec.JSONHash(value)
	if err != nil {
		t.Fatal(err)
	}
	expected, expectedHash, err := codec.JSONHash(value.StrategyEvidence)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, expected) || hash != expectedHash {
		t.Fatalf("private metadata changed public encoding: %s != %s", actual, expected)
	}
}
