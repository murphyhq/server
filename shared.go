package murphy

import (
	"crypto/sha256"
	"errors"
	"strings"
	"time"
	"uuid"

	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid")
	ErrConflict = errors.New("conflict")
)

type FieldError struct {
	Field  string
	Reason string
}

func (e *FieldError) Error() string {
	return e.Field + " " + e.Reason
}

func (e *FieldError) Is(target error) bool {
	return target == ErrInvalid
}

type NotFoundError struct {
	Entity string
}

func (e *NotFoundError) Error() string {
	return e.Entity + " not found"
}

func (e *NotFoundError) Is(target error) bool {
	return target == ErrNotFound
}

func NewID() string {
	return uuid.NewV7().String()
}

func DerivedID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	var id uuid.UUID
	copy(id[:], sum[:16])
	id[6] = id[6]&0x0f | 0x80
	id[8] = id[8]&0x3f | 0x80
	return id.String()
}

func Now() *timestamppb.Timestamp {
	return timestamppb.New(time.Now().UTC().Truncate(time.Microsecond))
}

func LookupID(entity, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return &NotFoundError{Entity: entity}
	}
	return nil
}

func Or[T comparable](value, fallback T) T {
	var zero T
	if value == zero {
		return fallback
	}
	return value
}

func invalid(field, reason string) error {
	return &FieldError{Field: field, Reason: reason}
}

func requireName(field, value string) error {
	switch {
	case strings.TrimSpace(value) == "":
		return invalid(field, "is required")
	case len(value) > 255:
		return invalid(field, "must be at most 255 characters")
	}
	return nil
}

func requireID(field, value string) error {
	if _, err := uuid.Parse(value); err != nil {
		return invalid(field, "must be a UUID")
	}
	return nil
}

func (r NotificationReason) Text() string {
	return enumText(r, "NOTIFICATION_REASON_")
}

func (u Urgency) Text() string {
	return enumText(u, "URGENCY_")
}

func UrgencyFromText(text string) Urgency {
	return enumFromText[Urgency](Urgency_value, "URGENCY_", text)
}

type protoEnum interface {
	~int32
	String() string
}

func enumText[E protoEnum](value E, prefix string) string {
	if value == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(value.String(), prefix))
}

func enumFromText[E ~int32](values map[string]int32, prefix, text string) E {
	if text == "" {
		return 0
	}
	value, ok := values[prefix+strings.ToUpper(text)]
	if !ok || value == 0 {
		return -1
	}
	return E(value)
}
