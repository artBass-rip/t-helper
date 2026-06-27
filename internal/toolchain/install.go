package toolchain

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	TFLintVersion = "0.63.1"
	TrivyVersion  = "0.71.2"
	maxArchive    = 256 << 20
	maxChecksums  = 1 << 20
)

type Release struct {
	Name            string
	Version         string
	AssetName       string
	AssetURL        string
	ChecksumsURL    string
	ChecksumsSHA256 string
	Format          string
	BinaryName      string
}

type Installer struct {
	Client   *http.Client
	Releases []Release
	Dir      string
}

func DefaultReleases(goos, goarch string) ([]Release, error) {
	if goarch != "amd64" && goarch != "arm64" {
		return nil, fmt.Errorf("unsupported toolchain architecture %q", goarch)
	}
	tflintOS := map[string]string{"darwin": "darwin", "linux": "linux", "windows": "windows"}[goos]
	trivyOS := map[string]string{"darwin": "macOS", "linux": "Linux", "windows": "Windows"}[goos]
	if tflintOS == "" || trivyOS == "" {
		return nil, fmt.Errorf("unsupported toolchain operating system %q", goos)
	}
	if goos == "windows" && goarch == "arm64" {
		return nil, fmt.Errorf("unsupported TFLint platform windows/arm64")
	}
	tflintAsset := fmt.Sprintf("tflint_%s_%s.zip", tflintOS, goarch)
	trivyArch := map[string]string{"amd64": "64bit", "arm64": "ARM64"}[goarch]
	trivyFormat := "tar.gz"
	if goos == "windows" {
		trivyFormat = "zip"
	}
	trivyAsset := fmt.Sprintf("trivy_%s_%s-%s.%s", TrivyVersion, trivyOS, trivyArch, trivyFormat)
	binarySuffix := ""
	if goos == "windows" {
		binarySuffix = ".exe"
	}
	return []Release{
		{
			Name:            "tflint",
			Version:         TFLintVersion,
			AssetName:       tflintAsset,
			AssetURL:        "https://github.com/terraform-linters/tflint/releases/download/v" + TFLintVersion + "/" + tflintAsset,
			ChecksumsURL:    "https://github.com/terraform-linters/tflint/releases/download/v" + TFLintVersion + "/checksums.txt",
			ChecksumsSHA256: "d8a6007829ce00a3bf624e048cfbd73e57ce6e75dcff82c8e6fb60e84fe53275",
			Format:          "zip",
			BinaryName:      "tflint" + binarySuffix,
		},
		{
			Name:            "trivy",
			Version:         TrivyVersion,
			AssetName:       trivyAsset,
			AssetURL:        "https://github.com/aquasecurity/trivy/releases/download/v" + TrivyVersion + "/" + trivyAsset,
			ChecksumsURL:    "https://github.com/aquasecurity/trivy/releases/download/v" + TrivyVersion + "/trivy_" + TrivyVersion + "_checksums.txt",
			ChecksumsSHA256: "db42245c0b60a38e247a1c3e58c8c3389c38a694cbca6453989740bc1fff984b",
			Format:          trivyFormat,
			BinaryName:      "trivy" + binarySuffix,
		},
	}, nil
}

func (i Installer) Install(ctx context.Context) error {
	if strings.TrimSpace(i.Dir) == "" {
		return errors.New("toolchain install directory is required")
	}
	client := i.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	if err := os.MkdirAll(i.Dir, 0o755); err != nil {
		return fmt.Errorf("create toolchain directory: %w", err)
	}
	for _, release := range i.Releases {
		if err := installRelease(ctx, client, i.Dir, release); err != nil {
			return fmt.Errorf("install %s %s: %w", release.Name, release.Version, err)
		}
	}
	return nil
}

func installRelease(ctx context.Context, client *http.Client, dir string, release Release) error {
	checksums, err := download(ctx, client, release.ChecksumsURL, maxChecksums)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	if release.ChecksumsSHA256 == "" {
		return errors.New("pinned checksum manifest digest is required")
	}
	manifestSum := sha256.Sum256(checksums)
	if got := hex.EncodeToString(manifestSum[:]); got != strings.ToLower(release.ChecksumsSHA256) {
		return fmt.Errorf("checksum manifest mismatch: got %s want %s", got, release.ChecksumsSHA256)
	}
	want, err := checksumForAsset(checksums, release.AssetName)
	if err != nil {
		return err
	}
	archive, err := download(ctx, client, release.AssetURL, maxArchive)
	if err != nil {
		return fmt.Errorf("download archive: %w", err)
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("archive checksum mismatch: got %s want %s", got, want)
	}
	binary, err := extractBinary(archive, release.Format, release.BinaryName)
	if err != nil {
		return err
	}
	target := filepath.Join(dir, release.BinaryName)
	temporary, err := os.CreateTemp(dir, "."+release.Name+"-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.Write(binary); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o755); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(target)
	}
	if err := os.Rename(temporaryName, target); err != nil {
		return err
	}
	return nil
}

func download(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}
	limited := io.LimitReader(resp.Body, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download exceeds %d bytes", limit)
	}
	return data, nil
}

func checksumForAsset(manifest []byte, asset string) (string, error) {
	for _, line := range strings.Split(string(manifest), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.TrimPrefix(fields[len(fields)-1], "*") == asset {
			checksum := strings.ToLower(fields[0])
			if len(checksum) != sha256.Size*2 {
				return "", fmt.Errorf("invalid checksum for %s", asset)
			}
			if _, err := hex.DecodeString(checksum); err != nil {
				return "", fmt.Errorf("invalid checksum for %s: %w", asset, err)
			}
			return checksum, nil
		}
	}
	return "", fmt.Errorf("checksum for %s is missing", asset)
}

func extractBinary(archive []byte, format, binaryName string) ([]byte, error) {
	switch format {
	case "zip":
		reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, file := range reader.File {
			if filepath.Base(file.Name) != binaryName || file.FileInfo().IsDir() {
				continue
			}
			opened, err := file.Open()
			if err != nil {
				return nil, err
			}
			data, readErr := io.ReadAll(io.LimitReader(opened, maxArchive+1))
			closeErr := opened.Close()
			if readErr != nil {
				return nil, readErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
			if len(data) > maxArchive {
				return nil, fmt.Errorf("extracted binary exceeds %d bytes", maxArchive)
			}
			return data, nil
		}
	case "tar.gz":
		compressed, err := gzip.NewReader(bytes.NewReader(archive))
		if err != nil {
			return nil, err
		}
		defer compressed.Close()
		reader := tar.NewReader(compressed)
		for {
			header, err := reader.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, err
			}
			if header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != binaryName {
				continue
			}
			data, err := io.ReadAll(io.LimitReader(reader, maxArchive+1))
			if err != nil {
				return nil, err
			}
			if len(data) > maxArchive {
				return nil, fmt.Errorf("extracted binary exceeds %d bytes", maxArchive)
			}
			return data, nil
		}
	default:
		return nil, fmt.Errorf("unsupported archive format %q", format)
	}
	return nil, fmt.Errorf("binary %s is missing from archive", binaryName)
}
