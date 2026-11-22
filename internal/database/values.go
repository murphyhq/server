package database

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func Time(ts *timestamppb.Timestamp) *time.Time {
	if ts == nil {
		return nil
	}
	t := ts.AsTime()
	return &t
}

func Timestamp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func Text(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func TextValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
