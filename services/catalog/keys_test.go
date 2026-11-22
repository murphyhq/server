package catalog_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/murphyhq/server"
	"github.com/murphyhq/server/services/catalog"
)

func secret(fill byte) string {
	return base64.StdEncoding.EncodeToString([]byte(strings.Repeat(string(fill), 32)))
}

func keyring(t *testing.T, spec string) *catalog.Keyring {
	t.Helper()
	k, err := catalog.ParseKeyring(spec)
	require.NoError(t, err)
	return k
}

var integration = &murphy.Integration{Id: "integration-1", ServiceId: "service-1", KeyVersion: 3}

func TestASignedKeyVerifiesToItsIntegration(t *testing.T) {
	k := keyring(t, "1:"+secret('a'))

	key, err := k.Sign(integration)
	require.NoError(t, err)
	claims, err := k.Verify(key)

	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(key, "murphy_ik_"))
	assert.Equal(t, "1", claims.GetSecretId())
	assert.Equal(t, "integration-1", claims.GetIntegrationId())
	assert.Equal(t, "service-1", claims.GetServiceId())
	assert.Equal(t, int32(3), claims.GetKeyVersion())
}

func TestKeysKeepWorkingThroughASecretRotation(t *testing.T) {
	before := keyring(t, "1:"+secret('a'))
	added := keyring(t, "1:"+secret('a')+",2:"+secret('b'))
	promoted := keyring(t, "2:"+secret('b')+",1:"+secret('a'))
	after := keyring(t, "2:"+secret('b'))

	old, err := before.Sign(integration)
	require.NoError(t, err)
	fresh, err := promoted.Sign(integration)
	require.NoError(t, err)

	for name, k := range map[string]*catalog.Keyring{"added": added, "promoted": promoted} {
		_, err := k.Verify(old)
		assert.NoError(t, err, name)
	}
	_, err = after.Verify(fresh)
	assert.NoError(t, err)
	_, err = after.Verify(old)
	assert.ErrorIs(t, err, catalog.ErrInvalidKey)
}

func TestTamperedOrMalformedKeysAreRefused(t *testing.T) {
	k := keyring(t, "1:"+secret('a'))
	key, err := k.Sign(integration)
	require.NoError(t, err)
	payload, sig, _ := strings.Cut(strings.TrimPrefix(key, "murphy_ik_"), ".")
	forged, err := keyring(t, "1:"+secret('z')).Sign(integration)
	require.NoError(t, err)

	for name, candidate := range map[string]string{
		"empty":             "",
		"no prefix":         strings.TrimPrefix(key, "murphy_ik_"),
		"no signature":      "murphy_ik_" + payload,
		"changed signature": "murphy_ik_" + payload + "." + strings.ToUpper(sig),
		"changed payload":   "murphy_ik_" + payload + "x." + sig,
		"other secret":      forged,
		"not base64":        "murphy_ik_!!!." + sig,
	} {
		_, err := k.Verify(candidate)
		assert.ErrorIs(t, err, catalog.ErrInvalidKey, name)
	}
}

func TestKeyringRejectsBadSecrets(t *testing.T) {
	for name, spec := range map[string]string{
		"empty":      "",
		"no id":      ":" + secret('a'),
		"no secret":  "1",
		"not base64": "1:not-base64!",
		"too short":  "1:" + base64.StdEncoding.EncodeToString([]byte("short")),
		"duplicate":  "1:" + secret('a') + ",1:" + secret('b'),
	} {
		_, err := catalog.ParseKeyring(spec)
		assert.Error(t, err, name)
	}
}
