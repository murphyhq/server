//go:build integration

package testenv

import (
	"encoding/base64"
	"strings"
)

var IntegrationKeySecrets = "1:" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))

const (
	WebhookSigningKey     = "whsk_Wtw42WNCjg4pDvUtbg6b77DqoXTuC798z9AmAO1Jdo+AFDVZGp1h6pUQh/Blf2Dxz+AqqxRo6MVJ6oaCKAtNnQ=="
	WebhookPublicKey      = "whpk_gBQ1WRqdYeqVEIfwZX9g8c/gKqsUaOjFSeqGgigLTZ0="
	NextWebhookSigningKey = "whsk_UW0+6vtujGqtA8mY1l74ElfidVaK+KajuF6iJGP7Yasdb3Ug5E4PquJNExug7UeRFC9yLYyvbz4TdxuNJAzS2g=="
	NextWebhookPublicKey  = "whpk_HW91IOROD6riTRMboO1HkRQvci2Mr28+E3cbjSQM0to="
)
