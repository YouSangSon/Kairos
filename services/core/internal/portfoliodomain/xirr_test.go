package portfoliodomain

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"testing"
	"time"

	"omni-folio/services/core/internal/exact"
)

func TestPortfolioXIRRIndependentGoldenCashFlows(t *testing.T) {
	raw, err := os.ReadFile("../../../../contracts/fixtures/portfolio-xirr-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Status string
			Flows        []struct{ Date, Amount string }
			ExpectedRate *string `json:"expected_rate"`
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("missing independent XIRR goldens")
	}
	for _, entry := range fixture.Cases {
		t.Run(entry.Name, func(t *testing.T) {
			var flows []XIRRCashFlow
			for _, flow := range entry.Flows {
				at, err := time.Parse("2006-01-02", flow.Date)
				if err != nil {
					t.Fatal(err)
				}
				amount, err := exact.ParseDecimal(flow.Amount)
				if err != nil {
					t.Fatal(err)
				}
				flows = append(flows, XIRRCashFlow{UTCDay(at), amount})
			}
			result, err := SolveXIRR(context.Background(), flows)
			if err != nil {
				t.Fatal(err)
			}
			var got *string
			switch entry.Status {
			case "calculated":
				got = result.Rate
				if result.Candidate != nil || result.SignChanges != 1 {
					t.Fatalf("unique result contract: %+v", result)
				}
			case "candidate":
				got = result.Candidate
				if result.Rate != nil || result.SignChanges <= 1 {
					t.Fatalf("candidate promoted to unique: %+v", result)
				}
			case "unavailable":
				if result.Rate != nil || result.Candidate != nil || result.Reason == "" {
					t.Fatalf("undefined XIRR became a number: %+v", result)
				}
				return
			default:
				t.Fatal("unknown golden status")
			}
			if got == nil || entry.ExpectedRate == nil {
				t.Fatalf("missing rate: %+v", result)
			}
			actual, _ := exact.ParseDecimal(*got)
			expected, _ := exact.ParseDecimal(*entry.ExpectedRate)
			tolerance := new(big.Rat).Abs(expected)
			if tolerance.Cmp(big.NewRat(1, 1)) < 0 {
				tolerance.SetInt64(1)
			}
			tolerance.Mul(tolerance, big.NewRat(1, 10000000000))
			if new(big.Rat).Abs(new(big.Rat).Sub(actual, expected)).Cmp(tolerance) > 0 {
				t.Fatalf("got %s, independent reference %s", *got, *entry.ExpectedRate)
			}
		})
	}
}

func TestPortfolioXIRRDateBudgetAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SolveXIRR(ctx, []XIRRCashFlow{{0, big.NewRat(-100, 1)}, {365, big.NewRat(110, 1)}}); err != context.Canceled {
		t.Fatalf("cancellation lost: %v", err)
	}
	flows := make([]XIRRCashFlow, xirrMaxDates+1)
	for i := range flows {
		flows[i] = XIRRCashFlow{int64(i), big.NewRat(-1, 1)}
	}
	result, err := SolveXIRR(context.Background(), flows)
	if err != nil || result.Reason != "cash_flow_limit" || result.Rate != nil {
		t.Fatalf("date budget ignored: %+v %v", result, err)
	}
	for i := range flows {
		flows[i].Day = 0
	}
	flows = append(flows, XIRRCashFlow{365, big.NewRat(11001, 1)})
	result, err = SolveXIRR(context.Background(), flows)
	if err != nil || result.Rate == nil {
		t.Fatalf("same-day flows incorrectly consume date budget: %+v %v", result, err)
	}
}
