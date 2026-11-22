//go:build integration

package testenv

import (
	"encoding/base64"
	"strings"
)

var IntegrationKeySecrets = "1:" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
