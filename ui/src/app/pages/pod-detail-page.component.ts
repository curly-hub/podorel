import { DatePipe, JsonPipe } from '@angular/common';
import { Component, OnInit, signal } from '@angular/core';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatDialog, MatDialogModule } from '@angular/material/dialog';
import { MatIconModule } from '@angular/material/icon';
import { MatListModule } from '@angular/material/list';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatSnackBar, MatSnackBarModule } from '@angular/material/snack-bar';
import { MatTabsModule } from '@angular/material/tabs';
import { MatTooltipModule } from '@angular/material/tooltip';
import { firstValueFrom } from 'rxjs';
import { ApiError, ApiService } from '../core/api.service';
import { AuditEvent, Container, LifecycleAction, LogLine, Pod, ResourceSample, SecuritySummary } from '../core/models';
import { aggregateResourceSamples, cpuProgressValue, formatBytes, formatCpuPercent, formatMemoryDisplay, hasCurrentStats, memoryProgressValue } from '../core/stats';
import { ConfirmationDialogComponent, ConfirmationDialogResult } from '../shared/confirmation-dialog/confirmation-dialog.component';

@Component({
  selector: 'app-pod-detail-page',
  standalone: true,
  imports: [DatePipe, JsonPipe, RouterLink, MatButtonModule, MatCardModule, MatChipsModule, MatDialogModule, MatIconModule, MatListModule, MatProgressBarModule, MatSnackBarModule, MatTabsModule, MatTooltipModule],
  templateUrl: './pod-detail-page.component.html',
  styleUrls: ['./pod-detail-page.component.scss']
})
export class PodDetailPageComponent implements OnInit {
  readonly pod = signal<Pod | null>(null);
  readonly containers = signal<Container[]>([]);
  readonly stats = signal<ResourceSample[]>([]);
  readonly logs = signal<LogLine[]>([]);
  readonly security = signal<SecuritySummary | null>(null);
  readonly audit = signal<AuditEvent[]>([]);
  readonly error = signal('');
  readonly loading = signal(true);

  private podId = '';

  constructor(
    private readonly route: ActivatedRoute,
    private readonly api: ApiService,
    private readonly dialog: MatDialog,
    private readonly snackBar: MatSnackBar
  ) {}

  ngOnInit(): void {
    this.podId = this.route.snapshot.paramMap.get('id') ?? '';
    void this.refresh();
  }

  async refresh(): Promise<void> {
    this.loading.set(true);
    this.error.set('');
    try {
      const detail = await this.api.pod(this.podId);
      const [stats, logs, security, audit] = await Promise.all([
        this.api.currentStats(),
        this.api.logsHistory({ podId: detail.pod.id, limit: 200 }),
        this.api.securitySummary(),
        this.api.audit(100)
      ]);
      this.pod.set(detail.pod);
      this.containers.set(detail.containers);
      this.stats.set(stats.filter((sample) => sample.pod_id === detail.pod.id));
      this.logs.set(logs.lines);
      this.security.set(security);
      this.audit.set(audit.filter((event) => event.target_id === detail.pod.id || detail.containers.some((container) => container.id === event.target_id)));
    } catch (error) {
      this.error.set(this.formatError(error));
    } finally {
      this.loading.set(false);
    }
  }

  async podAction(action: LifecycleAction | 'delete'): Promise<void> {
    const pod = this.pod();
    if (!pod) {
      return;
    }
    const result = await this.confirm(`${action} pod`, pod.name, action === 'kill' || action === 'delete');
    if (!result?.confirmed) {
      return;
    }
    try {
      if (action === 'delete') {
        await this.api.deletePod(pod.id, { confirm_name: result.confirm_name });
      } else {
        await this.api.podAction(pod.id, action, this.actionPayload(action, result));
      }
      this.snackBar.open(`${pod.name} ${action} requested`, 'Dismiss', { duration: 3500 });
      await this.refresh();
    } catch (error) {
      this.snackBar.open(this.formatError(error), 'Dismiss', { duration: 7000 });
    }
  }

  async containerAction(container: Container, action: LifecycleAction | 'delete'): Promise<void> {
    const result = await this.confirm(`${action} container`, container.name, action === 'kill' || action === 'delete');
    if (!result?.confirmed) {
      return;
    }
    try {
      if (action === 'delete') {
        await this.api.deleteContainer(container.id, { confirm_name: result.confirm_name });
      } else {
        await this.api.containerAction(container.id, action, this.actionPayload(action, result));
      }
      this.snackBar.open(`${container.name} ${action} requested`, 'Dismiss', { duration: 3500 });
      await this.refresh();
    } catch (error) {
      this.snackBar.open(this.formatError(error), 'Dismiss', { duration: 7000 });
    }
  }

  totalCpu(): number {
    return this.stats().reduce((total, sample) => total + (sample.cpu_percent_host_total || 0), 0);
  }

  totalMemoryBytes(): number {
    return this.stats().reduce((total, sample) => total + (sample.memory_bytes || 0), 0);
  }

  totalCpuLabel(): string {
    return formatCpuPercent(aggregateResourceSamples(this.stats()).cpuPercentHostTotal);
  }

  totalMemoryLabel(): string {
    const aggregate = aggregateResourceSamples(this.stats());
    return aggregate.available ? formatMemoryDisplay(aggregate) : 'unavailable';
  }

  totalCpuProgress(): number {
    return cpuProgressValue(aggregateResourceSamples(this.stats()));
  }

  totalMemoryProgress(): number {
    return memoryProgressValue(aggregateResourceSamples(this.stats()));
  }

  shortId(id = ''): string {
    return id.length > 12 ? id.slice(0, 12) : id;
  }

  stateClass(state = ''): string {
    return `state-${this.normalizedState(state)}`;
  }

  containerStateSummary(): string {
    const running = this.containers().filter((container) => this.normalizedState(container.state) === 'running').length;
    const exited = this.containers().filter((container) => this.isExitedState(container.state)).length;
    if (exited > 0) {
      return `${running} running / ${exited} exited`;
    }
    return `${running} running`;
  }

  podIssue(): string {
    const pod = this.pod();
    if (!pod || this.normalizedState(pod.state) !== 'degraded') {
      return '';
    }
    const exited = this.containers().filter((container) => this.isExitedState(container.state)).map((container) => container.name);
    return exited.length ? `Exited: ${exited.join(', ')}` : 'Podman reports this pod as degraded.';
  }

  containerStats(container: Container): ResourceSample | null {
    return this.stats().find((sample) => sample.container_id === container.id || sample.container_id === container.podman_container_id) ?? null;
  }

  containerCpuLabel(container: Container): string {
    const sample = this.containerStats(container);
    return sample && hasCurrentStats(sample) ? formatCpuPercent(sample.cpu_percent_host_total) : 'unavailable';
  }

  containerMemoryLabel(container: Container): string {
    const sample = this.containerStats(container);
    return sample && hasCurrentStats(sample) ? sample.memory_podman_raw || formatBytes(sample.memory_bytes) : 'unavailable';
  }

  sampleCpuProgress(sample: ResourceSample): number {
    return cpuProgressValue(aggregateResourceSamples([sample]));
  }

  sampleMemoryProgress(sample: ResourceSample): number {
    return memoryProgressValue(aggregateResourceSamples([sample]));
  }

  canPodAction(action: LifecycleAction | 'delete'): boolean {
    const pod = this.pod();
    if (!pod || this.isSeededSelfPlaceholder()) {
      return false;
    }
    const state = this.normalizedState(pod.state);
    switch (action) {
      case 'start':
        return ['stopped', 'exited', 'killed', 'created', 'degraded'].includes(state);
      case 'stop':
      case 'restart':
      case 'kill':
        return ['running', 'degraded'].includes(state);
      case 'delete':
        return true;
    }
  }

  canOpenShell(container: Container): boolean {
    return this.normalizedState(container.state) === 'running';
  }

  primaryShellContainerId(): string {
    const running = this.containers().find((container) => this.canOpenShell(container) && !container.name.toLowerCase().includes('infra'));
    const fallback = this.containers().find((container) => this.canOpenShell(container));
    return (running ?? fallback)?.id ?? '';
  }

  canContainerAction(container: Container, action: LifecycleAction | 'delete'): boolean {
    const state = this.normalizedState(container.state);
    switch (action) {
      case 'start':
        return ['stopped', 'exited', 'killed', 'created'].includes(state);
      case 'stop':
      case 'restart':
      case 'kill':
        return state === 'running';
      case 'delete':
        return true;
    }
  }

  formatMemory(bytes = 0): string {
    return formatBytes(bytes);
  }

  formatCpu(value = 0): string {
    return formatCpuPercent(value);
  }

  private normalizedState(state = ''): string {
    return (state || 'unknown').toLowerCase();
  }

  private isExitedState(state = ''): boolean {
    return ['exited', 'stopped', 'killed'].includes(this.normalizedState(state));
  }

  private isSeededSelfPlaceholder(): boolean {
    const pod = this.pod();
    return !!pod && pod.id === 'podorel-self-pod' && pod.name === 'podorel-web' && this.normalizedState(pod.state) === 'unknown';
  }

  private async confirm(title: string, name: string, destructive: boolean): Promise<ConfirmationDialogResult | undefined> {
    const label = title.charAt(0).toUpperCase() + title.slice(1);
    const dialogRef = this.dialog.open(ConfirmationDialogComponent, {
      data: {
        title: label,
        message: destructive ? `Type ${name} to continue.` : `Confirm ${title} for ${name}.`,
        confirmLabel: label,
        icon: destructive ? 'warning' : 'task_alt',
        warn: destructive,
        expectedName: destructive ? name : undefined
      }
    });
    return firstValueFrom(dialogRef.afterClosed());
  }

  private actionPayload(action: LifecycleAction, result: ConfirmationDialogResult): Record<string, unknown> {
    return action === 'kill' ? { confirm_name: result.confirm_name } : { confirm: true };
  }

  private formatError(error: unknown): string {
    if (error instanceof ApiError) {
      return `${error.message} Correlation ID: ${error.correlationId}`;
    }
    return 'Pod detail request failed.';
  }
}
