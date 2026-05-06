package config

import (
	"os"
	"strings"
	"testing"
)

func TestValidateImportShapeAcceptsExample(t *testing.T) {
	file, err := os.Open("../../config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := ValidateImportShape(file); err != nil {
		t.Fatalf("validate config.example.json: %v", err)
	}
}

func TestValidateImportShapeRejectsGlobalScanAliases(t *testing.T) {
	payload := `{"scanning":{"global_scann":[]}}`
	if err := ValidateImportShape(strings.NewReader(payload)); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateImportShapeRejectsUnknownTopLevelKeys(t *testing.T) {
	payload := `{"scan_roots":[]}`
	if err := ValidateImportShape(strings.NewReader(payload)); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateImportShapeRejectsUnknownScanningKeys(t *testing.T) {
	payload := `{"scanning":{"custom_roots":[]}}`
	if err := ValidateImportShape(strings.NewReader(payload)); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateImportShapeRejectsSensitiveLiterals(t *testing.T) {
	payload := `{"external_databases":{"username":"admin","password":"secret"}}`
	if err := ValidateImportShape(strings.NewReader(payload)); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateImportShapeRejectsTrailingPayload(t *testing.T) {
	data, err := os.ReadFile("../../config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateImportShape(strings.NewReader(string(data) + ` {}`)); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsSensitiveLiteralsWhenExternalDatabaseDisabled(t *testing.T) {
	file, err := os.Open("../../config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	cfg, err := Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ExternalDatabase.Enabled = false
	cfg.ExternalDatabase.Username = "admin"
	cfg.ExternalDatabase.Password = "secret"
	if err := Validate(cfg); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateRejectsNonPositiveRepositoryPollInterval(t *testing.T) {
	file, err := os.Open("../../config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	cfg, err := Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Repositories.PollInterval = "0s"
	if err := Validate(cfg); err == nil {
		t.Fatal("expected zero poll interval validation error")
	}
	cfg.Repositories.PollInterval = "-1s"
	if err := Validate(cfg); err == nil {
		t.Fatal("expected negative poll interval validation error")
	}
}
