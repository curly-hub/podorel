import { DatePipe } from '@angular/common';
import { Component, OnInit, signal } from '@angular/core';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatIconModule } from '@angular/material/icon';
import { MatListModule } from '@angular/material/list';
import { ApiError, ApiService } from '../core/api.service';
import { Agent, AppSettings, AuditEvent, PodView, ResourceSample, SecuritySummary, SystemStatus } from '../core/models';
import { aggregateResourceSamples, formatBytes, formatCpuPercent } from '../core/stats';
import { HelpTooltipComponent } from '../shared/help-tooltip/help-tooltip.component';

@Component({
  selector: 'app-dashboard-page',
  standalone: true,
  imports: [DatePipe, HelpTooltipComponent, MatButtonModule, MatCardModule, MatChipsModule, MatIconModule, MatListModule],
  templateUrl: './dashboard-page.component.html',
  styleUrls: ['./dashboard-page.component.scss']
})
export class DashboardPageComponent implements OnInit {
  readonly helpTopics = {
    page: 'Dashboard summarizes live pod, agent, security, and audit state from the backend APIs.',
    pods: 'Pods counted from the agent snapshot. Actions still require the selected agent to be online.',
    cpu: 'CPU is summed from current Podman stats samples and expressed as a percent of total host CPU capacity.',
    memory: 'Memory is summed from current Podman stats samples for visible containers.',
    security: 'Security status comes from the scanner, digest, and host package checks.',
    agents: 'Agents are local processes that connect PoDorel to rootless Podman for a Linux user.',
    recentActions: 'Recent audited API actions with correlation IDs for troubleshooting.',
    runtime: 'Runtime profile shows mode and retention settings that affect fallbacks and logs.',
    supervisor: 'The development supervisor owns the local agent, web API, and UI processes. Foreground mode stops when the terminal or user session exits.'
  };

  readonly pods = signal<PodView[]>([]);
  readonly stats = signal<ResourceSample[]>([]);
  readonly security = signal<SecuritySummary | null>(null);
  readonly agents = signal<Agent[]>([]);
  readonly audit = signal<AuditEvent[]>([]);
  readonly settings = signal<AppSettings | null>(null);
  readonly systemStatus = signal<SystemStatus | null>(null);
  readonly error = signal('');
  readonly loading = signal(true);

  constructor(private readonly api: ApiService) {}

  ngOnInit(): void {
    void this.refresh();
  }

  async refresh(): Promise<void> {
    this.loading.set(true);
    this.error.set('');
    try {
      const [pods, stats, security, agents, audit, settings, systemStatus] = await Promise.all([
        this.api.pods(),
        this.api.currentStats(),
        this.api.securitySummary(),
        this.api.agents(),
        this.api.audit(8),
        this.api.settings(),
        this.api.systemStatus().catch(() => null)
      ]);
      this.pods.set(pods);
      this.stats.set(stats);
      this.security.set(security);
      this.agents.set(agents);
      this.audit.set(audit);
      this.settings.set(settings);
      this.systemStatus.set(systemStatus);
    } catch (error) {
      this.error.set(this.formatError(error));
    } finally {
      this.loading.set(false);
    }
  }

  podCountByState(state: string): number {
    return this.pods().filter((pod) => pod.state.toLowerCase() === state).length;
  }

  unhealthyPods(): number {
    return this.pods().filter((pod) => pod.health && pod.health !== 'healthy' && pod.health !== 'unknown').length;
  }

  recentErrors(): AuditEvent[] {
    return this.audit().filter((event) => event.result !== 'success');
  }

  totalCpu(): number {
    return this.stats().reduce((total, stat) => total + (stat.cpu_percent_host_total || 0), 0);
  }

  totalMemoryBytes(): number {
    return this.stats().reduce((total, stat) => total + (stat.memory_bytes || 0), 0);
  }

  totalCpuLabel(): string {
    return formatCpuPercent(aggregateResourceSamples(this.stats()).cpuPercentHostTotal);
  }

  logLimitLabel(): string {
    const logs = this.settings()?.logs;
    if (!logs) {
      return 'Unknown';
    }
    return `${logs.total_limit_mb} MB total / ${logs.per_pod_limit_mb} MB per pod`;
  }

  selfStatusLabel(): string {
    return this.settings()?.mode === 'development' ? 'Dev web/API on localhost' : 'Managed through primary agent';
  }

  formatMemory(bytes: number): string {
    return formatBytes(bytes);
  }

  devSupervisor(): Record<string, unknown> | null {
    return this.systemStatus()?.dev_supervisor ?? null;
  }

  devSupervisorStatus(): string {
    return this.valueText(this.devSupervisor()?.['status']);
  }

  devSupervisorMode(): string {
    return this.valueText(this.devSupervisor()?.['supervisor_mode']);
  }

  devSupervisorMessage(): string {
    return this.valueText(this.devSupervisor()?.['message']);
  }

  devSupervisorWarning(): string {
    const supervisor = this.devSupervisor();
    if (!supervisor) {
      return '';
    }
    const mode = this.devSupervisorMode().toLowerCase();
    const status = this.devSupervisorStatus().toLowerCase();
    if (mode === 'foreground') {
      return 'PoDorel is running in foreground dev mode. If that terminal or user session exits, the UI, API, and agent will stop.';
    }
    if (status !== 'unknown' && status !== 'running' && status !== 'ok') {
      return this.devSupervisorMessage();
    }
    return '';
  }

  private valueText(value: unknown): string {
    if (typeof value === 'boolean') {
      return value ? 'ok' : 'unavailable';
    }
    if (typeof value === 'string' || typeof value === 'number') {
      const text = String(value).trim();
      return text || 'unknown';
    }
    return 'unknown';
  }

  private formatError(error: unknown): string {
    if (error instanceof ApiError) {
      return `${error.message} Correlation ID: ${error.correlationId}`;
    }
    return 'Dashboard data could not be loaded.';
  }
}
