package sqlite

import (
	"omni-folio/services/core/internal/modules/trading/internal/application"
	"omni-folio/services/core/internal/paperdomain"
)

const (
	paperFillPolicyVersion = paperdomain.FillPolicyVersion
	paperMaxQuantity       = paperdomain.MaxQuantity
)

var paperExecutionPolicy = application.PaperExecutionPolicy
