package catalog

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/murphyhq/server"
)

const keyPrefix = "murphy_ik_"

var ErrInvalidKey = errors.New("invalid integration key")

type Keyring struct {
	signing string
	secrets map[string][]byte
}

func ParseKeyring(spec string) (*Keyring, error) {
	k := &Keyring{secrets: map[string][]byte{}}
	for entry := range strings.SplitSeq(spec, ",") {
		id, encoded, ok := strings.Cut(strings.TrimSpace(entry), ":")
		if !ok || id == "" {
			return nil, errors.New("each entry must be <id>:<base64 secret>")
		}
		secret, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("secret %s is not base64", id)
		}
		if len(secret) < 32 {
			return nil, fmt.Errorf("secret %s must be at least 32 bytes", id)
		}
		if _, exists := k.secrets[id]; exists {
			return nil, fmt.Errorf("secret %s appears twice", id)
		}
		k.secrets[id] = secret
		if k.signing == "" {
			k.signing = id
		}
	}
	return k, nil
}

func (k *Keyring) Sign(integration *murphy.Integration) (string, error) {
	data, err := proto.Marshal(&murphy.IntegrationKey{
		SecretId:      k.signing,
		IntegrationId: integration.GetId(),
		ServiceId:     integration.GetServiceId(),
		KeyVersion:    integration.GetKeyVersion(),
	})
	if err != nil {
		return "", fmt.Errorf("encode integration key: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(data)
	return keyPrefix + payload + "." + signature(k.secrets[k.signing], payload), nil
}

func (k *Keyring) Verify(key string) (*murphy.IntegrationKey, error) {
	payload, sig, ok := strings.Cut(strings.TrimPrefix(key, keyPrefix), ".")
	if !ok || !strings.HasPrefix(key, keyPrefix) {
		return nil, ErrInvalidKey
	}
	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, ErrInvalidKey
	}
	claims := &murphy.IntegrationKey{}
	if err := proto.Unmarshal(data, claims); err != nil || claims.GetIntegrationId() == "" {
		return nil, ErrInvalidKey
	}
	secret, ok := k.secrets[claims.GetSecretId()]
	if !ok || !hmac.Equal([]byte(sig), []byte(signature(secret, payload))) {
		return nil, ErrInvalidKey
	}
	return claims, nil
}

func signature(secret []byte, payload string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(keyPrefix + payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
