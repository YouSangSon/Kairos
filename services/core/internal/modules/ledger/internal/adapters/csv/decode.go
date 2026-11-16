package csvimport

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"omni-folio/services/core/internal/modules/ledger/contracts"
	"unicode/utf8"
)

const (
	maxBodyBytes  = 1 << 20
	maxImportRows = 10000
)

type APIError = contracts.APIError

func Decode(body []byte) (contracts.ImportInput, *contracts.Error) {
	if len(body) == 0 {
		return contracts.ImportInput{}, &contracts.Error{Kind: contracts.InvalidInput, Body: APIError{Code: "empty_csv", Message: "CSV body must not be empty"}}
	}
	if len(body) > maxBodyBytes {
		return contracts.ImportInput{}, &contracts.Error{Kind: contracts.InvalidInput, Body: APIError{Code: "csv_too_large", Message: "CSV body must not exceed 1048576 bytes"}}
	}
	if !utf8.Valid(body) {
		return contracts.ImportInput{}, &contracts.Error{Kind: contracts.InvalidInput, Body: APIError{Code: "invalid_encoding", Message: "CSV body must be UTF-8"}}
	}
	r := csv.NewReader(bytes.NewReader(body))
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil || len(records) == 0 {
		return contracts.ImportInput{}, &contracts.Error{Kind: contracts.InvalidInput, Body: APIError{Code: "invalid_csv", Message: "body must be valid CSV with a header"}}
	}
	if len(records)-1 > maxImportRows {
		return contracts.ImportInput{}, &contracts.Error{Kind: contracts.InvalidInput, Body: APIError{Code: "too_many_rows", Message: "CSV must not contain more than 10000 data rows"}}
	}
	required := []string{"source_event_id", "account_id", "type", "occurred_at", "symbol", "quantity", "price", "fee", "currency", "amount"}
	columns := make(map[string]int, len(records[0]))
	for i, name := range records[0] {
		if _, exists := columns[name]; exists {
			return contracts.ImportInput{}, &contracts.Error{Kind: contracts.InvalidInput, Body: APIError{Code: "invalid_csv_header", Message: "CSV header names must be unique"}}
		}
		columns[name] = i
	}
	for _, name := range required {
		if _, ok := columns[name]; !ok {
			return contracts.ImportInput{}, &contracts.Error{Kind: contracts.InvalidInput, Body: APIError{Code: "invalid_csv_header", Message: "CSV is missing required column " + name, Field: name}}
		}
	}

	input := contracts.ImportInput{Rows: make([]contracts.ImportRow, 0, len(records)-1)}
	hash := sha256.Sum256(body)
	input.FileSHA256 = hex.EncodeToString(hash[:])
	for _, record := range records[1:] {
		row := contracts.ImportRow{}
		for name, index := range columns {
			if index < len(record) {
				row[name] = record[index]
			}
		}
		input.Rows = append(input.Rows, row)
	}
	return input, nil
}
