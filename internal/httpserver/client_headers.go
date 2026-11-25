package httpserver

import (
	"net/http"
	"strings"
	"unicode"

	"go.opentelemetry.io/otel/attribute"

	"github.com/murphyhq/server/internal/telemetry"
)

const maxClientValue = 64

var clientHeaders = []struct {
	header   string
	name     string
	standard bool
}{
	{"Murphy-App-Version", "murphy.client.app.version", false},
	{"Murphy-App-Build", "murphy.client.app.build", false},
	{"Murphy-OS", "os.name", true},
	{"Murphy-OS-Version", "os.version", true},
	{"Murphy-Device-Model", "device.model.name", true},
	{"Murphy-Locale", "murphy.client.locale", false},
}

type clientValue struct {
	name     string
	value    string
	standard bool
}

func clientValues(r *http.Request) []clientValue {
	values := make([]clientValue, 0, len(clientHeaders))
	for _, c := range clientHeaders {
		if value := cleanClientValue(r.Header.Get(c.header)); value != "" {
			values = append(values, clientValue{name: c.name, value: value, standard: c.standard})
		}
	}
	return values
}

func clientFields(values []clientValue) []telemetry.Field {
	fields := make([]telemetry.Field, 0, len(values))
	for _, v := range values {
		if v.standard {
			fields = append(fields, telemetry.Standard(v.name, v.value))
			continue
		}
		fields = append(fields, telemetry.WithField(v.name, v.value))
	}
	return fields
}

func clientAttributes(values []clientValue) []attribute.KeyValue {
	attributes := make([]attribute.KeyValue, 0, len(values))
	for _, v := range values {
		attributes = append(attributes, attribute.String(v.name, v.value))
	}
	return attributes
}

func cleanClientValue(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
	runes := []rune(strings.TrimSpace(cleaned))
	if len(runes) > maxClientValue {
		runes = runes[:maxClientValue]
	}
	return string(runes)
}
