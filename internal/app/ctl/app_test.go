package ctl

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/artBass-rip/t-helper/internal/app/storageproviders"
)

func TestProvidersListsMVPAdapters(t *testing.T) {
	var out bytes.Buffer
	app := New(&out, storageproviders.MVPRegistry())
	if err := app.RunCommand(context.Background(), "providers"); err != nil {
		t.Fatalf("providers: %v", err)
	}

	got := strings.Fields(out.String())
	want := []string{"postgres", "sqlite"}
	if len(got) != len(want) {
		t.Fatalf("providers = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("providers = %v, want %v", got, want)
		}
	}
}

func TestRunCommandRejectsUnknownCommand(t *testing.T) {
	var out bytes.Buffer
	app := New(&out, storageproviders.MVPRegistry())
	if err := app.RunCommand(context.Background(), "unknown"); err == nil {
		t.Fatal("expected error")
	}
}
