package main

import (
	"context"
	"testing"
	"time"
)

func TestPortfolioQueriesKeepUnrelatedMarketCorruptionOutsideTheirScope(t *testing.T) {
	for _, tc := range []struct {
		name, table string
		read        func(*Service) error
		prove       func(*Service) error
	}{
		{"cash ignores price corruption", "security_price_observations", func(s *Service) error {
			_, err := s.portfolio.CashValuation(context.Background(), "KRW", s.now().UTC().Format(time.RFC3339Nano))
			return err
		}, func(s *Service) error { _, err := s.portfolio.CurrentValuation(context.Background()); return err }},
		{"holding ignores FX corruption", "fx_observations", func(s *Service) error {
			_, err := s.portfolio.HoldingValuation(context.Background(), s.now().UTC().Format(time.RFC3339Nano))
			return err
		}, func(s *Service) error { _, err := s.portfolio.CurrentValuation(context.Background()); return err }},
		{"current ignores benchmark corruption", "benchmark_series_versions", func(s *Service) error { _, err := s.portfolio.CurrentValuation(context.Background()); return err }, func(s *Service) error { _, err := s.portfolio.Benchmarks(context.Background()); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := testService(t, nil, nil)
			s.db.SetMaxOpenConns(1)
			if _, err := s.db.Exec(`CREATE TEMP TRIGGER unexpected_market_trigger BEFORE INSERT ON ` + tc.table + ` BEGIN SELECT RAISE(IGNORE); END`); err != nil {
				t.Fatal(err)
			}
			if err := tc.read(s); err != nil {
				t.Fatalf("unrelated journal changed query behavior: %v", err)
			}
			if err := tc.prove(s); err == nil {
				t.Fatal("required journal corruption was accepted")
			}
		})
	}
}
