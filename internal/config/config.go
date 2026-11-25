package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
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
			continue
		}
		if rule := field.Tag.Get("validate"); rule != "" {
			if err := validate(v.Field(i), rule); err != nil {
				*errs = append(*errs, fmt.Errorf("%s: %w", name, err))
			}
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
		v.Set(reflect.ValueOf(list(raw)))
		return nil
	}

	if v.Type() == reflect.TypeFor[[]netip.Prefix]() {
		entries := list(raw)
		prefixes := make([]netip.Prefix, len(entries))
		for i, entry := range entries {
			prefix, err := netip.ParsePrefix(entry)
			if err != nil {
				return fmt.Errorf("entry %d: not a network in CIDR notation, as in 10.0.0.0/8", i+1)
			}
			prefixes[i] = prefix
		}
		v.Set(reflect.ValueOf(prefixes))
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

func list(raw string) []string {
	var values []string
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			values = append(values, part)
		}
	}
	return values
}

var rules = map[string]func(string) error{
	"base-url": baseURL,
	"http-url": httpURL,
}

func validate(v reflect.Value, rule string) error {
	check, ok := rules[rule]
	if !ok {
		return fmt.Errorf("unknown validation rule %q", rule)
	}
	if v.Kind() == reflect.Slice {
		for i := range v.Len() {
			if err := check(v.Index(i).String()); err != nil {
				return fmt.Errorf("entry %d: %w", i+1, err)
			}
		}
		return nil
	}
	if v.String() == "" {
		return nil
	}
	return check(v.String())
}

func httpURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return errors.New("not a URL of the form scheme://host[:port][/path]")
	}
	return nil
}

func baseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return errors.New("not a base URL of the form scheme://host[:port]")
	}
	return nil
}
