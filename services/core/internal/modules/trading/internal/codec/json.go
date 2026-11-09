package codec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func JSONHash(value any) ([]byte, string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	hash := sha256.Sum256(raw)
	return raw, hex.EncodeToString(hash[:]), nil
}
