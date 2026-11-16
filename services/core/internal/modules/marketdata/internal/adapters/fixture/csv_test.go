package fixture

import (
	"os"
	"strings"
	"testing"
)

func TestMarketDataFixtureRejectsInvalidRows(t *testing.T) {
	header := "bar_at,symbol,venue,timezone,interval,open,high,low,close,volume\n"
	valid := "2026-01-02T00:00:00Z,AAPL,XNAS,America/New_York,1d,10,11,9,10,100\n"
	tests := map[string]string{
		"empty":        header,
		"too many":     header + strings.Repeat(valid, maxMarketDataRows+1),
		"metadata":     header + valid + "2026-01-03T00:00:00Z,MSFT,XNAS,America/New_York,1d,10,11,9,10,100\n",
		"unordered":    header + valid + "2026-01-01T00:00:00Z,AAPL,XNAS,America/New_York,1d,10,11,9,10,100\n",
		"duplicate":    header + valid + valid,
		"invalid ohlc": header + "2026-01-02T00:00:00Z,AAPL,XNAS,America/New_York,1d,10,9,8,10,100\n",
		"volume":       header + "2026-01-02T00:00:00Z,AAPL,XNAS,America/New_York,1d,10,11,9,10,-1\n",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(input)); err == nil {
				t.Fatal("invalid fixture was accepted")
			}
		})
	}
}

func TestMarketDataFixtureCapsInputBeforeCSVParsing(t *testing.T) {
	_, err := Parse(strings.NewReader(strings.Repeat("x", maxMarketDataBytes+1)))
	if err == nil || !strings.Contains(err.Error(), "must not exceed") {
		t.Fatalf("oversized fixture error=%v", err)
	}
}

func TestMarketDataIdentifiedFixtureRejectsIncompleteOrMixedIdentity(t *testing.T) {
	input, err := os.ReadFile("../../../../../../../../contracts/fixtures/market-bars-identified.csv")
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"instrument_aapl,USD", ",USD"}, {"instrument_aapl,USD", "instrument_aapl,"}, {"instrument_aapl,USD", "instrument_aapl,usd"}, {"instrument_aapl,USD", "bad id,USD"}, {"instrument_aapl,USD", "another,USD"}, {"instrument_aapl,USD", "instrument_aapl,KRW"}} {
		if _, err := Parse(strings.NewReader(strings.Replace(string(input), pair[0], pair[1], 1))); err == nil {
			t.Fatalf("accepted mismatched identity %q", pair[1])
		}
	}
}
