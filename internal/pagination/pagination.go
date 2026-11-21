package pagination

import (
	"uuid"

	"github.com/murphyhq/server"
)

const (
	defaultLimit = 50
	maxLimit     = 100
)

type Page struct {
	Cursor string
	Limit  int
}

type Window struct {
	Before string
	Limit  int
}

func (w Window) Rows() int {
	return w.Limit + 1
}

func Resolve(page Page) (Window, error) {
	window := Window{Before: uuid.Max().String(), Limit: defaultLimit}
	switch {
	case page.Limit < 0:
		return Window{}, &murphy.FieldError{Field: "limit", Reason: "must not be negative"}
	case page.Limit > 0:
		window.Limit = min(page.Limit, maxLimit)
	}
	if page.Cursor != "" {
		if _, err := uuid.Parse(page.Cursor); err != nil {
			return Window{}, &murphy.FieldError{Field: "cursor", Reason: "is not valid"}
		}
		window.Before = page.Cursor
	}
	return window, nil
}

func Trim[T interface{ GetId() string }](items []T, window Window) ([]T, string) {
	if len(items) <= window.Limit {
		return items, ""
	}
	items = items[:window.Limit]
	return items, items[window.Limit-1].GetId()
}
