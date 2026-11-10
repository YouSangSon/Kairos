package composition

import (
	"context"
	"database/sql"
	"net/http"
	maintenance "omni-folio/services/core/internal/modules/maintenance"
	maintconstruction "omni-folio/services/core/internal/modules/maintenance/construction"
	maint "omni-folio/services/core/internal/modules/maintenance/contracts"
	marketconstruction "omni-folio/services/core/internal/modules/marketdata/construction"
	market "omni-folio/services/core/internal/modules/marketdata/contracts"
	tradingconstruction "omni-folio/services/core/internal/modules/trading/construction"
	trading "omni-folio/services/core/internal/modules/trading/contracts"
	sdk "omni-folio/services/core/internal/providers/kiwoom"
	pool "omni-folio/services/core/internal/storage/sqlite"
	"time"
)

var processMockReadPacer = sdk.NewMockReadPacer(time.Now, sdk.WaitMockRead)

type Resources struct {
	Now func() time.Time
	ID  func(string) string
}

func NewResources() Resources                         { return Resources{Now: time.Now, ID: randomID} }
func (r Resources) MockReadPacer() *sdk.MockReadPacer { return processMockReadPacer }
func (r Resources) MockReadTransport() (*http.Transport, func()) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return transport, transport.CloseIdleConnections
}
func (r Resources) compose(db *sql.DB) (*Runtime, error) {
	runtime, err := Compose(Config{DB: db, Now: r.Now, ID: r.ID})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return runtime, nil
}
func (r Resources) OpenServer(path, fixture, research string) (*Runtime, error) {
	db, err := pool.OpenExisting(path)
	if err != nil {
		return nil, err
	}
	runtime, err := Compose(Config{DB: db, Now: r.Now, ID: r.ID, RequireStartup: true, MarketFixturePath: fixture, ResearchFeedPath: research})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return runtime, nil
}
func (r Resources) Trading() tradingconstruction.CLIResources { return tradingResources{r} }

type tradingResources struct{ Resources }
type tradingSession struct{ *Runtime }

func (s tradingSession) Trading() trading.Trading { return s.Runtime.Trading }
func (s tradingSession) RequireSchema() error {
	return s.Maintenance.Schema.RequireCurrent(context.Background())
}
func (r tradingResources) OpenExisting(path string) (tradingconstruction.Session, error) {
	db, err := pool.OpenExisting(path)
	if err != nil {
		return nil, err
	}
	s, err := r.compose(db)
	if err != nil {
		return nil, err
	}
	return tradingSession{s}, nil
}
func (r tradingResources) OpenReadOnly(ctx context.Context, path string) (tradingconstruction.Session, error) {
	db, err := pool.OpenReadOnlyExisting(ctx, path)
	if err != nil {
		return nil, err
	}
	s, err := r.compose(db)
	if err != nil {
		return nil, err
	}
	return tradingSession{s}, nil
}
func (r tradingResources) OpenReadWrite(ctx context.Context, path string) (tradingconstruction.Session, error) {
	db, err := pool.OpenReadWriteExisting(ctx, path)
	if err != nil {
		return nil, err
	}
	s, err := r.compose(db)
	if err != nil {
		return nil, err
	}
	return tradingSession{s}, nil
}
func (r Resources) Market() marketconstruction.CLIResources { return marketResources{r} }

type marketResources struct{ Resources }
type marketSession struct{ *Runtime }

func (s marketSession) Imports() market.Imports              { return s.Runtime.Market.Imports }
func (s marketSession) OwnerPrice() market.OwnerPriceImports { return s.Runtime.OwnerPrice }
func (r marketResources) OpenReadOnly(ctx context.Context, path string) (marketconstruction.Session, error) {
	db, err := pool.OpenReadOnlyExisting(ctx, path)
	if err != nil {
		return nil, err
	}
	s, err := r.compose(db)
	if err != nil {
		return nil, err
	}
	return marketSession{s}, nil
}
func (r marketResources) OpenReadWrite(ctx context.Context, path string) (marketconstruction.Session, error) {
	db, err := pool.OpenReadWriteExisting(ctx, path)
	if err != nil {
		return nil, err
	}
	s, err := r.compose(db)
	if err != nil {
		return nil, err
	}
	return marketSession{s}, nil
}
func (r Resources) Maintenance() maintconstruction.CLIResources { return maintenanceResources{r} }

type maintenanceResources struct{ Resources }
type maintenanceSession struct{ *Runtime }

func (s maintenanceSession) Migrations() maint.Migrations { return s.Runtime.Maintenance.Migrations }
func (s maintenanceSession) Backups() maint.Backups       { return s.Runtime.Maintenance.Backups }
func (r maintenanceResources) Open(path string) (maintconstruction.Session, error) {
	db, err := pool.Open(path)
	if err != nil {
		return nil, err
	}
	s, err := r.compose(db)
	if err != nil {
		return nil, err
	}
	return maintenanceSession{s}, nil
}
func (r maintenanceResources) OpenExisting(path string) (maintconstruction.Session, error) {
	db, err := pool.OpenExisting(path)
	if err != nil {
		return nil, err
	}
	s, err := r.compose(db)
	if err != nil {
		return nil, err
	}
	return maintenanceSession{s}, nil
}
func (r maintenanceResources) VerifyManifest(ctx context.Context, path, golden, manifest string) error {
	verifier, err := maintenance.ComposeVerification(maintenance.VerificationConfig{Now: r.Now, ID: r.ID, NewOwners: func(db *sql.DB) (maintenance.Owners, error) { return recoveryOwners(db, r.Now, r.ID) }})
	if err != nil {
		return err
	}
	return verifier.VerifyManifest(ctx, path, golden, manifest)
}
