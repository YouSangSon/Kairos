package sqlite

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPaperProposalSchemaAllows64DigitTargetButRejectsZeroBuy(t *testing.T) {
	svc, signal, _ := paperProposalFixture(t)
	raw := paperProposalForTest(t, svc, signal, "golden_cross")
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	body["target_quantity"] = strings.Repeat("9", 64)
	if _, err := decodePaperProposal(hashPaperProposalForTest(t, body)); err != nil {
		t.Fatalf("schema-valid target rejected: %v", err)
	}
	body["target_quantity"] = "0"
	if _, err := decodePaperProposal(hashPaperProposalForTest(t, body)); err == nil {
		t.Fatal("zero buy target accepted")
	}
}
