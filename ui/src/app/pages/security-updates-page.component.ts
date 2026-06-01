import { DatePipe } from '@angular/common';
import { Component, OnInit, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatIconModule } from '@angular/material/icon';
import { MatSnackBar, MatSnackBarModule } from '@angular/material/snack-bar';
import { MatTableModule } from '@angular/material/table';
import { ApiError, ApiService } from '../core/api.service';
import { HostPackageUpdate, ImageDigest, ScannerInstallOption, ScannerOptions, SecurityFinding, SecurityScan, SecuritySummary } from '../core/models';
import { HelpTooltipComponent } from '../shared/help-tooltip/help-tooltip.component';

type SeverityKey = 'critical' | 'high' | 'medium' | 'low' | 'unknown';
type ScannerSafetyStatus = 'safe' | 'unsafe' | 'unknown' | 'unavailable';

interface SeveritySummary {
  key: SeverityKey;
  label: string;
  value: string;
}

interface ScannerSafety {
  status: ScannerSafetyStatus;
  label: string;
  detail: string;
}

@Component({
  selector: 'app-security-updates-page',
  standalone: true,
  imports: [DatePipe, HelpTooltipComponent, MatButtonModule, MatCardModule, MatChipsModule, MatIconModule, MatSnackBarModule, MatTableModule],
  templateUrl: './security-updates-page.component.html',
  styleUrls: ['./security-updates-page.component.scss']
})
export class SecurityUpdatesPageComponent implements OnInit {
  readonly helpTopics = {
    page: 'Security combines vulnerability scan results, image digest checks, and host package update checks.',
    scanner: 'The vulnerability scanner executable on the host agent. PoDorel currently expects Trivy unless Settings points to another compatible scanner path.',
    lastScan: 'When the latest scan finished. Empty means no successful scan has been recorded yet.',
    digest: 'Compares locally used image digests with registry data to flag images that may have newer upstream content.',
    hostPackages: 'Checks Podman-related host packages through apt or dnf when available.',
    installOptions: 'Commands are suggestions for the host terminal. PoDorel shows them but does not run sudo installs from the web UI.',
    versionSafety: 'PoDorel checks the installed Trivy version against known compromised releases from the March 2026 advisory.',
    findings: 'Stored CVE rows returned by the scanner for images known to PoDorel.',
    imageDigests: 'Per-image digest freshness checks. An update means the remote image differs from the local digest.',
    hostUpdates: 'Package updates detected for Podman-related host packages.',
    schedule: 'Whether scans are configured to run automatically or only when you press Rescan.',
    severity: 'Scanner severity counts from the latest scan summary.'
  };

  readonly summary = signal<SecuritySummary | null>(null);
  readonly scanResult = signal<SecurityScan | null>(null);
  readonly findings = signal<SecurityFinding[]>([]);
  readonly imageDigests = signal<ImageDigest[]>([]);
  readonly hostUpdates = signal<HostPackageUpdate[]>([]);
  readonly scannerOptions = signal<ScannerOptions | null>(null);
  readonly error = signal('');
  readonly scanning = signal(false);

  readonly displayedFindingColumns = ['severity', 'vulnerability', 'target', 'package', 'fixed'];
  readonly displayedDigestColumns = ['image', 'status', 'checked'];
  readonly displayedPackageColumns = ['package', 'installed', 'available', 'checked'];

  private readonly severityOrder: SeverityKey[] = ['critical', 'high', 'medium', 'low', 'unknown'];
  private readonly fallbackScannerOptions: ScannerInstallOption[] = [
    {
      id: 'debian-ubuntu-repository',
      title: 'Debian / Ubuntu repository',
      description: "Add Aqua Security's official APT repository, then install Trivy with apt.",
      command: `sudo apt-get install -y wget gnupg
wget -qO - https://aquasecurity.github.io/trivy-repo/deb/public.key | gpg --dearmor | sudo tee /usr/share/keyrings/trivy.gpg > /dev/null
echo "deb [signed-by=/usr/share/keyrings/trivy.gpg] https://aquasecurity.github.io/trivy-repo/deb generic main" | sudo tee /etc/apt/sources.list.d/trivy.list
sudo apt-get update
sudo apt-get install -y trivy`,
      available: false,
      requires_sudo: true,
      official: true,
      docs_url: 'https://trivy.dev/docs/latest/getting-started/installation/'
    },
    {
      id: 'fedora-rhel-repository',
      title: 'Fedora / RHEL repository',
      description: "Add Aqua Security's official RPM repository, then install Trivy with dnf.",
      command: `sudo tee /etc/yum.repos.d/trivy.repo > /dev/null <<'EOF'
[trivy]
name=Trivy repository
baseurl=https://aquasecurity.github.io/trivy-repo/rpm/releases/$basearch/
gpgcheck=1
enabled=1
gpgkey=https://aquasecurity.github.io/trivy-repo/rpm/public.key
EOF
sudo dnf -y update
sudo dnf -y install trivy`,
      available: false,
      requires_sudo: true,
      official: true,
      docs_url: 'https://trivy.dev/docs/latest/getting-started/installation/'
    },
    {
      id: 'official-install-script',
      title: 'Official install script',
      description: 'Download the Trivy release installer and place the binary in /usr/local/bin.',
      command: 'curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sudo sh -s -- -b /usr/local/bin',
      available: false,
      requires_sudo: true,
      official: true,
      docs_url: 'https://trivy.dev/docs/latest/getting-started/installation/'
    },
    {
      id: 'custom-scanner-path',
      title: 'Use an existing scanner path',
      description: 'If Trivy is installed somewhere custom on the host agent, set Security scanner in Settings to the executable path.',
      command: '/path/to/trivy --version',
      available: false,
      requires_sudo: false,
      official: false,
      docs_url: 'https://trivy.dev/docs/latest/getting-started/installation/'
    }
  ];

  constructor(private readonly api: ApiService, private readonly snackBar: MatSnackBar) {}

  ngOnInit(): void {
    void this.refresh();
  }

  async refresh(): Promise<void> {
    this.error.set('');
    try {
      const [summary, scannerOptions, findings, imageDigests, hostUpdates] = await Promise.all([
        this.api.securitySummary(),
        this.api.scannerOptions(),
        this.api.securityFindings(),
        this.api.imageDigests(),
        this.api.hostPackageUpdates()
      ]);
      this.summary.set(summary);
      this.scannerOptions.set(scannerOptions);
      this.scanResult.set(null);
      this.findings.set(this.arrayOrEmpty(findings));
      this.imageDigests.set(this.arrayOrEmpty(imageDigests));
      this.hostUpdates.set(this.arrayOrEmpty(hostUpdates));
    } catch (error) {
      this.error.set(this.formatError(error));
    }
  }

  async scan(): Promise<void> {
    this.scanning.set(true);
    this.error.set('');
    try {
      const scan = await this.api.scanSecurity();
      this.scanResult.set(scan);
      const findings = await this.api.securityFindings(scan.id);
      this.findings.set(this.arrayOrEmpty(findings));
      await this.refresh();
    } catch (error) {
      this.error.set(this.formatError(error));
    } finally {
      this.scanning.set(false);
    }
  }

  latestSummary(): Record<string, unknown> {
    return this.scanResult()?.summary ?? this.summary()?.latest_scan?.summary ?? {};
  }

  severitySummary(): SeveritySummary[] {
    const summary = this.latestSummary();
    const hasSummary = Object.keys(summary).length > 0;
    return this.severityOrder.map((key) => ({
      key,
      label: key,
      value: hasSummary ? this.summaryValue(summary[key]) : '-'
    }));
  }

  scannerErrors(): string {
    return this.scanResult()?.error_message ?? this.summary()?.latest_scan?.error_message ?? '';
  }

  scannerReady(): boolean {
    return this.scannerOptions()?.scanner_available === true || this.summary()?.scanner_available === true;
  }

  scannerUnsafe(): boolean {
    return this.scannerVersionSafety().status === 'unsafe';
  }

  scannerUnavailable(): boolean {
    const options = this.scannerOptions();
    if (options) {
      return !options.scanner_available;
    }
    const scan = this.latestScanRecord();
    return this.summary()?.scanner_available === false || scan?.status === 'unavailable' || scan?.error_code === 'SCANNER_UNAVAILABLE';
  }

  scannerSetupNeeded(): boolean {
    return !this.scannerReady() || this.scannerUnsafe();
  }

  scannerNotice(): string {
    return this.scannerOptions()?.scanner_error ?? this.scanResult()?.error_message ?? this.summary()?.scanner_error ?? this.summary()?.latest_scan?.error_message ?? '';
  }

  scannerStatusLabel(): string {
    if (this.scanning()) {
      return 'Scan running';
    }
    if (this.scannerUnavailable()) {
      return 'Scanner setup needed';
    }
    if (this.scannerUnsafe()) {
      return 'Unsafe scanner version';
    }
    if (this.scannerReady() && !this.scannerUnsafe()) {
      return 'Scanner ready';
    }
    return this.humanize(this.summary()?.status ?? this.latestScanRecord()?.status ?? 'unknown');
  }

  scannerDetail(): string {
    const options = this.scannerOptions();
    if (options?.scanner_available) {
      const safety = this.scannerVersionSafety();
      const version = this.scannerVersionLabel();
      if (safety.status === 'unsafe') {
        return safety.detail;
      }
      return `${options.scanner} ${version} detected at ${options.scanner_path || 'PATH'}`;
    }
    return this.scannerNotice() || `Install ${options?.scanner || this.summary()?.scanner || 'trivy'} on the host running the PoDorel agent.`;
  }

  statusIcon(): string {
    if (this.scanning()) {
      return 'sync';
    }
    if (this.scannerUnavailable()) {
      return 'build_circle';
    }
    if (this.scannerUnsafe()) {
      return 'report';
    }
    const status = this.latestScanRecord()?.status ?? this.summary()?.status ?? 'unknown';
    if (status === 'complete' || this.scannerReady()) {
      return 'verified';
    }
    if (status === 'failed') {
      return 'error';
    }
    return 'help';
  }

  scannerInstallOptions(): ScannerInstallOption[] {
    const options = this.scannerOptions()?.options ?? [];
    return options.length ? options : this.fallbackScannerOptions;
  }

  recommendedScannerOption(): ScannerInstallOption | null {
    const id = this.recommendedScannerOptionId();
    return this.scannerInstallOptions().find((option) => option.id === id) ?? this.scannerInstallOptions()[0] ?? null;
  }

  recommendedScannerOptionId(): string {
    const options = this.scannerInstallOptions();
    if (this.scannerReady() && !this.scannerUnsafe()) {
      return 'custom-scanner-path';
    }
    return options.find((option) => option.available && option.official && option.id.includes('debian'))?.id
      ?? options.find((option) => option.available && option.official && option.id.includes('fedora'))?.id
      ?? options.find((option) => option.available && option.official)?.id
      ?? options.find((option) => option.available)?.id
      ?? options[0]?.id
      ?? '';
  }

  scannerInstallStatus(option: ScannerInstallOption): string {
    if (this.scannerReady() && option.id === 'custom-scanner-path') {
      return 'Current scanner';
    }
    if (!this.scannerReady() && option.id === this.recommendedScannerOptionId()) {
      return 'Recommended here';
    }
    if (option.available) {
      return 'Available here';
    }
    return option.requires_sudo ? 'Host terminal' : 'Manual';
  }

  optionIcon(option: ScannerInstallOption): string {
    if (this.scannerReady() && option.id === 'custom-scanner-path') {
      return 'task_alt';
    }
    return option.id === this.recommendedScannerOptionId() ? 'terminal' : 'content_copy';
  }

  setupSteps(): string[] {
    if (this.scannerUnsafe()) {
      return ['Remove the unsafe Trivy version', 'Install a safe current release', 'Refresh, then rescan'];
    }
    if (this.scannerReady() && !this.scannerUnsafe()) {
      return ['Scanner detected', 'Run rescan when needed', 'Review findings below'];
    }
    return ['Copy one install command', 'Run it on the PoDorel host', 'Refresh, then rescan'];
  }

  scannerVersionLabel(): string {
    return this.parsedScannerVersion() || this.scannerOptions()?.scanner_version || this.latestScanRecord()?.scanner_version || 'unknown version';
  }

  scannerVersionSafety(): ScannerSafety {
    if (this.scannerUnavailable()) {
      return { status: 'unavailable', label: 'Scanner unavailable', detail: 'Install Trivy on the host agent before PoDorel can verify the scanner version.' };
    }
    const version = this.parsedScannerVersion();
    if (!version) {
      return { status: 'unknown', label: 'Version unknown', detail: 'PoDorel could not parse the scanner version. Verify the executable before trusting scan results.' };
    }
    if (['0.69.4', '0.69.5', '0.69.6'].includes(version)) {
      return {
        status: 'unsafe',
        label: `Unsafe ${version}`,
        detail: 'This Trivy version/tag is covered by the March 2026 supply-chain advisory. Do not run scans with it; remove it and install a known-safe current release.'
      };
    }
    return { status: 'safe', label: `Version ${version}`, detail: 'This installed Trivy version passes the scanner safety check.' };
  }

  scannerVersionClass(): string {
    return `version-pill ${this.scannerVersionSafety().status}`;
  }

  scannerVersionIcon(): string {
    const status = this.scannerVersionSafety().status;
    if (status === 'safe') {
      return 'verified';
    }
    if (status === 'unsafe') {
      return 'report';
    }
    if (status === 'unavailable') {
      return 'build_circle';
    }
    return 'help';
  }

  verificationCommand(): string {
    const path = this.scannerOptions()?.scanner_path || this.scannerOptions()?.scanner || 'trivy';
    return `${path} --version`;
  }

  findingsList(): SecurityFinding[] {
    return [...this.arrayOrEmpty(this.findings())].sort((left, right) =>
      this.severityRank(left.severity) - this.severityRank(right.severity) || right.id - left.id
    );
  }

  imageDigestList(): ImageDigest[] {
    return this.arrayOrEmpty(this.imageDigests());
  }

  hostUpdateList(): HostPackageUpdate[] {
    return this.arrayOrEmpty(this.hostUpdates());
  }

  findingCountLabel(): string {
    return this.countLabel(this.findingsList().length, 'finding');
  }

  digestIssueCountLabel(): string {
    const count = this.imageDigestList().filter((digest) => digest.update_available || !!digest.error_message).length;
    return this.countLabel(count, 'image issue');
  }

  hostUpdateCountLabel(): string {
    const count = this.hostUpdateList().filter((update) => update.update_available).length;
    return this.countLabel(count, 'package update');
  }

  findingSeverityClass(finding: SecurityFinding): string {
    return `severity-badge severity-${this.normalizedSeverity(finding.severity)}`;
  }

  severityTileClass(item: SeveritySummary): string {
    return `severity-tile severity-${item.key}`;
  }

  digestStatus(digest: ImageDigest): string {
    if (digest.error_message) {
      return digest.error_message;
    }
    return digest.update_available ? 'Update available' : 'Current';
  }

  digestStatusClass(digest: ImageDigest): string {
    if (digest.error_message) {
      return 'status-pill warning';
    }
    return digest.update_available ? 'status-pill danger' : 'status-pill success';
  }

  packageStatusClass(update: HostPackageUpdate): string {
    return update.update_available ? 'status-pill warning' : 'status-pill success';
  }

  async copyCommand(option: ScannerInstallOption): Promise<void> {
    await this.copyText(option.command);
  }

  async copyVerificationCommand(): Promise<void> {
    await this.copyText(this.verificationCommand());
  }

  latestScanRecord(): SecurityScan | null {
    return this.scanResult() ?? this.summary()?.latest_scan ?? null;
  }

  private async copyText(value: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(value);
      this.snackBar.open('Command copied', 'Dismiss', { duration: 2500 });
    } catch {
      this.snackBar.open('Copy failed; select the command manually.', 'Dismiss', { duration: 4000 });
    }
  }

  private parsedScannerVersion(): string {
    const raw = `${this.scannerOptions()?.scanner_version || this.latestScanRecord()?.scanner_version || ''}`;
    if (!raw || raw === 'unknown' || raw === 'unavailable') {
      return '';
    }
    const match = raw.match(/(?:Version:\s*)?v?(\d+\.\d+\.\d+)/i);
    return match?.[1] ?? '';
  }

  private summaryValue(value: unknown): string {
    if (typeof value === 'number') {
      return String(value);
    }
    if (typeof value === 'string' && value.trim()) {
      return value;
    }
    return '0';
  }

  private severityRank(severity: string): number {
    const rank = this.severityOrder.indexOf(this.normalizedSeverity(severity));
    return rank >= 0 ? rank : this.severityOrder.length;
  }

  private normalizedSeverity(severity: string): SeverityKey {
    const normalized = (severity || 'unknown').toLowerCase();
    return this.severityOrder.includes(normalized as SeverityKey) ? normalized as SeverityKey : 'unknown';
  }

  private countLabel(count: number, noun: string): string {
    return `${count} ${noun}${count === 1 ? '' : 's'}`;
  }

  private arrayOrEmpty<T>(value: T[] | null | undefined): T[] {
    return Array.isArray(value) ? value : [];
  }

  private humanize(value: string): string {
    return value.replace(/_/g, ' ').replace(/^./, (match) => match.toUpperCase());
  }

  private formatError(error: unknown): string {
    if (error instanceof ApiError) {
      return `${error.message} Correlation ID: ${error.correlationId}`;
    }
    return 'Security data could not be loaded.';
  }
}
