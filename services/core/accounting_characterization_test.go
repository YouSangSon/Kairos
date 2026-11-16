package main

import (
	"math/big"
	"testing"
)

func TestArchitectureFIFOAllocationExactExamples(t *testing.T) {
	tests := []struct {
		name, cost, take, quantity, want string
	}{
		{"finite decimal", "10", "1", "4", "2.5"},
		{"recurring decimal uses half even", "1", "1", "3", "0.33333333"},
		{"last fill receives the exact residual", "1", "3", "3", "1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allocation, err := fifoCostAllocation(characterizedRat(t, test.cost), characterizedRat(t, test.take), characterizedRat(t, test.quantity))
			got, formatErr := formatDecimal(allocation)
			if err != nil || formatErr != nil || got != test.want {
				t.Fatalf("allocation=%q err=%v formatErr=%v want=%q", got, err, formatErr, test.want)
			}
		})
	}

	if _, err := fifoCostAllocation(big.NewRat(1, 3), big.NewRat(1, 1), big.NewRat(2, 1)); err == nil {
		t.Fatal("non-decimal lot cost was accepted")
	}
}

func characterizedRat(t *testing.T, raw string) *big.Rat {
	t.Helper()
	value, err := parseDecimal(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
