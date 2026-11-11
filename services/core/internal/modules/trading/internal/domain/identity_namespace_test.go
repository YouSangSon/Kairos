package domain

import (
	"strings"
	"testing"
)

func TestDatedAliasesCannotEnterOrdinaryReferenceNamespace(t *testing.T) {
	for _, kind := range []string{"order", "execution"} {
		ordinary := "kiwoom_" + kind + "_" + strings.Repeat("A", 24)
		dated := "kiwoom_dated_" + kind + "_" + strings.Repeat("A", 24)
		if !OrderAlias(ordinary, kind) {
			t.Fatalf("ordinary %s reference was rejected", kind)
		}
		if OrderAlias(dated, kind) {
			t.Fatalf("dated %s alias entered the ordinary reference namespace", kind)
		}
	}
}
