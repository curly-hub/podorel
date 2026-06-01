package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/curly-hub/podorel/server/internal/agents"
	"github.com/curly-hub/podorel/server/internal/api"
	"github.com/curly-hub/podorel/server/internal/db"
	"github.com/curly-hub/podorel/server/internal/security"
)

func (a *App) runSecurityScan(ctx context.Context, agentID string) (db.SecurityScan, error) {
	started := a.now()
	scanner := a.configuredScannerName()
	scannerStatus, scannerClient, err := a.securityScannerForScan(ctx, agentID, scanner)
	if err != nil {
		if a.allowSnapshotFallback {
			return a.securityScanFromFixture(ctx, agentID, scanner, started)
		}
		images, imageErr := a.containerImages(ctx, agentID)
		if imageErr == nil {
			a.recordImageDigestChecks(ctx, agentID, images)
			a.recordHostPackageUpdates(ctx, agentID)
		}
		summary := emptySecuritySummary("scanner_unavailable")
		summary["scanner_available"] = false
		summary["image_count"] = len(images)
		summary["digest_checks"] = imageErr == nil
		summary["host_package_checks"] = imageErr == nil
		if imageErr != nil {
			summary["image_error"] = imageErr.Error()
		}
		return a.store.CreateSecurityScan(ctx, db.SecurityScan{
			AgentID:        agentID,
			Status:         "unavailable",
			Scanner:        scanner,
			ScannerVersion: "unavailable",
			StartedAt:      started,
			FinishedAt:     a.now(),
			Summary:        summary,
			ErrorCode:      "SCANNER_UNAVAILABLE",
			ErrorMessage:   firstNonEmpty(scannerStatus.Error, err.Error(), scannerUnavailableMessage(scanner)),
		})
	}
	images, err := a.containerImages(ctx, agentID)
	if err != nil {
		return db.SecurityScan{}, err
	}
	version := firstNonEmpty(scannerStatus.Version, "unknown")
	summary := emptySecuritySummary("trivy")
	findings := []db.SecurityFinding{}
	var scanErr error
	for _, image := range images {
		raw, err := a.runScannerForImage(ctx, scannerClient, scanner, scannerStatus.Path, image)
		if err != nil {
			scanErr = errors.Join(scanErr, fmt.Errorf("%s: %w", image, err))
			continue
		}
		parsed, err := security.ParseTrivySeveritySummary(raw)
		if err != nil {
			scanErr = errors.Join(scanErr, fmt.Errorf("%s: %w", image, err))
			continue
		}
		addSeveritySummary(summary, parsed)
		parsedFindings, err := security.ParseTrivyFindings(raw)
		if err != nil {
			scanErr = errors.Join(scanErr, fmt.Errorf("%s: %w", image, err))
			continue
		}
		for _, finding := range parsedFindings {
			findings = append(findings, db.SecurityFinding{
				Target:           firstNonEmpty(finding.Target, image),
				VulnerabilityID:  finding.VulnerabilityID,
				Severity:         finding.Severity,
				Title:            finding.Title,
				PackageName:      finding.PackageName,
				InstalledVersion: finding.InstalledVersion,
				FixedVersion:     finding.FixedVersion,
				RawJSON:          finding.RawJSON,
			})
		}
	}
	summary["image_count"] = len(images)
	status := "complete"
	errorCode := ""
	errorMessage := ""
	if scanErr != nil {
		status = "failed"
		errorCode = "SCANNER_FAILED"
		errorMessage = scanErr.Error()
	}
	scan, err := a.store.CreateSecurityScan(ctx, db.SecurityScan{
		AgentID:        agentID,
		Status:         status,
		Scanner:        scanner,
		ScannerVersion: version,
		StartedAt:      started,
		FinishedAt:     a.now(),
		Summary:        summary,
		ErrorCode:      errorCode,
		ErrorMessage:   errorMessage,
	})
	if err != nil {
		return db.SecurityScan{}, err
	}
	for i := range findings {
		findings[i].ScanID = scan.ID
	}
	if err := a.store.InsertSecurityFindings(ctx, findings); err != nil {
		return db.SecurityScan{}, err
	}
	a.recordImageDigestChecks(ctx, agentID, images)
	a.recordHostPackageUpdates(ctx, agentID)
	return scan, nil
}

func (a *App) securityScanFromFixture(ctx context.Context, agentID string, scanner string, started time.Time) (db.SecurityScan, error) {
	raw, err := os.ReadFile(fixturePath("trivy", "basic.json"))
	if err != nil {
		return a.store.CreateSecurityScan(ctx, db.SecurityScan{
			AgentID:        agentID,
			Status:         "failed",
			Scanner:        scanner,
			ScannerVersion: "development-fixture-missing",
			StartedAt:      started,
			FinishedAt:     a.now(),
			Summary:        emptySecuritySummary("development_fixture_missing"),
			ErrorCode:      "SCANNER_UNAVAILABLE",
			ErrorMessage:   err.Error(),
		})
	}
	parsed, err := security.ParseTrivySeveritySummary(raw)
	if err != nil {
		return db.SecurityScan{}, err
	}
	summary := emptySecuritySummary("development_fixture")
	addSeveritySummary(summary, parsed)
	scan, err := a.store.CreateSecurityScan(ctx, db.SecurityScan{
		AgentID:        agentID,
		Status:         "complete",
		Scanner:        scanner,
		ScannerVersion: "development-fixture",
		StartedAt:      started,
		FinishedAt:     a.now(),
		Summary:        summary,
	})
	if err != nil {
		return db.SecurityScan{}, err
	}
	parsedFindings, err := security.ParseTrivyFindings(raw)
	if err != nil {
		return db.SecurityScan{}, err
	}
	findings := make([]db.SecurityFinding, 0, len(parsedFindings))
	for _, finding := range parsedFindings {
		findings = append(findings, db.SecurityFinding{ScanID: scan.ID, Target: finding.Target, VulnerabilityID: finding.VulnerabilityID, Severity: finding.Severity, Title: finding.Title, PackageName: finding.PackageName, InstalledVersion: finding.InstalledVersion, FixedVersion: finding.FixedVersion, RawJSON: finding.RawJSON})
	}
	return scan, a.store.InsertSecurityFindings(ctx, findings)
}

func (a *App) securityScannerForScan(ctx context.Context, agentID string, scanner string) (agents.ScannerStatus, AgentClient, error) {
	if _, client, ok, err := a.agentClient(ctx, agentID); err == nil && ok {
		status, err := client.ScannerStatus(ctx, scanner)
		if err != nil {
			return agents.ScannerStatus{Scanner: scanner, Available: false, Error: err.Error()}, client, err
		}
		if !status.Available {
			message := firstNonEmpty(status.Error, scannerUnavailableMessage(scanner))
			return status, client, errors.New(message)
		}
		return status, client, nil
	}
	path, err := lookupSecurityScanner(scanner)
	if err != nil {
		return agents.ScannerStatus{Scanner: scanner, Available: false, Error: scannerUnavailableMessage(scanner)}, nil, err
	}
	version := commandFirstLine(ctx, path, "--version")
	if version == "" {
		version = "unknown"
	}
	return agents.ScannerStatus{Scanner: scanner, Available: true, Path: path, Version: version}, nil, nil
}

func (a *App) runScannerForImage(ctx context.Context, client AgentClient, scanner string, localScannerPath string, image string) ([]byte, error) {
	if client != nil {
		result, err := client.ScanImage(ctx, agents.ScanImageRequest{Scanner: scanner, Image: image})
		if err != nil {
			return nil, err
		}
		return []byte(result.RawJSON), nil
	}
	return commandOutput(ctx, localScannerPath, "image", "--format", "json", "--quiet", image)
}

func (a *App) containerImages(ctx context.Context, agentID string) ([]string, error) {
	containers, err := a.store.ListContainers(ctx, "", agentID)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	for _, container := range containers {
		image := strings.TrimSpace(container.Image)
		if image == "" {
			continue
		}
		seen[image] = struct{}{}
	}
	images := make([]string, 0, len(seen))
	for image := range seen {
		images = append(images, image)
	}
	sort.Strings(images)
	return images, nil
}

func (a *App) recordImageDigestChecks(ctx context.Context, agentID string, images []string) {
	if err := a.store.DeleteImageDigests(ctx, agentID); err != nil {
		a.logger.Error(ctx, "image_digest_cleanup", "could not clear previous image digest checks", map[string]any{"agent_id": agentID, "error": err.Error()})
	}
	if _, client, ok, err := a.agentClient(ctx, agentID); err == nil && ok {
		a.recordAgentImageDigestChecks(ctx, client, agentID, images)
		return
	}
	podmanPath, podmanErr := exec.LookPath("podman")
	skopeoPath, skopeoErr := exec.LookPath("skopeo")
	for _, image := range images {
		digest := db.ImageDigest{AgentID: agentID, ImageName: image, CheckedAt: a.now()}
		if podmanErr != nil {
			digest.ErrorMessage = "podman CLI unavailable for local digest check"
			_ = a.store.InsertImageDigest(ctx, digest)
			continue
		}
		digest.LocalDigest = strings.TrimSpace(string(mustCommandOutput(ctx, podmanPath, "image", "inspect", "--format", "{{.Digest}}", image)))
		if digest.LocalDigest == "" {
			digest.ErrorMessage = "local image digest unavailable"
			_ = a.store.InsertImageDigest(ctx, digest)
			continue
		}
		if skopeoErr == nil {
			digest.RemoteDigest = strings.TrimSpace(string(mustCommandOutput(ctx, skopeoPath, "inspect", "--format", "{{.Digest}}", "docker://"+image)))
		} else {
			digest.ErrorMessage = "skopeo unavailable for remote digest check"
		}
		status := security.CompareImageDigest(image, digest.LocalDigest, digest.RemoteDigest)
		digest.UpdateAvailable = status.UpdateAvailable
		_ = a.store.InsertImageDigest(ctx, digest)
	}
}

func (a *App) recordAgentImageDigestChecks(ctx context.Context, client AgentClient, agentID string, images []string) {
	for _, image := range images {
		digest := db.ImageDigest{AgentID: agentID, ImageName: image, CheckedAt: a.now()}
		result, err := client.ImageDigest(ctx, agents.ImageDigestRequest{Image: image})
		if err != nil {
			digest.ErrorMessage = err.Error()
			_ = a.store.InsertImageDigest(ctx, digest)
			continue
		}
		digest.LocalDigest = result.LocalDigest
		digest.RemoteDigest = result.RemoteDigest
		digest.ErrorMessage = result.Error
		status := security.CompareImageDigest(image, digest.LocalDigest, digest.RemoteDigest)
		digest.UpdateAvailable = status.UpdateAvailable
		_ = a.store.InsertImageDigest(ctx, digest)
	}
}

func (a *App) recordHostPackageUpdates(ctx context.Context, agentID string) {
	tracked := security.TrackedPodmanPackages()
	if apt, err := exec.LookPath("apt"); err == nil {
		raw, err := commandOutput(ctx, apt, "list", "--upgradable")
		if err == nil {
			for _, update := range security.ParseAptUpdates(string(raw), tracked) {
				_ = a.store.InsertHostPackageUpdate(ctx, db.HostPackageUpdate{AgentID: agentID, PackageName: update.Name, InstalledVersion: update.InstalledVersion, AvailableVersion: update.AvailableVersion, UpdateAvailable: update.UpdateAvailable, CheckedAt: a.now(), RawJSON: mustJSON(update)})
			}
		}
		return
	}
	if dnf, err := exec.LookPath("dnf"); err == nil {
		raw, _ := commandOutput(ctx, dnf, "check-update")
		for _, update := range security.ParseDnfUpdates(string(raw), tracked) {
			_ = a.store.InsertHostPackageUpdate(ctx, db.HostPackageUpdate{AgentID: agentID, PackageName: update.Name, InstalledVersion: update.InstalledVersion, AvailableVersion: update.AvailableVersion, UpdateAvailable: update.UpdateAvailable, CheckedAt: a.now(), RawJSON: mustJSON(update)})
		}
	}
}

type scannerOptionsResponse struct {
	Scanner          string                 `json:"scanner"`
	ScannerAvailable bool                   `json:"scanner_available"`
	ScannerPath      string                 `json:"scanner_path"`
	ScannerVersion   string                 `json:"scanner_version"`
	ScannerError     string                 `json:"scanner_error"`
	Options          []scannerInstallOption `json:"options"`
}

type scannerInstallOption struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	Command      string `json:"command"`
	Available    bool   `json:"available"`
	RequiresSudo bool   `json:"requires_sudo"`
	Official     bool   `json:"official"`
	DocsURL      string `json:"docs_url"`
}

func (a *App) handleScannerOptions(w http.ResponseWriter, r *http.Request, _ db.Session) {
	scanner := a.configuredScannerName()
	status := a.securityScannerStatus(r.Context(), db.PrimaryAgentID, scanner)
	response := scannerOptionsResponse{
		Scanner:          scanner,
		ScannerAvailable: status.Available,
		ScannerPath:      status.Path,
		ScannerVersion:   status.Version,
		Options:          scannerInstallOptions(scanner),
	}
	if !status.Available {
		response.ScannerError = firstNonEmpty(status.Error, scannerUnavailableMessage(scanner))
	}
	api.WriteOK(r.Context(), w, response)
}

func (a *App) securityScannerStatus(ctx context.Context, agentID string, scanner string) agents.ScannerStatus {
	status, _, err := a.securityScannerForScan(ctx, agentID, scanner)
	if err != nil {
		status.Available = false
		status.Error = firstNonEmpty(status.Error, err.Error(), scannerUnavailableMessage(scanner))
	}
	return status
}

func (a *App) configuredScannerName() string {
	scanner := strings.TrimSpace(a.cfg.Security.Scanner)
	if scanner == "" {
		return "trivy"
	}
	return scanner
}

func scannerInstallOptions(scanner string) []scannerInstallOption {
	docsURL := "https://trivy.dev/docs/latest/getting-started/installation/"
	options := []scannerInstallOption{
		{
			ID:          "debian-ubuntu-repository",
			Title:       "Debian / Ubuntu repository",
			Description: "Add Aqua Security's official APT repository, then install Trivy with apt.",
			Command: strings.TrimSpace(`sudo apt-get install -y wget gnupg
wget -qO - https://aquasecurity.github.io/trivy-repo/deb/public.key | gpg --dearmor | sudo tee /usr/share/keyrings/trivy.gpg > /dev/null
echo "deb [signed-by=/usr/share/keyrings/trivy.gpg] https://aquasecurity.github.io/trivy-repo/deb generic main" | sudo tee /etc/apt/sources.list.d/trivy.list
sudo apt-get update
sudo apt-get install -y trivy`),
			Available:    commandExists("apt-get"),
			RequiresSudo: true,
			Official:     true,
			DocsURL:      docsURL,
		},
		{
			ID:          "fedora-rhel-repository",
			Title:       "Fedora / RHEL repository",
			Description: "Add Aqua Security's RPM repository, then install Trivy with dnf or yum.",
			Command: strings.TrimSpace(`cat <<'EOF' | sudo tee /etc/yum.repos.d/trivy.repo
[trivy]
name=Trivy repository
baseurl=https://aquasecurity.github.io/trivy-repo/rpm/releases/$basearch/
gpgcheck=1
enabled=1
gpgkey=https://aquasecurity.github.io/trivy-repo/rpm/public.key
EOF
sudo dnf -y update
sudo dnf -y install trivy`),
			Available:    commandExists("dnf") || commandExists("yum"),
			RequiresSudo: true,
			Official:     true,
			DocsURL:      docsURL,
		},
		{
			ID:           "official-install-script",
			Title:        "Official install script",
			Description:  "Download the Trivy release installer and place the binary in /usr/local/bin.",
			Command:      "curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sudo sh -s -- -b /usr/local/bin",
			Available:    commandExists("curl"),
			RequiresSudo: true,
			Official:     true,
			DocsURL:      docsURL,
		},
		{
			ID:          "custom-scanner-path",
			Title:       "Use an existing scanner path",
			Description: "If Trivy is installed somewhere custom, set the scanner value in Settings to that executable path.",
			Command:     scanner + " --version",
			Available:   true,
			Official:    false,
			DocsURL:     docsURL,
		},
	}
	return options
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
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

func scannerUnavailableMessage(scanner string) string {
	return fmt.Sprintf("%s is not installed or not on the host agent PATH. Install Trivy on the host, or set Security scanner to an executable path in Settings.", scanner)
}

func emptySecuritySummary(source string) map[string]any {
	return map[string]any{"critical": 0, "high": 0, "medium": 0, "low": 0, "unknown": 0, "source": source}
}

func addSeveritySummary(summary map[string]any, parsed security.SeveritySummary) {
	summary["critical"] = intFromAny(summary["critical"]) + parsed.Critical
	summary["high"] = intFromAny(summary["high"]) + parsed.High
	summary["medium"] = intFromAny(summary["medium"]) + parsed.Medium
	summary["low"] = intFromAny(summary["low"]) + parsed.Low
	summary["unknown"] = intFromAny(summary["unknown"]) + parsed.Unknown
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func commandFirstLine(ctx context.Context, name string, args ...string) string {
	raw, err := commandOutput(ctx, name, args...)
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(raw), "\n")
	return strings.TrimSpace(line)
}

func commandOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
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

func mustCommandOutput(ctx context.Context, name string, args ...string) []byte {
	raw, err := commandOutput(ctx, name, args...)
	if err != nil {
		return nil
	}
	return raw
}

func (a *App) handleSecurityFindings(w http.ResponseWriter, r *http.Request, _ db.Session) {
	findings, err := a.store.ListSecurityFindings(r.Context(), r.URL.Query().Get("scan_id"), 500)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	api.WriteOK(r.Context(), w, findings)
}

func (a *App) handleImageDigests(w http.ResponseWriter, r *http.Request, _ db.Session) {
	digests, err := a.store.ListImageDigests(r.Context(), 100)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	api.WriteOK(r.Context(), w, digests)
}

func (a *App) handleHostPackageUpdates(w http.ResponseWriter, r *http.Request, _ db.Session) {
	updates, err := a.store.ListHostPackageUpdates(r.Context(), 100)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	api.WriteOK(r.Context(), w, updates)
}
