package sqlite

import (
	"omni-folio/services/core/internal/modules/trading/internal/application"
	paperdomain "omni-folio/services/core/internal/modules/trading/internal/domain/paper"
)

const (
	paperFillPolicyVersion = paperdomain.FillPolicyVersion
	paperMaxQuantity       = paperdomain.MaxQuantity
)

var paperExecutionPolicy = application.PaperExecutionPolicy
