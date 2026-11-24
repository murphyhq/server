//go:build integration

package testenv

import (
	"encoding/base64"
	"strings"
)

var IntegrationKeySecrets = "1:" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))

const (
	WebhookSigningKey = "whsk_Wtw42WNCjg4pDvUtbg6b77DqoXTuC798z9AmAO1Jdo+AFDVZGp1h6pUQh/Blf2Dxz+AqqxRo6MVJ6oaCKAtNnQ=="
	WebhookPublicKey  = "whpk_gBQ1WRqdYeqVEIfwZX9g8c/gKqsUaOjFSeqGgigLTZ0="
)
