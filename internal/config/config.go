package config

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

const prefix = "MURPHY_"

var standard = []string{"AWS_", "OTEL_"}

func Load(dst any) error {
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return errors.New("load configuration: destination must be a pointer to a struct")
	}

	var errs []error
	load(v.Elem(), &errs)
	return errors.Join(errs...)
}

func load(v reflect.Value, errs *[]error) {
	t := v.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		name, ok := field.Tag.Lookup("env")
		if !ok {
			if field.Type.Kind() == reflect.Struct && field.Type != reflect.TypeFor[time.Time]() {
				load(v.Field(i), errs)
			}
			continue
		}

		if !slices.ContainsFunc(standard, func(p string) bool { return strings.HasPrefix(name, p) }) {
			name = prefix + name
		}
		if err := set(v.Field(i), field, name); err != nil {
			*errs = append(*errs, fmt.Errorf("%s: %w", name, err))
		}
	}
}

func set(v reflect.Value, field reflect.StructField, name string) error {
	raw, ok := os.LookupEnv(name)
	if !ok || raw == "" {
		raw = field.Tag.Get("default")
	}
	if raw == "" {
		if field.Tag.Get("required") == "true" {
			return errors.New("required but not set")
		}
		return nil
	}
	return parse(v, raw)
}

func parse(v reflect.Value, raw string) error {
	if v.Type() == reflect.TypeFor[time.Duration]() {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return errors.New("not a valid duration")
		}
		v.SetInt(int64(d))
		return nil
	}

	if v.Type() == reflect.TypeFor[[]string]() {
		var values []string
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				values = append(values, part)
			}
		}
		v.Set(reflect.ValueOf(values))
		return nil
	}

	switch v.Kind() {
	case reflect.String:
		v.SetString(raw)
	case reflect.Int, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, v.Type().Bits())
		if err != nil {
			return errors.New("not a valid integer")
		}
		v.SetInt(n)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return errors.New("not a valid boolean")
		}
		v.SetBool(b)
	default:
		return fmt.Errorf("unsupported type %s", v.Type())
	}
	return nil
}
