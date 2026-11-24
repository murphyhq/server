package webhook

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

const (
	HeaderID        = "webhook-id"
	HeaderTimestamp = "webhook-timestamp"
	HeaderSignature = "webhook-signature"

	signatureVersion = "v1a"
)

var ErrBadSignature = errors.New("the webhook's signature doesn't match any key")

func sign(keys []ed25519.PrivateKey, id string, at time.Time, body []byte) string {
	content := signedContent(id, strconv.FormatInt(at.Unix(), 10), body)
	signatures := make([]string, len(keys))
	for i, key := range keys {
		signatures[i] = signatureVersion + "," + base64.StdEncoding.EncodeToString(ed25519.Sign(key, content))
	}
	return strings.Join(signatures, " ")
}

func Verify(keys []ed25519.PublicKey, id, timestamp, signatures string, body []byte, now time.Time, tolerance time.Duration) error {
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errors.New("the webhook's timestamp is not a number of seconds")
	}
	if age := now.Sub(time.Unix(seconds, 0)); age > tolerance || age < -tolerance {
		return errors.New("the webhook's timestamp is too far from now")
	}
	content := signedContent(id, timestamp, body)
	for _, signature := range strings.Fields(signatures) {
		version, encoded, ok := strings.Cut(signature, ",")
		if !ok || version != signatureVersion {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			continue
		}
		for _, key := range keys {
			if ed25519.Verify(key, content, raw) {
				return nil
			}
		}
	}
	return ErrBadSignature
}

func signedContent(id, timestamp string, body []byte) []byte {
	return []byte(id + "." + timestamp + "." + string(body))
}
