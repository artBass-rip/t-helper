package toolchain

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallerDownloadsVerifiesAndInstallsBinary(t *testing.T) {
	t.Parallel()
	archive := zipFixture(t, "nested/tflint", []byte("tool-binary"))
	sum := sha256.Sum256(archive)
	manifest := []byte(hex.EncodeToString(sum[:]) + "  tflint_test.zip\n")
	manifestSum := sha256.Sum256(manifest)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var body []byte
		switch r.URL.Path {
		case "/checksums.txt":
			body = manifest
		case "/tflint_test.zip":
			body = archive
		default:
			return response(http.StatusNotFound, nil), nil
		}
		return response(http.StatusOK, body), nil
	})}

	dir := t.TempDir()
	err := (Installer{
		Dir:    dir,
		Client: client,
		Releases: []Release{{
			Name:            "tflint",
			Version:         "test",
			AssetName:       "tflint_test.zip",
			AssetURL:        "https://example.test/tflint_test.zip",
			ChecksumsURL:    "https://example.test/checksums.txt",
			ChecksumsSHA256: hex.EncodeToString(manifestSum[:]),
			Format:          "zip",
			BinaryName:      "tflint",
		}},
	}).Install(context.Background())
	if err != nil {
		t.Fatalf("install toolchain: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "tflint"))
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if string(raw) != "tool-binary" {
		t.Fatalf("installed binary = %q", raw)
	}
}

func TestInstallerRejectsChecksumMismatch(t *testing.T) {
	t.Parallel()
	archive := zipFixture(t, "tool", []byte("binary"))
	manifest := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  tool.zip\n")
	manifestSum := sha256.Sum256(manifest)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/checksums.txt" {
			return response(http.StatusOK, manifest), nil
		}
		return response(http.StatusOK, archive), nil
	})}
	err := (Installer{Dir: t.TempDir(), Client: client, Releases: []Release{{
		Name: "tool", Version: "test", AssetName: "tool.zip", AssetURL: "https://example.test/tool.zip",
		ChecksumsURL: "https://example.test/checksums.txt", ChecksumsSHA256: hex.EncodeToString(manifestSum[:]), Format: "zip", BinaryName: "tool",
	}}}).Install(context.Background())
	if err == nil {
		t.Fatal("checksum mismatch was accepted")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func response(status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestDefaultReleases(t *testing.T) {
	t.Parallel()
	releases, err := DefaultReleases("darwin", "arm64")
	if err != nil {
		t.Fatalf("default releases: %v", err)
	}
	if len(releases) != 2 || releases[0].AssetName != "tflint_darwin_arm64.zip" || releases[1].AssetName != "trivy_0.71.2_macOS-ARM64.tar.gz" {
		t.Fatalf("unexpected releases: %+v", releases)
	}
	if _, err := DefaultReleases("plan9", "amd64"); err == nil {
		t.Fatal("unsupported OS was accepted")
	}
}

func zipFixture(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	file, err := writer.Create(name)
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := file.Write(content); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buffer.Bytes()
}
