package ledger

import (
	"bytes"
	"encoding/json"
	"omni-folio/services/core/internal/modules/ledger/contracts"
	ledgerdomain "omni-folio/services/core/internal/modules/ledger/internal/domain"
	"testing"
)

func TestBrokerTypedConversionsPreserveCanonicalArraysAndBytes(t *testing.T) {
	for _, populated := range []bool{false, true} {
		input := &contracts.BrokerSnapshot{}
		if populated {
			input.Positions = []contracts.BrokerPosition{{Symbol: "005930", Quantity: "1.25"}}
			input.OpenOrders = []contracts.BrokerOpenOrder{}
		}
		expected, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		domain := brokerSnapshotDomain(input)
		canonical, err := json.Marshal(domain)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := json.Marshal(brokerSnapshotDTO(domain))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(expected, canonical) || !bytes.Equal(expected, restored) {
			t.Fatalf("typed conversion changed canonical bytes: %s / %s / %s", expected, canonical, restored)
		}
	}
	if brokerSnapshotDomain(nil) != nil || brokerDifferencesDTO(nil) != nil {
		t.Fatal("nil input became a populated projection")
	}
	if brokerDifferencesDTO([]ledgerdomain.BrokerPositionDifference{}) == nil {
		t.Fatal("empty differences became nil")
	}
}
