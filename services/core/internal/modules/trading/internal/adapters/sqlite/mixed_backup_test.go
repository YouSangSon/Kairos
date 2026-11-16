package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	ledger "omni-folio/services/core/internal/modules/ledger"
	maintenance "omni-folio/services/core/internal/modules/maintenance"
	maintenancecontracts "omni-folio/services/core/internal/modules/maintenance/contracts"
	market "omni-folio/services/core/internal/modules/marketdata"
	watch "omni-folio/services/core/internal/modules/watchlist"
	"omni-folio/services/core/internal/storage/observation"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestG38C2PaperAuthorization(t *testing.T) {
	t.Run("immutable local authorization and no Kiwoom", func(t *testing.T) {
		svc, signal, _ := g38c2PaperSignalFixture(t, true)
		signal.TargetQuantity = "10"
		lease := mustK2CLease(t, svc, k2aAccountRef)
		_, order, err := svc.AdmitPaperSignal(context.Background(), k2aAccountRef, signal, lease.FencingToken)
		if err != nil || order == nil || order.Status != "OPEN" {
			t.Fatalf("authorized order=%+v err=%v", order, err)
		}
		var authorizations, reservations, events int
		if err := svc.db.QueryRow(`SELECT COUNT(*) FROM paper_execution_authorizations`).Scan(&authorizations); err != nil {
			t.Fatal(err)
		}
		if err := svc.db.QueryRow(`SELECT COUNT(*) FROM risk_reservations`).Scan(&reservations); err != nil {
			t.Fatal(err)
		}
		if err := svc.db.QueryRow(`SELECT COUNT(*) FROM order_events WHERE order_id=?`, order.OrderID).Scan(&events); err != nil {
			t.Fatal(err)
		}
		if authorizations != 1 || reservations != 0 || events != 4 {
			t.Fatalf("authorization rows=%d reservations=%d events=%d", authorizations, reservations, events)
		}
		authorization, found, err := loadPaperExecutionAuthorizationByOrder(context.Background(), svc.db, order.OrderID)
		if err != nil || !found {
			t.Fatalf("paper authorization found=%t err=%v", found, err)
		}
		tx, err := svc.db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		fill := OrderEvent{
			EventID: "event_g38c2_task3_fill_rejected", OrderID: order.OrderID, Type: "FILL_RECORDED", Source: "synthetic",
			Quantity: "1", Price: "100", OccurredAt: svc.now().UTC().Format(time.RFC3339Nano),
			PaperAuthorizationID: authorization.AuthorizationID,
		}
		if err := insertOrderEvent(context.Background(), tx, fill, svc.now().UTC().Format(time.RFC3339Nano)); err == nil {
			tx.Rollback()
			t.Fatal("Task 3 admitted a paper-authorized fill")
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if err := svc.db.QueryRow(`SELECT COUNT(*) FROM order_events WHERE event_type='FILL_RECORDED'`).Scan(&events); err != nil || events != 0 {
			t.Fatalf("rejected paper fill rows=%d err=%v", events, err)
		}
		client := newSyntheticKiwoomClient(t, KiwoomMock, kiwoomRoundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("paper order reached Kiwoom transport")
			return nil, nil
		}))
		dispatch := mustKiwoomMockLimitOrderDispatch(t, client)
		if submitted, err := svc.submitAuthorizedLimitOrder(context.Background(), dispatch, order.OrderID, lease.FencingToken); err == nil || submitted != nil {
			t.Fatalf("paper order entered Kiwoom submit: state=%+v err=%v", submitted, err)
		}
		for _, statement := range []string{
			`UPDATE paper_execution_authorizations SET quantity='9'`,
			`DELETE FROM paper_execution_authorizations`,
		} {
			if _, err := svc.db.Exec(statement); err == nil {
				t.Fatalf("paper authorization mutation accepted: %s", statement)
			}
		}
	})

	for _, boundary := range []struct {
		name   string
		offset time.Duration
		accept bool
	}{
		{"direct authorization at expiry minus one nanosecond", -time.Nanosecond, true},
		{"direct authorization at expiry", 0, false},
	} {
		t.Run(boundary.name, func(t *testing.T) {
			svc, signal, _ := g38c2PaperSignalFixture(t, true)
			signal.TargetQuantity = "10"
			signalEvent, err := recordG38C2PaperSignalForTest(svc, k2aAccountRef, signal)
			if err != nil {
				t.Fatal(err)
			}
			tx, err := svc.db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			state, err := svc.recordOrderIntentTx(context.Background(), tx, capitalizedPaperOrderIntent(*signalEvent, "BUY", "10"))
			if err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			lease := mustK2CLease(t, svc, k2aAccountRef)
			authority, err := loadExecutionAuthoritySnapshot(context.Background(), svc.db, k2aAccountRef)
			if err != nil {
				t.Fatal(err)
			}
			expires, _ := canonicalUTCTime(lease.LeaseExpiresAt)
			authorization := paperExecutionAuthorization{
				AuthorizationID: paperEventID("authorization", state.OrderID), SchemaVersion: paperExecutionAuthorizationSchema,
				OrderID: state.OrderID, AccountRef: k2aAccountRef, PaperAccountingSessionID: signalEvent.PaperAccountingSessionID,
				ExecutionPolicySHA256: signalEvent.ExecutionPolicySHA256, PolicyVersion: paperAccountingPolicyVersion,
				Side: "BUY", Quantity: "10", AuthorityEventID: authority.EventID, FencingToken: lease.FencingToken,
				RiskEventID: paperEventID("risk", state.OrderID), DispatchEventID: paperEventID("dispatch", state.OrderID),
				AuthorizedAt: expires.Add(boundary.offset).Format(time.RFC3339Nano),
			}
			tx, err = svc.db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			err = insertPaperExecutionAuthorization(context.Background(), tx, authorization)
			if boundary.accept && err != nil {
				tx.Rollback()
				t.Fatalf("authorization before lease expiry was rejected: %v", err)
			}
			if !boundary.accept && err == nil {
				tx.Rollback()
				t.Fatal("direct authorization at lease expiry was accepted")
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := svc.db.QueryRow(`SELECT COUNT(*) FROM paper_execution_authorizations`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("authorization boundary rows=%d err=%v", count, err)
			}
		})
	}

	for _, test := range []struct {
		name   string
		mutate func(*Store, *ExecutionAuthorityState)
		token  func(*ExecutionAuthorityState) int64
	}{
		{name: "wrong fence", token: func(lease *ExecutionAuthorityState) int64 { return lease.FencingToken + 1 }},
		{name: "stale fence", token: func(lease *ExecutionAuthorityState) int64 { return lease.FencingToken - 1 }},
		{name: "foreign owner", mutate: func(svc *Store, _ *ExecutionAuthorityState) { svc.executionOwner = "foreign_owner" }},
		{name: "expired", mutate: func(svc *Store, lease *ExecutionAuthorityState) {
			expires, _ := canonicalUTCTime(lease.LeaseExpiresAt)
			svc.now = func() time.Time { return expires }
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, signal, _ := g38c2PaperSignalFixture(t, true)
			signal.TargetQuantity, signal.ExpiresAt = "10", "2026-01-12T00:00:00.000000000Z"
			lease := mustK2CLease(t, svc, k2aAccountRef)
			if test.mutate != nil {
				test.mutate(svc, lease)
			}
			token := lease.FencingToken
			if test.token != nil {
				token = test.token(lease)
			}
			before := paperAdmissionCountsForTest(t, svc)
			if _, _, err := svc.AdmitPaperSignal(context.Background(), k2aAccountRef, signal, token); err == nil {
				t.Fatal("invalid lease authorized paper dispatch")
			}
			if after := paperAdmissionCountsForTest(t, svc); after != before {
				t.Fatalf("lease failure leaked rows: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestG38C2PaperAuthorizationBackup(t *testing.T) {
	svc, signal, _ := g38c2PaperSignalFixture(t, true)
	signal.TargetQuantity = "10"
	lease := mustK2CLease(t, svc, k2aAccountRef)
	_, order, err := svc.AdmitPaperSignal(context.Background(), k2aAccountRef, signal, lease.FencingToken)
	if err != nil {
		t.Fatal(err)
	}
	golden := mixedWriteCurrentSnapshot(t, svc)
	backup := filepath.Join(t.TempDir(), "paper-authorization.db")
	manifest, err := mixedBackupModule(t, svc, svc.now, svc.id).CreateBackup(context.Background(), maintenancecontracts.BackupRequest{OutputPath: backup, GoldenPath: golden, ManifestPath: backup + ".manifest.json"})
	if err != nil || manifest.SchemaVersion != "omni-folio.sqlite.v27" || manifest.PaperExecutionAuthorizationCount != 1 || manifest.PaperCapitalizedFillCount != 0 {
		t.Fatalf("paper authorization manifest=%+v err=%v", manifest, err)
	}
	if err := mixedBackupModule(t, svc, svc.now, svc.id).VerifyRestore(context.Background(), backup, golden); err != nil {
		t.Fatal(err)
	}
	restoredDB, err := openExistingDB(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer restoredDB.Close()
	restored, err := newService(restoredDB, time.Now, randomID).LoadOrderState(context.Background(), order.OrderID)
	if err != nil || restored.Status != "OPEN" {
		t.Fatalf("restored paper order=%+v err=%v", restored, err)
	}
}

func TestLocalPaperHeartbeatRetainsAuthorityPastOriginalExpiry(t *testing.T) {
	svc, signal, _ := paperProposalFixture(t)
	ctx := context.Background()
	now := mustTime("2026-01-10T07:00:00Z")
	svc.now = func() time.Time { return now }
	claim, err := svc.acquirePaperRunnerLease(ctx, k2aAccountRef)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.startLocalPaperExecutionWithClaim(ctx, k2aAccountRef, signal.StrategySelectionEventID, claim)
	if err != nil {
		t.Fatal(err)
	}
	firstClaim, firstFence := *claim, lease.FencingToken
	proposal := paperProposalForTest(t, svc, signal, "golden_cross")
	_, order, err := svc.processPaperProposal(ctx, k2aAccountRef, signal.StrategySelectionEventID, proposal, signal.SignalBarObservationID, lease.FencingToken, true, claim)
	if err != nil || order == nil {
		t.Fatalf("initial admission: %+v %v", order, err)
	}
	for i := 0; i < 4; i++ {
		now = now.Add(10 * time.Second)
		nextClaim, nextLease, err := svc.heartbeatLocalPaperExecution(ctx, claim, lease.FencingToken)
		if err != nil {
			t.Fatal(err)
		}
		if nextClaim.FencingToken != claim.FencingToken || nextLease.FencingToken != lease.FencingToken+1 || nextLease.LeaseOwner != lease.LeaseOwner {
			t.Fatal("unexpected renewal identity")
		}
		claim, lease = nextClaim, nextLease
	}
	if _, err := svc.requireCurrentSyntheticExecutionLease(ctx, svc.db, k2aAccountRef, firstFence, now); err == nil {
		t.Fatal("stale execution fence retained authority")
	}
	if err := validatePaperRunnerLeaseTx(ctx, svc.db, &firstClaim, k2aAccountRef, svc.paperRunnerOwner, now); err == nil {
		t.Fatal("old global heartbeat tuple retained authority")
	}
	_, replay, err := svc.processPaperProposal(ctx, k2aAccountRef, signal.StrategySelectionEventID, proposal, signal.SignalBarObservationID, lease.FencingToken, true, claim)
	if err != nil || replay == nil || replay.OrderID != order.OrderID {
		t.Fatalf("renewed authority lost original admission: %+v %v", replay, err)
	}
	recordG38C2FillBar(t, svc, "renewed-fill-bar", "101", "100", "2026-01-10")
	if _, err := svc.runPaperOrderWithClaim(ctx, order.OrderID, firstFence, claim); err == nil {
		t.Fatal("stale execution fence filled order")
	}
	filled, err := svc.runPaperOrderWithClaim(ctx, order.OrderID, lease.FencingToken, claim)
	if err != nil || filled.Status != "FILLED" || filled.FilledQuantity != "10" {
		t.Fatalf("renewed fill: %+v %v", filled, err)
	}
	latest, err := loadExecutionAuthoritySnapshot(ctx, svc.db, k2aAccountRef)
	if err != nil {
		t.Fatal(err)
	}
	var fillFence int64
	var fillAuthority string
	if err := svc.db.QueryRow(`SELECT json_extract(event_json,'$.fencing_token'),json_extract(event_json,'$.execution_authority_event_id') FROM order_events WHERE order_id=? AND event_type='FILL_RECORDED'`, order.OrderID).Scan(&fillFence, &fillAuthority); err != nil {
		t.Fatal(err)
	}
	if fillFence != lease.FencingToken || fillAuthority != latest.EventID {
		t.Fatal("fill did not use newest renewal authority")
	}
	if _, err := provePaperPerformancePolicyRecovery(ctx, svc.db); err != nil {
		t.Fatal(err)
	}
	if err := svc.haltOwnedSyntheticExecutionLease(ctx, k2aAccountRef, lease.FencingToken); err != nil {
		t.Fatal(err)
	}
	if err := svc.releasePaperRunnerLease(ctx, claim); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "renewed.db")
	golden := mixedWriteCurrentSnapshot(t, svc)
	if _, err := mixedBackupModule(t, svc, svc.now, svc.id).CreateBackup(context.Background(), maintenancecontracts.BackupRequest{OutputPath: backup, GoldenPath: golden, ManifestPath: backup + ".manifest.json"}); err != nil {
		t.Fatal(err)
	}
	if err := mixedBackupModule(t, svc, svc.now, svc.id).VerifyManifest(context.Background(), backup, golden, backup+".manifest.json"); err != nil {
		t.Fatal(err)
	}
	if err := mixedBackupModule(t, svc, svc.now, svc.id).VerifyRestore(context.Background(), backup, golden); err != nil {
		t.Fatal(err)
	}
}

func TestK2CDatabaseBypassAndWeakRestoreGuardAreRejected(t *testing.T) {
	svc, _ := testService(t, nil, nil)
	order := mustRecordK2AOrder(t, svc, "client-db-bypass")
	event := k2aEvent("direct-sql-risk", order.OrderID, "RISK_APPROVED")
	raw, hash, err := orderJSONHash(event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`INSERT INTO order_events(event_id,event_sha256,order_id,event_type,source,event_json,recorded_at) VALUES(?,?,?,?,?,?,?)`,
		event.EventID, hash, event.OrderID, event.Type, event.Source, string(raw), "2026-01-10T15:00:00Z"); err == nil {
		t.Fatal("direct SQL inserted risk approval without a reservation")
	}
	lease := mustK2CLease(t, svc, k2aAccountRef)
	forgedOrder := mustRecordK2AOrder(t, svc, "client-db-forged-reservation")
	var authorityEventID string
	if err := svc.db.QueryRow(`SELECT event_id FROM execution_authority_events WHERE account_ref=? AND reason_code='lease_acquired' ORDER BY sequence DESC LIMIT 1`, k2aAccountRef).Scan(&authorityEventID); err != nil {
		t.Fatal(err)
	}
	tx, err := svc.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	forgedReservation := riskReservationRecord{
		ReservationID: "risk_reservation_direct_sql",
		OrderID:       forgedOrder.OrderID, AccountRef: forgedOrder.AccountRef, PolicyVersion: syntheticRiskPolicyVersion,
		AuthorityEventID: authorityEventID, FencingToken: lease.FencingToken, Quantity: forgedOrder.Quantity,
		LimitPrice: forgedOrder.LimitPrice, LimitNotional: "10000", RiskEventID: "direct_sql_forged_risk",
		DispatchEventID: "direct_sql_forged_dispatch", ReservedAt: "2026-01-10T15:00:00Z",
	}
	if err := insertRiskReservation(context.Background(), tx, forgedReservation); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	forgedEvent := k2aEvent(forgedReservation.RiskEventID, forgedOrder.OrderID, "RISK_APPROVED")
	raw, hash, err = orderJSONHash(forgedEvent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.Exec(`INSERT INTO order_events(event_id,event_sha256,order_id,event_type,source,event_json,recorded_at,authority_reservation_id) VALUES(?,?,?,?,?,?,?,?)`,
		forgedEvent.EventID, hash, forgedEvent.OrderID, forgedEvent.Type, forgedEvent.Source, string(raw), "2026-01-10T15:00:00Z", forgedReservation.ReservationID); err == nil {
		t.Fatal("direct SQL inserted authority event without matching metadata")
	}

	restoreSvc, _ := testService(t, nil, nil)
	golden := mixedWriteCurrentSnapshot(t, restoreSvc)
	backup := filepath.Join(t.TempDir(), "weak-guard.db")
	if _, err := mixedBackupModule(t, restoreSvc, time.Now, randomID).CreateBackup(context.Background(), maintenancecontracts.BackupRequest{OutputPath: backup, GoldenPath: golden, ManifestPath: backup + ".manifest.json"}); err != nil {
		t.Fatal(err)
	}
	candidate, err := openExistingDB(backup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := candidate.Exec(`
DROP TRIGGER order_events_risk_reservation_guard;
CREATE TRIGGER order_events_risk_reservation_guard
BEFORE INSERT ON order_events
WHEN 0
BEGIN
    SELECT RAISE(ABORT, 'risk approval requires an authority reservation');
END;`); err != nil {
		candidate.Close()
		t.Fatal(err)
	}
	if err := candidate.Close(); err != nil {
		t.Fatal(err)
	}
	if err := mixedBackupModule(t, restoreSvc, time.Now, randomID).VerifyRestore(context.Background(), backup, golden); err == nil {
		t.Fatal("restore accepted a disabled authority guard trigger")
	}
}

func mixedBackupOwners(db *sql.DB, now func() time.Time, id func(string) string) (maintenance.Owners, error) {
	registry := observation.New(db, now)
	ledgerAPI, err := ledger.Compose(ledger.Config{DB: db, Observations: registry, Now: now, ID: id, PreviewTTL: func() time.Duration { return 15 * time.Minute }})
	if err != nil {
		return maintenance.Owners{}, err
	}
	marketModule, err := market.Compose(market.Config{DB: db, Observations: registry, Ledger: ledgerAPI, Now: now, ID: id})
	if err != nil {
		return maintenance.Owners{}, err
	}
	tradingStore := New(db, now, id, "mixed-backup-execution", "mixed-backup-runner")
	if err := tradingStore.BindRecoveryReaders(registry); err != nil {
		return maintenance.Owners{}, err
	}
	watchModule, err := watch.Compose(db, registry, nil, now)
	if err != nil {
		return maintenance.Owners{}, err
	}
	return maintenance.Owners{Observations: registry, Ledger: ledgerAPI, Market: marketModule.Recovery, Trading: tradingStore, Watchlist: watchModule.Recovery}, nil
}

func mixedBackupModule(t testing.TB, svc *Store, now func() time.Time, id func(string) string) maintenancecontracts.Backups {
	t.Helper()
	owners, err := mixedBackupOwners(svc.db.DB, now, id)
	if err != nil {
		t.Fatal(err)
	}
	module, err := maintenance.Compose(maintenance.Config{DB: svc.db.DB, Observations: owners.Observations, Trading: owners.Trading, Now: now, ID: id, NewOwners: func(db *sql.DB) (maintenance.Owners, error) { return mixedBackupOwners(db, now, id) }})
	if err != nil {
		t.Fatal(err)
	}
	return module.Backups
}

func mixedWriteCurrentSnapshot(t testing.TB, svc *Store) string {
	t.Helper()
	registry := observation.New(svc.db.DB, svc.now)
	api, err := ledger.Compose(ledger.Config{DB: svc.db.DB, Observations: registry, Now: svc.now, ID: svc.id, PreviewTTL: func() time.Duration { return 15 * time.Minute }})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := api.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "golden-snapshot.json")
	if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
