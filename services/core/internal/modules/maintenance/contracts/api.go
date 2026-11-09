package contracts

import "context"

type Migrations interface{ Migrate(context.Context) error }
