package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const prefix = "MURPHY_"

type Resolver interface {
	Resolve(ctx context.Context, ref string) (string, error)
}

type Option func(*loader)

func WithResolver(scheme string, r Resolver) Option {
	return func(l *loader) {
		l.resolvers[scheme] = r
	}
}

type loader struct {
	resolvers map[string]Resolver
}

func Load(ctx context.Context, dst any, opts ...Option) error {
	l := loader{resolvers: map[string]Resolver{
		"env":  EnvResolver{},
		"file": FileResolver{},
	}}
	for _, opt := range opts {
		opt(&l)
	}

	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return errors.New("load configuration: destination must be a pointer to a struct")
	}

	var errs []error
	l.load(ctx, v.Elem(), &errs)
	return errors.Join(errs...)
}

func (l loader) load(ctx context.Context, v reflect.Value, errs *[]error) {
	t := v.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		name, ok := field.Tag.Lookup("env")
		if !ok {
			if field.Type.Kind() == reflect.Struct && field.Type != reflect.TypeFor[time.Time]() {
				l.load(ctx, v.Field(i), errs)
			}
			continue
		}

		name = prefix + name
		if err := l.set(ctx, v.Field(i), field, name); err != nil {
			*errs = append(*errs, fmt.Errorf("%s: %w", name, err))
		}
	}
}

func (l loader) set(ctx context.Context, v reflect.Value, field reflect.StructField, name string) error {
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

	if field.Tag.Get("secret") == "true" {
		resolved, err := l.resolve(ctx, raw)
		if err != nil {
			return err
		}
		raw = resolved
	}

	return parse(v, raw)
}

func (l loader) resolve(ctx context.Context, raw string) (string, error) {
	scheme, _, ok := strings.Cut(raw, "://")
	if !ok {
		return "", errors.New("secret must be a reference such as env://NAME or file:///path")
	}
	resolver, ok := l.resolvers[scheme]
	if !ok {
		return "", fmt.Errorf("unsupported secret reference scheme %q", scheme)
	}
	value, err := resolver.Resolve(ctx, raw)
	if err != nil {
		return "", fmt.Errorf("resolve %s:// reference: %w", scheme, err)
	}
	return value, nil
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
