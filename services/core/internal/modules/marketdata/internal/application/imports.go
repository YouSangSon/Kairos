package application

import (
	"context"
	"errors"
	"time"

	c "omni-folio/services/core/internal/modules/marketdata/contracts"
	"omni-folio/services/core/internal/modules/marketdata/internal/domain"
	storage "omni-folio/services/core/internal/storage/contracts"
)

func recordedAt(ctx context.Context, scope interface {
	LastRecorded(context.Context, string) (string, error)
}, kind, fetched string, clock func() time.Time) (string, error) {
	previous, err := scope.LastRecorded(ctx, kind)
	if err != nil {
		return "", err
	}
	recorded, err := domain.OwnerRecordedAt(previous, fetched, clock())
	if errors.Is(err, domain.ErrCaptureFuture) {
		return "", c.ErrRejected
	}
	return recorded, err
}

func (s *Service) ImportOwnerFX(ctx context.Context, raw []byte) (*c.OwnerFXSeriesReceipt, error) {
	input, canonical, sha, err := s.decode.OwnerFX(ctx, raw)
	if err != nil {
		return nil, err
	}
	var result *c.OwnerFXSeriesReceipt
	err = s.store.Write(ctx, func(scope Scope) error {
		if err := scope.Schema(ctx); err != nil {
			return err
		}
		legacy, _, err := scope.FXObservations(ctx)
		if err != nil {
			return err
		}
		prior, proof, err := scope.OwnerFX(ctx, input.BaseCurrency, input.QuoteCurrency, input.SourceVersion)
		if err != nil {
			return err
		}
		if prior != nil {
			if domain.VersionConflicts(prior.DataSHA256, sha) {
				return c.ErrConflict
			}
			result = OwnerFXReceipt(prior)
			return nil
		}
		for _, mark := range legacy {
			if domain.LegacyFXConflicts(input.BaseCurrency, input.QuoteCurrency, mark.BaseCurrency, mark.QuoteCurrency) {
				return c.ErrConflict
			}
		}
		recorded, err := recordedAt(ctx, scope, "fx", input.FetchedAt, s.now)
		if err != nil {
			return err
		}
		record := OwnerFXRecord{Sequence: int64(proof.Versions + 1), Input: *input, DataSHA256: sha, RecordedAt: recorded}
		if err := scope.InsertOwnerFX(ctx, record, canonical); err != nil {
			return errors.New("owner FX series could not be stored")
		}
		result = OwnerFXReceipt(&record)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) ImportBenchmark(ctx context.Context, raw []byte) (*c.BenchmarkSeriesReceipt, error) {
	input, canonical, sha, err := s.decode.Benchmark(ctx, raw)
	if err != nil {
		return nil, err
	}
	var result *c.BenchmarkSeriesReceipt
	err = s.store.Write(ctx, func(scope Scope) error {
		if err := scope.Schema(ctx); err != nil {
			return err
		}
		prior, proof, err := scope.Benchmark(ctx, input.BenchmarkID, input.SourceVersion)
		if err != nil {
			return err
		}
		if prior != nil {
			if domain.VersionConflicts(prior.DataSHA256, sha) {
				return c.ErrConflict
			}
			result = BenchmarkReceipt(prior)
			return nil
		}
		definition, err := scope.LatestBenchmark(ctx, input.BenchmarkID)
		if err != nil {
			return err
		}
		if definition != nil && !domain.SameBenchmarkDefinition(domain.BenchmarkDefinition{definition.Currency, definition.ReturnBasis, definition.SourceReference, definition.Sample}, domain.BenchmarkDefinition{input.Currency, input.ReturnBasis, input.SourceReference, input.Sample}) {
			return c.ErrConflict
		}
		recorded, err := recordedAt(ctx, scope, "benchmark", input.FetchedAt, s.now)
		if err != nil {
			return err
		}
		record := BenchmarkRecord{Sequence: int64(proof.Versions + 1), Input: *input, DataSHA256: sha, RecordedAt: recorded}
		if err := scope.InsertBenchmark(ctx, record, canonical); err != nil {
			return errors.New("benchmark series could not be stored")
		}
		result = BenchmarkReceipt(&record)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) BenchmarkStatus(ctx context.Context, id, version string) (*c.BenchmarkSeriesReceipt, error) {
	if !domain.SafeID(id) || (version != "" && !domain.SafeID(version)) {
		return nil, errors.New("benchmark query is invalid")
	}
	var result *c.BenchmarkSeriesReceipt
	err := s.store.Read(ctx, func(scope Reader) error {
		if err := scope.Schema(ctx); err != nil {
			return err
		}
		record, _, err := scope.Benchmark(ctx, id, version)
		if err != nil {
			return err
		}
		if record == nil {
			return c.ErrBenchmarkNotFound
		}
		result = BenchmarkReceipt(record)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *ScopedImporter) ImportOwnerPriceScoped(ctx context.Context, ref storage.ObservationRef, command c.OwnerPriceCommand, identities c.PriceIdentities) (*c.OwnerPriceSeriesReceipt, error) {
	input, canonical, sha := &command.Input, command.Canonical, command.DataSHA256

	var result *c.OwnerPriceSeriesReceipt
	err := s.store.ScopedImport(ctx, ref, func(scope ScopedPriceScope) error {
		if err := scope.Schema(ctx); err != nil {
			return err
		}
		legacy, _, err := scope.PriceObservations(ctx)
		if err != nil {
			return err
		}
		if !domain.OwnerPriceAdmitted(identities.Identities, input.InstrumentID, input.Currency) {
			return c.ErrRejected
		}
		prior, proof, err := scope.OwnerPrice(ctx, identities.Identities, input.InstrumentID, input.Currency, input.SourceVersion)
		if err != nil {
			return err
		}
		if prior != nil {
			if domain.VersionConflicts(prior.DataSHA256, sha) {
				return c.ErrConflict
			}
			result = OwnerPriceReceipt(prior)
			return nil
		}
		for _, mark := range legacy {
			if domain.LegacyPriceConflicts(input.InstrumentID, input.Currency, mark.InstrumentID, mark.Currency) {
				return c.ErrConflict
			}
		}
		recorded, err := recordedAt(ctx, scope, "security_price", input.FetchedAt, s.now)
		if err != nil {
			return err
		}
		record := OwnerPriceRecord{Sequence: int64(proof.Versions + 1), Input: *input, DataSHA256: sha, RecordedAt: recorded}
		if err := scope.InsertOwnerPrice(ctx, record, canonical); err != nil {
			return errors.New("owner price series could not be stored")
		}
		result = OwnerPriceReceipt(&record)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type OwnerPriceImporter struct {
	atomic c.AtomicOwnerPriceImport
	decode Decoder
}

func NewOwnerPriceImporter(atomic c.AtomicOwnerPriceImport, decode Decoder) *OwnerPriceImporter {
	return &OwnerPriceImporter{atomic, decode}
}
func (s *OwnerPriceImporter) ImportOwnerPrice(ctx context.Context, raw []byte) (*c.OwnerPriceSeriesReceipt, error) {
	input, canonical, sha, err := s.decode.OwnerPrice(ctx, raw)
	if err != nil {
		return nil, err
	}
	return s.atomic.ImportOwnerPriceAtomic(ctx, c.OwnerPriceCommand{Input: *input, Canonical: canonical, DataSHA256: sha})
}
