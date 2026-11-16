package ledger

import (
	"context"
	"errors"
	c "omni-folio/services/core/internal/modules/ledger/contracts"
	"reflect"
	"testing"
)

type syncBrokerProbe struct {
	c.Broker
	steps *[]string
	fail  string
}

func (p syncBrokerProbe) step(name string) error {
	*p.steps = append(*p.steps, name)
	if p.fail == name {
		return errors.New("private error")
	}
	return nil
}
func (p syncBrokerProbe) VerifyBrokerSync(context.Context) error { return p.step("preflight") }
func (p syncBrokerProbe) CheckBrokerBinding(_ context.Context, account string) error {
	if account == "" {
		return p.step("binding-empty")
	}
	return p.step("binding-expected")
}
func (p syncBrokerProbe) RecordMockBrokerSnapshot(_ context.Context, ledgerAccount, expected string, snapshot *c.BrokerSnapshot) (*c.BrokerKnownGoodSnapshot, error) {
	if ledgerAccount != "account-main" || expected != "opaque-account" || snapshot.AccountRef != expected {
		panic("binding contract changed")
	}
	if err := p.step("store"); err != nil {
		return nil, err
	}
	return &c.BrokerKnownGoodSnapshot{Provider: "kiwoom", Snapshot: *snapshot}, nil
}

type syncFactoryProbe struct {
	broker syncBrokerProbe
	actual string
}

func (p syncFactoryProbe) Open(context.Context) (MockSnapshot, error) {
	if err := p.broker.step("open"); err != nil {
		return nil, err
	}
	return p, nil
}
func (p syncFactoryProbe) AccountRef() string { return "opaque-account" }
func (p syncFactoryProbe) Snapshot(context.Context) (*c.BrokerSnapshot, error) {
	if err := p.broker.step("read"); err != nil {
		return nil, err
	}
	return &c.BrokerSnapshot{AccountRef: p.actual}, nil
}
func (p syncFactoryProbe) Close() { _ = p.broker.step("close") }
func TestMockSyncPreservesPreflightBindingReadStoreOrder(t *testing.T) {
	for _, tc := range []struct {
		name, fail, account string
		want                []string
	}{
		{"preflight", "preflight", "opaque-account", []string{"preflight"}},
		{"old binding", "binding-empty", "opaque-account", []string{"preflight", "binding-empty"}},
		{"secret", "open", "opaque-account", []string{"preflight", "binding-empty", "open"}},
		{"configured binding", "binding-expected", "opaque-account", []string{"preflight", "binding-empty", "open", "binding-expected", "close"}},
		{"provider", "read", "opaque-account", []string{"preflight", "binding-empty", "open", "binding-expected", "read", "close"}},
		{"actual account", "", "different-account", []string{"preflight", "binding-empty", "open", "binding-expected", "read", "close"}},
		{"store", "store", "opaque-account", []string{"preflight", "binding-empty", "open", "binding-expected", "read", "store", "close"}},
		{"success", "", "opaque-account", []string{"preflight", "binding-empty", "open", "binding-expected", "read", "store", "close"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var steps []string
			b := syncBrokerProbe{steps: &steps, fail: tc.fail}
			service := MockSync{Broker: b, Factory: syncFactoryProbe{broker: b, actual: tc.account}}
			receipt, err := service.Run(context.Background(), "account-main")
			if !reflect.DeepEqual(steps, tc.want) {
				t.Fatalf("steps=%v want=%v", steps, tc.want)
			}
			if (err == nil) != (tc.name == "success") || (receipt != nil) != (tc.name == "success") {
				t.Fatalf("receipt=%+v err=%v", receipt, err)
			}
		})
	}
}
