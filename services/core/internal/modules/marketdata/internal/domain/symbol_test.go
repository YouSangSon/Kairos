package domain

import "testing"

func TestKRXSymbolKeepsSixASCIIDigits(t *testing.T) {
	for _, symbol := range []string{"005930", "000000", "999999"} {
		if !KRXSymbol(symbol) {
			t.Fatalf("valid stock code rejected %q", symbol)
		}
	}
	for _, symbol := range []string{"", "A005930", "00A930", "00593 ", "１２３", "₩abc"} {
		if KRXSymbol(symbol) {
			t.Fatalf("invalid stock code accepted %q", symbol)
		}
	}
}
