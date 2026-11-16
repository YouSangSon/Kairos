package architecture

import "omni-folio/services/core/internal/storage/ownership"

func checkOwnedSQL(owner, source string, objects []string) error {
	return ownership.ValidateSQL(owner, source, objects)
}
func checkSchemaSQL(source string) error { _, err := ownership.ValidateSchema(source); return err }
