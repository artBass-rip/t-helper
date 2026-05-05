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
