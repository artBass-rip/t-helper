package storage

import (
	"reflect"
	"testing"
)

func TestDialectPlaceholderAndInList(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		placeholder string
		inList      string
	}{
		{name: "sqlite", provider: "sqlite", placeholder: "?", inList: "?, ?, ?"},
		{name: "postgres", provider: "postgres", placeholder: "$2", inList: "$2, $3, $4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialect := NewDialect(tt.provider)
			if got := dialect.Placeholder(2); got != tt.placeholder {
				t.Fatalf("Placeholder(2) = %q, want %q", got, tt.placeholder)
			}
			if got := dialect.InList(2, 3); got != tt.inList {
				t.Fatalf("InList(2, 3) = %q, want %q", got, tt.inList)
			}
		})
	}
}

func TestDialectPlaceholdersRejectsEmptyCount(t *testing.T) {
	if got := NewDialect("postgres").Placeholders(1, 0); got != nil {
		t.Fatalf("Placeholders(1, 0) = %#v, want nil", got)
	}
}

func TestDialectTimeExprAndBoolArg(t *testing.T) {
	sqlite := NewDialect("sqlite")
	if got := sqlite.TimeExpr("created_at"); got != "created_at" {
		t.Fatalf("sqlite TimeExpr = %q", got)
	}
	if !reflect.DeepEqual([]any{sqlite.BoolArg(true), sqlite.BoolArg(false)}, []any{1, 0}) {
		t.Fatalf("sqlite BoolArg values differ")
	}

	postgres := NewDialect("postgres")
	if got := postgres.TimeExpr("created_at"); got != "created_at::text" {
		t.Fatalf("postgres TimeExpr = %q", got)
	}
	if !reflect.DeepEqual([]any{postgres.BoolArg(true), postgres.BoolArg(false)}, []any{true, false}) {
		t.Fatalf("postgres BoolArg values differ")
	}
}
