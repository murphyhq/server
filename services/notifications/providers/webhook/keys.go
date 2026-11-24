package webhook

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const (
	privatePrefix = "whsk_"
	publicPrefix  = "whpk_"
)

func ParseSigningKeys(text string) ([]ed25519.PrivateKey, error) {
	var keys []ed25519.PrivateKey
	for i, field := range strings.Split(text, ",") {
		raw, err := decode(strings.TrimSpace(field), privatePrefix, ed25519.PrivateKeySize)
		if err != nil {
			return nil, fmt.Errorf("signing key %d: %w", i+1, err)
		}
		keys = append(keys, ed25519.PrivateKey(raw))
	}
	return keys, nil
}

func ParsePublicKeys(text string) ([]ed25519.PublicKey, error) {
	var keys []ed25519.PublicKey
	for i, field := range strings.Split(text, ",") {
		raw, err := decode(strings.TrimSpace(field), publicPrefix, ed25519.PublicKeySize)
		if err != nil {
			return nil, fmt.Errorf("public key %d: %w", i+1, err)
		}
		keys = append(keys, ed25519.PublicKey(raw))
	}
	return keys, nil
}

func EncodePrivateKey(key ed25519.PrivateKey) string {
	return privatePrefix + base64.StdEncoding.EncodeToString(key)
}

func EncodePublicKey(key ed25519.PublicKey) string {
	return publicPrefix + base64.StdEncoding.EncodeToString(key)
}

func decode(field, prefix string, size int) ([]byte, error) {
	encoded, ok := strings.CutPrefix(field, prefix)
	if !ok {
		return nil, fmt.Errorf("must start with %s", prefix)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) != size {
		return nil, errors.New("must be a base64-encoded Ed25519 key")
	}
	return raw, nil
}
