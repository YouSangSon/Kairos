package ledger

import ledgerdomain "omni-folio/services/core/internal/modules/ledger/internal/domain"

func ComposeIdentifiers() func(string) string { return ledgerdomain.InstrumentIDForSymbol }
