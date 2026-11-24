package webhook

import (
	"crypto/ed25519"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func keyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	return public, private
}

func TestASignatureChecksOutWithEitherKeyDuringARotation(t *testing.T) {
	oldPublic, oldPrivate := keyPair(t)
	newPublic, newPrivate := keyPair(t)
	now := time.Now()
	body := []byte(`{"type":"incident.paged"}`)
	signatures := sign([]ed25519.PrivateKey{oldPrivate, newPrivate}, "id-1", now, body)
	timestamp := strconv.FormatInt(now.Unix(), 10)

	assert.NoError(t, Verify([]ed25519.PublicKey{oldPublic}, "id-1", timestamp, signatures, body, now, 5*time.Minute), "a receiver still on the old key")
	assert.NoError(t, Verify([]ed25519.PublicKey{newPublic}, "id-1", timestamp, signatures, body, now, 5*time.Minute), "a receiver already on the new key")
}

func TestATamperedOrStaleWebhookIsRejected(t *testing.T) {
	public, private := keyPair(t)
	otherPublic, _ := keyPair(t)
	now := time.Now()
	body := []byte(`{"type":"incident.paged"}`)
	signatures := sign([]ed25519.PrivateKey{private}, "id-1", now, body)
	timestamp := strconv.FormatInt(now.Unix(), 10)

	for name, check := range map[string]func() error{
		"another body": func() error {
			return Verify([]ed25519.PublicKey{public}, "id-1", timestamp, signatures, []byte(`{}`), now, time.Minute)
		},
		"another ID": func() error {
			return Verify([]ed25519.PublicKey{public}, "id-2", timestamp, signatures, body, now, time.Minute)
		},
		"another key": func() error {
			return Verify([]ed25519.PublicKey{otherPublic}, "id-1", timestamp, signatures, body, now, time.Minute)
		},
		"an old timestamp": func() error {
			return Verify([]ed25519.PublicKey{public}, "id-1", timestamp, signatures, body, now.Add(10*time.Minute), time.Minute)
		},
		"no signature": func() error {
			return Verify([]ed25519.PublicKey{public}, "id-1", timestamp, "", body, now, time.Minute)
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, check())
		})
	}
}

func TestKeysRoundTripThroughTheirText(t *testing.T) {
	public, private := keyPair(t)

	privates, err := ParseSigningKeys(EncodePrivateKey(private))
	require.NoError(t, err)
	publics, err := ParsePublicKeys(EncodePublicKey(public) + ", " + EncodePublicKey(public))
	require.NoError(t, err)

	assert.Equal(t, []ed25519.PrivateKey{private}, privates)
	assert.Len(t, publics, 2)
	_, err = ParseSigningKeys(EncodePublicKey(public))
	assert.Error(t, err, "a public key is not a signing key")
}
