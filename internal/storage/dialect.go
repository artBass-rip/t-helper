package storage

import (
	"fmt"
	"strings"
)

type Dialect struct {
	provider string
}

func NewDialect(provider string) Dialect {
	return Dialect{provider: provider}
}

func (d Dialect) Placeholder(idx int) string {
	if d.provider == "postgres" {
		return fmt.Sprintf("$%d", idx)
	}
	return "?"
}

func (d Dialect) Placeholders(start, count int) []string {
	if count <= 0 {
		return nil
	}
	out := make([]string, count)
	for i := range out {
		out[i] = d.Placeholder(start + i)
	}
	return out
}

func (d Dialect) InList(start, count int) string {
	return strings.Join(d.Placeholders(start, count), ", ")
}

func (d Dialect) TimeExpr(column string) string {
	if d.provider == "postgres" {
		return column + "::text"
	}
	return column
}

func (d Dialect) BoolArg(value bool) any {
	if d.provider == "postgres" {
		return value
	}
	if value {
		return 1
	}
	return 0
}
