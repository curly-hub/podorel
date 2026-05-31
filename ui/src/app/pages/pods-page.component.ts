import { Component, OnInit, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatDialog, MatDialogModule } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatMenuModule } from '@angular/material/menu';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatSelectModule } from '@angular/material/select';
import { MatSnackBar, MatSnackBarModule } from '@angular/material/snack-bar';
import { MatTooltipModule } from '@angular/material/tooltip';
import { firstValueFrom } from 'rxjs';
import { ApiError, ApiService } from '../core/api.service';
import { Agent, LifecycleAction, PodView } from '../core/models';
import { aggregateResourceSamples, cpuProgressValue, formatCpuPercent, formatMemoryDisplay, memoryProgressValue, StatsAggregate } from '../core/stats';
import { ConfirmationDialogComponent, ConfirmationDialogResult } from '../shared/confirmation-dialog/confirmation-dialog.component';
import { HelpTooltipComponent } from '../shared/help-tooltip/help-tooltip.component';

@Component({
  selector: 'app-pods-page',
  standalone: true,
  imports: [FormsModule, HelpTooltipComponent, RouterLink, MatButtonModule, MatCardModule, MatChipsModule, MatDialogModule, MatFormFieldModule, MatIconModule, MatInputModule, MatMenuModule, MatProgressBarModule, MatSelectModule, MatSnackBarModule, MatTooltipModule],
  templateUrl: './pods-page.component.html',
  styleUrls: ['./pods-page.component.scss']
})
export class PodsPageComponent implements OnInit {
  readonly helpTopics = {
    page: 'Pods are groups of containers managed through rootless Podman by a selected PoDorel agent.',
    state: 'Pod state is reported by Podman through the agent snapshot.',
    health: 'Health is the best-known container or pod health reported by Podman, or unknown when no health check exists.',
    scan: 'Security scan state for this pod image. Not scanned means no scanner result has been recorded yet.',
    source: 'Snapshot source tells you whether the row is live agent data or development/test cached data.',
    cpu: 'CPU comes from live Podman stats samples and is normalized against total host CPU capacity.',
    memory: 'Memory comes from live Podman stats samples for containers in this pod.',
    statsSource: 'This shows how many container stats rows were sampled for the current pod list.',
    containers: 'Container count and state summary for containers inside this pod.',
    uptime: 'Approximate time since the pod creation timestamp reported by Podman.',
    agentOffline: 'Actions are disabled when the owning agent is offline because PoDorel cannot safely reach Podman.'
  };

  readonly pods = signal<PodView[]>([]);
  readonly agents = signal<Agent[]>([]);
  readonly error = signal('');
  readonly loading = signal(true);
  readonly stateFilters = ['all', 'running', 'degraded', 'stopped', 'unknown'];
  search = '';
  stateFilter = 'all';

  constructor(private readonly api: ApiService, private readonly dialog: MatDialog, private readonly snackBar: MatSnackBar) {}

  ngOnInit(): void {
    void this.refresh();
  }

  async refresh(): Promise<void> {
    this.loading.set(true);
    this.error.set('');
    try {
      const pods = await this.api.pods();
      const agents = await this.api.agents();
      this.pods.set(pods);
      this.agents.set(agents);
    } catch (error) {
      this.error.set(this.formatError(error));
    } finally {
      this.loading.set(false);
    }
  }

  async action(pod: PodView, action: LifecycleAction | 'delete'): Promise<void> {
    if (this.isDevelopmentSelfPlaceholder(pod)) {
      this.snackBar.open('The dev web/API runs directly on localhost, outside Podman.', 'Dismiss', { duration: 6000 });
      return;
    }
    if (!this.agentOnline(pod)) {
      this.snackBar.open(`Agent ${pod.agent_id} is offline; action buttons are disabled until health is restored.`, 'Dismiss', { duration: 6000 });
      return;
    }
    const result = await this.confirm(pod, action);
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

  shortId(id: string): string {
    return id.length > 12 ? id.slice(0, 12) : id;
  }

  filteredPods(): PodView[] {
    const query = this.search.trim().toLowerCase();
    return this.pods().filter((pod) => {
      const matchesState = this.stateFilter === 'all' || this.normalizedState(pod) === this.stateFilter;
      const matchesQuery = !query || `${pod.name} ${pod.id} ${pod.agent_id} ${pod.state}`.toLowerCase().includes(query);
      return matchesState && matchesQuery;
    });
  }

  stateCount(state: string): number {
    if (state === 'all') {
      return this.pods().length;
    }
    return this.pods().filter((pod) => this.normalizedState(pod) === state).length;
  }

  podStats(pod: PodView): StatsAggregate {
    return aggregateResourceSamples(pod.stats);
  }

  statsAvailable(pod: PodView): boolean {
    return this.podStats(pod).available;
  }

  cpuValue(pod: PodView): number {
    return cpuProgressValue(this.podStats(pod));
  }

  memoryValue(pod: PodView): number {
    return memoryProgressValue(this.podStats(pod));
  }

  metricLabel(pod: PodView, metric: 'cpu' | 'memory'): string {
    const stats = this.podStats(pod);
    if (!stats.available) {
      return 'unavailable';
    }
    if (metric === 'cpu') {
      return formatCpuPercent(stats.cpuPercentHostTotal);
    }
    return formatMemoryDisplay(stats);
  }

  statsSourceLabel(pod: PodView): string {
    const stats = this.podStats(pod);
    if (!stats.available) {
      return 'No live Podman stats yet';
    }
    const noun = stats.containerCount === 1 ? 'container' : 'containers';
    return `Live Podman stats · ${stats.containerCount} ${noun} sampled`;
  }

  containerStateSummary(pod: PodView): string {
    const running = pod.containers.filter((container) => this.normalizedContainerState(container.state) === 'running').length;
    const exited = pod.containers.filter((container) => ['exited', 'stopped'].includes(this.normalizedContainerState(container.state))).length;
    if (exited > 0) {
      return `${running} running / ${exited} exited`;
    }
    return `${running} running`;
  }

  podIssue(pod: PodView): string {
    if (this.normalizedState(pod) === 'degraded') {
      const exited = pod.containers.filter((container) => ['exited', 'stopped'].includes(this.normalizedContainerState(container.state))).map((container) => container.name);
      return exited.length ? `Exited: ${exited.join(', ')}` : 'Podman reports this pod as degraded.';
    }
    if (pod.self_management && this.normalizedState(pod) === 'unknown') {
      return 'Dev stack: web/API runs directly on localhost, not inside a managed Podman pod.';
    }
    return '';
  }

  uptime(pod: PodView): string {
    const created = Date.parse(pod.created_at);
    if (!created) {
      return 'Unknown';
    }
    const hours = Math.max(0, Math.floor((Date.now() - created) / 3600000));
    if (hours < 1) {
      return 'Less than 1h';
    }
    return `${hours}h`;
  }

  stateClass(pod: PodView): string {
    return `state-${this.normalizedState(pod)}`;
  }

  podCardClass(pod: PodView): string {
    return `pod-card ${this.stateClass(pod)}`;
  }

  canStart(pod: PodView): boolean {
    if (this.isDevelopmentSelfPlaceholder(pod) || !this.agentOnline(pod)) {
      return false;
    }
    return ['stopped', 'exited', 'killed', 'created', 'degraded'].includes(this.normalizedState(pod));
  }

  canStop(pod: PodView): boolean {
    if (this.isDevelopmentSelfPlaceholder(pod) || !this.agentOnline(pod)) {
      return false;
    }
    return ['running', 'degraded'].includes(this.normalizedState(pod));
  }

  canRestart(pod: PodView): boolean {
    if (this.isDevelopmentSelfPlaceholder(pod) || !this.agentOnline(pod)) {
      return false;
    }
    return ['running', 'degraded'].includes(this.normalizedState(pod));
  }

  canKill(pod: PodView): boolean {
    if (this.isDevelopmentSelfPlaceholder(pod) || !this.agentOnline(pod)) {
      return false;
    }
    return ['running', 'degraded'].includes(this.normalizedState(pod));
  }

  canDelete(pod: PodView): boolean {
    return !this.isDevelopmentSelfPlaceholder(pod) && this.agentOnline(pod);
  }

  shellContainerId(pod: PodView): string {
    if (this.isDevelopmentSelfPlaceholder(pod) || !this.agentOnline(pod)) {
      return '';
    }
    const running = pod.containers.find((container) => this.normalizedContainerState(container.state) === 'running' && !container.name.toLowerCase().includes('infra'));
    const fallback = pod.containers.find((container) => this.normalizedContainerState(container.state) === 'running');
    return (running ?? fallback)?.id ?? '';
  }

  agentOnline(pod: PodView): boolean {
    const agent = this.agents().find((item) => item.id === pod.agent_id);
    return !agent || agent.status === 'online';
  }

  agentWarning(): string {
    if (this.agents().some((agent) => agent.status === 'offline')) {
      return 'An agent is offline. Pod actions stay disabled until UI proxy, web server, agent socket/token, and Podman health checks recover.';
    }
    if (this.agents().some((agent) => agent.status === 'registered' && this.isUnsetTimestamp(agent.last_seen_at))) {
      return 'Agent is registered but has not reported a live heartbeat yet. Pod metrics and live actions may be unavailable until the agent is running.';
    }
    return '';
  }

  private normalizedState(pod: PodView): string {
    return (pod.state || 'unknown').toLowerCase();
  }

  private normalizedContainerState(state: string): string {
    return (state || 'unknown').toLowerCase();
  }

  private isDevelopmentSelfPlaceholder(pod: PodView): boolean {
    return pod.self_management && this.normalizedState(pod) === 'unknown';
  }

  private isUnsetTimestamp(value?: string): boolean {
    return !value || value.startsWith('0001-01-01');
  }

  private async confirm(pod: PodView, action: LifecycleAction | 'delete'): Promise<ConfirmationDialogResult | undefined> {
    const destructive = action === 'kill' || action === 'delete';
    const label = action[0].toUpperCase() + action.slice(1);
    const dialogRef = this.dialog.open(ConfirmationDialogComponent, {
      data: {
        title: `${label} pod`,
        message: destructive ? `This will ${action} ${pod.name}. Type the pod name to continue.` : `Confirm ${action} for ${pod.name}.`,
        confirmLabel: label,
        icon: destructive ? 'warning' : 'task_alt',
        warn: destructive,
        expectedName: destructive ? pod.name : undefined
      }
    });
    return firstValueFrom(dialogRef.afterClosed());
  }

  private actionPayload(action: LifecycleAction, result: ConfirmationDialogResult): Record<string, unknown> {
    if (action === 'kill') {
      return { confirm_name: result.confirm_name };
    }
    return { confirm: true };
  }

  private formatError(error: unknown): string {
    if (error instanceof ApiError) {
      if (error.status === 401) {
        return 'Authentication required. Sign in again to view pods.';
      }
      return `${error.message} Correlation ID: ${error.correlationId}`;
    }
    return 'Pod request failed.';
  }
}
