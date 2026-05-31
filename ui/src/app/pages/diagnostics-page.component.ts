import { Component, OnInit } from '@angular/core';
import { DatePipe, JsonPipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatTabsModule } from '@angular/material/tabs';
import { ApiError, ApiService } from '../core/api.service';
import { DebugTrace, RuntimeMode, SystemStatus } from '../core/models';
import { HelpTooltipComponent } from '../shared/help-tooltip/help-tooltip.component';

@Component({
  selector: 'app-diagnostics-page',
  standalone: true,
  imports: [DatePipe, FormsModule, HelpTooltipComponent, JsonPipe, MatButtonModule, MatCardModule, MatFormFieldModule, MatIconModule, MatInputModule, MatTabsModule],
  templateUrl: './diagnostics-page.component.html',
  styleUrls: ['./diagnostics-page.component.scss']
})
export class DiagnosticsPageComponent implements OnInit {
  readonly helpTopics = {
    page: 'Diagnostics shows which layer failed: browser proxy, Go web server, agent socket, agent token, Podman socket, or Podman CLI fallback.',
    mode: 'Runtime mode controls whether development/test-only fallbacks and debug traces are allowed.',
    rawTraces: 'Raw traces are detailed request/debug records. They are redacted in production so sensitive values are not exposed.',
    health: 'Overall web API health for the backend process.',
    agent: 'Primary agent health reported by the web server, including socket/token/Podman layer checks.',
    fallback: 'Whether PoDorel is using live agent data, development cached snapshots, or no fallback.',
    supervisor: 'Development-only process supervisor. If it exits, the dev agent, web API, and UI are stopped unless the stack was started detached or by a real service manager.',
    systemJson: 'Raw system status returned by /api/system/status. It is useful for support but should not be the main operating view.',
    healthJson: 'Raw backend health response. Use it to compare the summarized tiles with the exact API payload.',
    correlationId: 'A request identifier attached to failures and audit records so logs and UI errors can be matched.',
    bundle: 'A redacted support bundle with runtime and health details. Creating it requires admin password confirmation.',
    supportLogs: 'Downloads the selected PoDorel server logs as a text file. Review before sharing because logs can include hostnames, paths, image names, container names, and correlation IDs.'
  };

  health: Record<string, unknown> | null = null;
  runtime: RuntimeMode | null = null;
  systemStatus: SystemStatus | null = null;
  traces: DebugTrace[] = [];
  bundle: Record<string, unknown> | null = null;
  correlationId = '';
  adminPassword = '';
  supportLogsBusy = false;
  supportLogsStatus = '';
  error = '';
  redacted = false;
  loading = false;

  constructor(private readonly api: ApiService) {}

  ngOnInit(): void {
    void this.refresh();
  }

  async refresh(): Promise<void> {
    this.error = '';
    this.loading = true;
    const failures: string[] = [];
    try {
      const [health, runtime, systemStatus] = await Promise.allSettled([
        this.withTimeout(this.api.health(), 'Health'),
        this.withTimeout(this.api.runtimeMode(), 'Runtime mode'),
        this.withTimeout(this.api.systemStatus(), 'System status')
      ]);
      if (health.status === 'fulfilled') { this.health = health.value; } else { failures.push(this.formatError(health.reason)); }
      if (runtime.status === 'fulfilled') { this.runtime = runtime.value; } else { failures.push(this.formatError(runtime.reason)); }
      if (systemStatus.status === 'fulfilled') { this.systemStatus = systemStatus.value; } else { failures.push(this.formatError(systemStatus.reason)); }
      await this.loadTraces(false);
      if (failures.length && !this.error) {
        this.error = failures.join(' ');
      }
    } finally {
      this.loading = false;
    }
  }


  devSupervisor(): Record<string, unknown> {
    return this.systemStatus?.dev_supervisor ?? {};
  }

  devSupervisorStatus(): string {
    return this.valueText(this.devSupervisor()['status']);
  }

  devSupervisorMode(): string {
    return this.valueText(this.devSupervisor()['supervisor_mode']);
  }

  devSupervisorMessage(): string {
    return this.valueText(this.devSupervisor()['message']);
  }

  devSupervisorWarning(): string {
    if (this.runtime?.mode !== 'development') {
      return '';
    }
    const status = this.devSupervisorStatus();
    const mode = this.devSupervisorMode();
    if (status !== 'running') {
      return this.devSupervisorMessage() || 'Development supervisor is not reporting as running.';
    }
    if (mode === 'foreground') {
      return 'PoDorel is running under a foreground dev supervisor. Closing that shell/session stops the UI, API, and agent. Start with scripts/deploy-dev.sh --detach to keep it up.';
    }
    return '';
  }

  private valueText(value: unknown): string {
    if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
      const text = String(value).trim();
      return text || 'unknown';
    }
    return 'unknown';
  }

  async loadTraces(clearError = true): Promise<void> {
    if (clearError) {
      this.error = "";
    }
    try {
      const response = await this.withTimeout(this.api.traces(this.correlationId), 'Traces');
      if (Array.isArray(response)) {
        this.traces = response;
        this.redacted = false;
      } else {
        this.traces = response.traces;
        this.redacted = response.redacted;
      }
    } catch (error) {
      this.error = this.formatError(error);
    }
  }

  async downloadSupportLogs(): Promise<void> {
    this.error = '';
    this.supportLogsStatus = '';
    this.supportLogsBusy = true;
    try {
      const content = await this.api.downloadLogs({ limit: 5000 });
      this.downloadTextFile(content, `podorel-support-logs-${this.timestampForFilename()}.txt`);
      this.supportLogsStatus = 'Support logs downloaded. Review the file before sharing it.';
    } catch (error) {
      this.error = this.formatError(error);
    } finally {
      this.supportLogsBusy = false;
    }
  }

  private downloadTextFile(content: string, filename: string): void {
    const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = filename;
    anchor.click();
    URL.revokeObjectURL(url);
  }

  private timestampForFilename(): string {
    return new Date().toISOString().replace(/[:.]/g, '-');
  }

  async createBundle(): Promise<void> {
    this.error = '';
    try {
      this.bundle = await this.api.diagnosticsBundle({ password: this.adminPassword });
    } catch (error) {
      this.error = this.formatError(error);
    }
  }

  private async withTimeout<T>(promise: Promise<T>, label: string, timeoutMs = 3500): Promise<T> {
    let timeout: ReturnType<typeof setTimeout> | undefined;
    const timeoutPromise = new Promise<never>((_, reject) => {
      timeout = setTimeout(() => reject(new Error(`${label} timed out.`)), timeoutMs);
    });
    try {
      return await Promise.race([promise, timeoutPromise]);
    } finally {
      if (timeout) {
        clearTimeout(timeout);
      }
    }
  }

  private formatError(error: unknown): string {
    if (error instanceof ApiError) {
      return `${error.message} Correlation ID: ${error.correlationId}`;
    }
    if (error instanceof Error && error.message) {
      return error.message;
    }
    return 'Diagnostics request failed.';
  }
}
