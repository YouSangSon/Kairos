package migrations

import (
	"embed"
	"strings"
)

//go:embed *.sql
var scripts embed.FS

type source struct{}

var Files source

func (source) ReadFile(name string) ([]byte, error) {
	return scripts.ReadFile(strings.TrimPrefix(name, "migrations/"))
}
