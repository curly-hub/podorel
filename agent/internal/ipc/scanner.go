package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const scannerCommandTimeout = 5 * time.Minute

type scannerStatus struct {
	Scanner   string `json:"scanner"`
	Available bool   `json:"available"`
	Path      string `json:"path"`
	Version   string `json:"version"`
	Error     string `json:"error"`
}

type scanImageRequest struct {
	Scanner string `json:"scanner"`
	Image   string `json:"image"`
}

type scanImageResult struct {
	Scanner        string `json:"scanner"`
	ScannerPath    string `json:"scanner_path"`
	ScannerVersion string `json:"scanner_version"`
	Image          string `json:"image"`
	RawJSON        string `json:"raw_json"`
}

type imageDigestRequest struct {
	Image string `json:"image"`
}

type imageDigestResult struct {
	Image        string `json:"image"`
	LocalDigest  string `json:"local_digest"`
	RemoteDigest string `json:"remote_digest"`
	Error        string `json:"error"`
}

func (s Server) handleScannerStatus(w http.ResponseWriter, r *http.Request) {
	scanner := strings.TrimSpace(r.URL.Query().Get("scanner"))
	writeResult(w, resolveScannerStatus(r.Context(), scanner), nil)
}

func (s Server) handleScanImage(w http.ResponseWriter, r *http.Request) {
	var req scanImageRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	image := strings.TrimSpace(req.Image)
	if image == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "image is required"})
		return
	}
	status := resolveScannerStatus(r.Context(), req.Scanner)
	if !status.Available {
		writeResult(w, nil, errors.New(status.Error))
		return
	}
	raw, err := scanImageWithScanner(r.Context(), status, image)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	writeResult(w, scanImageResult{
		Scanner:        status.Scanner,
		ScannerPath:    status.Path,
		ScannerVersion: status.Version,
		Image:          image,
		RawJSON:        string(raw),
	}, nil)
}

func (s Server) handleImageDigest(w http.ResponseWriter, r *http.Request) {
	var req imageDigestRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	image := strings.TrimSpace(req.Image)
	if image == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "image is required"})
		return
	}
	result := imageDigestResult{Image: image}
	podmanPath, err := lookupHostCommand("podman")
	if err != nil {
		result.Error = "podman CLI unavailable on host agent for local digest check"
		writeResult(w, result, nil)
		return
	}
	result.LocalDigest = strings.TrimSpace(string(mustScannerCommandOutput(r.Context(), podmanPath, "image", "inspect", "--format", "{{.Digest}}", image)))
	if result.LocalDigest == "" {
		result.Error = "local image digest unavailable"
		writeResult(w, result, nil)
		return
	}
	if skopeoPath, err := lookupHostCommand("skopeo"); err == nil {
		result.RemoteDigest = strings.TrimSpace(string(mustScannerCommandOutput(r.Context(), skopeoPath, "inspect", "--format", "{{.Digest}}", "docker://"+image)))
	} else {
		result.Error = "skopeo unavailable on host agent for remote digest check"
	}
	writeResult(w, result, nil)
}

func scanImageWithScanner(ctx context.Context, status scannerStatus, image string) ([]byte, error) {
	if status.Scanner == "trivy" {
		raw, archiveErr := scanTrivyPodmanArchive(ctx, status.Path, image)
		if archiveErr == nil {
			return raw, nil
		}
		raw, directErr := scannerCommandOutput(ctx, status.Path, "image", "--format", "json", "--quiet", image)
		if directErr != nil {
			return raw, errors.Join(directErr, fmt.Errorf("podman image archive scan failed: %w", archiveErr))
		}
		return raw, nil
	}
	return scannerCommandOutput(ctx, status.Path, "image", "--format", "json", "--quiet", image)
}

func scanTrivyPodmanArchive(ctx context.Context, trivyPath string, image string) ([]byte, error) {
	podmanPath, err := lookupHostCommand("podman")
	if err != nil {
		return nil, err
	}
	tempDir, err := os.MkdirTemp("", "podorel-trivy-image-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)
	archivePath := filepath.Join(tempDir, "image.tar")
	if _, err := scannerCommandOutput(ctx, podmanPath, "image", "save", "--format", "docker-archive", "-o", archivePath, image); err != nil {
		return nil, err
	}
	return scannerCommandOutput(ctx, trivyPath, "image", "--format", "json", "--quiet", "--input", archivePath)
}

func resolveScannerStatus(ctx context.Context, scanner string) scannerStatus {
	scanner = strings.TrimSpace(scanner)
	if scanner == "" {
		scanner = "trivy"
	}
	path, err := lookupSecurityScanner(scanner)
	if err != nil {
		return scannerStatus{Scanner: scanner, Available: false, Error: scannerUnavailableMessage(scanner)}
	}
	version := scannerCommandFirstLine(ctx, path, "--version")
	if version == "" {
		version = "unknown"
	}
	return scannerStatus{Scanner: scanner, Available: true, Path: path, Version: version}
}

func lookupSecurityScanner(scanner string) (string, error) {
	path, err := exec.LookPath(scanner)
	if err == nil {
		return path, nil
	}
	if strings.Contains(scanner, string(os.PathSeparator)) {
		return "", err
	}
	if scanner != "trivy" {
		return "", err
	}
	for _, candidate := range []string{"/usr/local/bin/trivy", "/usr/bin/trivy", "/bin/trivy"} {
		if path, candidateErr := exec.LookPath(candidate); candidateErr == nil {
			return path, nil
		}
	}
	return "", err
}

func lookupHostCommand(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err == nil {
		return path, nil
	}
	if strings.Contains(name, string(os.PathSeparator)) {
		return "", err
	}
	for _, candidate := range []string{"/usr/local/bin/" + name, "/usr/bin/" + name, "/bin/" + name} {
		if path, candidateErr := exec.LookPath(candidate); candidateErr == nil {
			return path, nil
		}
	}
	return "", err
}

func scannerUnavailableMessage(scanner string) string {
	return fmt.Sprintf("%s is not installed or not on the host agent PATH. Install Trivy on the host, or set Security scanner to an executable path in Settings.", scanner)
}

func scannerCommandFirstLine(ctx context.Context, name string, args ...string) string {
	raw, err := scannerCommandOutput(ctx, name, args...)
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(raw), "\n")
	return strings.TrimSpace(line)
}

func scannerCommandOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, scannerCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func mustScannerCommandOutput(ctx context.Context, name string, args ...string) []byte {
	raw, err := scannerCommandOutput(ctx, name, args...)
	if err != nil {
		return nil
	}
	return raw
}
