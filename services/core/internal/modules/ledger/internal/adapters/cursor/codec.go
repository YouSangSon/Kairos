package cursor

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"omni-folio/services/core/internal/codec"
	c "omni-folio/services/core/internal/modules/ledger/contracts"
)

func Encode(key [32]byte, cursor c.ActivityCursor) string {
	plaintext, _ := json.Marshal(cursor)
	block, _ := aes.NewCipher(key[:])
	aead, _ := cipher.NewGCM(block)
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	sealed := aead.Seal(nonce, nonce, plaintext, nil)
	return base64.RawURLEncoding.EncodeToString(sealed)
}

func Decode(key [32]byte, raw string) (*c.ActivityCursor, error) {
	if raw == "" || len(raw) > 1024 {
		return nil, errors.New("invalid cursor length")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) > 768 {
		return nil, errors.New("invalid cursor encoding")
	}
	if base64.RawURLEncoding.EncodeToString(decoded) != raw {
		return nil, errors.New("cursor encoding is not canonical")
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(decoded) < aead.NonceSize() {
		return nil, errors.New("invalid cursor ciphertext")
	}
	plaintext, err := aead.Open(nil, decoded[:aead.NonceSize()], decoded[aead.NonceSize():], nil)
	if err != nil || len(plaintext) > 512 {
		return nil, errors.New("invalid cursor authentication")
	}
	var cursor c.ActivityCursor
	decoder := json.NewDecoder(bytes.NewReader(plaintext))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("cursor must contain one object")
	}
	if cursor.Version != 1 || cursor.LedgerRevision < 1 || cursor.Sequence < 1 ||
		cursor.Sequence > cursor.LedgerRevision || !codec.CanonicalUTC(cursor.LedgerRecordedAt) || !codec.CanonicalUTC(cursor.OccurredAt) {
		return nil, errors.New("invalid cursor fields")
	}
	return &cursor, nil
}
