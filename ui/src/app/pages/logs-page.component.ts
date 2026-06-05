import { DatePipe } from '@angular/common';
import { Component, OnDestroy, OnInit, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { MatTabsModule } from '@angular/material/tabs';
import { MatTooltipModule } from '@angular/material/tooltip';
import { ApiError, ApiService } from '../core/api.service';
import { Agent, Container, LogLine, PodView } from '../core/models';

@Component({
  selector: 'app-logs-page',
  standalone: true,
  imports: [DatePipe, FormsModule, MatButtonModule, MatCardModule, MatChipsModule, MatFormFieldModule, MatIconModule, MatInputModule, MatSelectModule, MatTabsModule, MatTooltipModule],
  templateUrl: './logs-page.component.html',
  styleUrls: ['./logs-page.component.scss']
})
export class LogsPageComponent implements OnInit, OnDestroy {
  readonly lines = signal<LogLine[]>([]);
  readonly liveLines = signal<LogLine[]>([]);
  readonly agents = signal<Agent[]>([]);
  readonly pods = signal<PodView[]>([]);
  readonly containers = signal<Container[]>([]);
  readonly error = signal('');
  readonly loading = signal(false);
  readonly downloading = signal(false);
  readonly connecting = signal(false);
  readonly paused = signal(false);
  readonly connected = signal(false);
  search = '';
  source = 'all';
  agentId = '';
  podId = '';
  containerId = '';
  limit = 500;
  historyLoadedAt = '';
  liveStartedAt = '';
  lastLiveAt = '';

  private socket: WebSocket | null = null;

  constructor(private readonly api: ApiService) {}

  ngOnInit(): void {
    void this.loadInitialData();
  }

  ngOnDestroy(): void {
    this.disconnectLive();
  }

  async loadInitialData(): Promise<void> {
    this.error.set('');
    this.loading.set(true);
    const limit = this.normalizedLimit();
    this.limit = limit;
    try {
      const [agents, pods, containers, history] = await Promise.all([
        this.api.agents(),
        this.api.pods(),
        this.api.containers(),
        this.api.logsHistory({ limit })
      ]);
      this.agents.set(agents);
      this.pods.set(pods);
      this.containers.set(containers);
      this.lines.set(history.lines);
      this.historyLoadedAt = new Date().toISOString();
    } catch (error) {
      this.error.set(this.formatError(error));
    } finally {
      this.loading.set(false);
    }
  }

  async applyFilters(): Promise<void> {
    await this.loadHistory();
    if (this.socket || this.connected() || this.connecting()) {
      this.restartLive();
    }
  }

  async loadHistory(): Promise<void> {
    this.error.set('');
    this.loading.set(true);
    const limit = this.normalizedLimit();
    this.limit = limit;
    try {
      const history = await this.api.logsHistory({
        agentId: this.blankToUndefined(this.agentId),
        podId: this.blankToUndefined(this.podId),
        containerId: this.blankToUndefined(this.containerId),
        limit
      });
      this.lines.set(history.lines);
      this.historyLoadedAt = new Date().toISOString();
    } catch (error) {
      this.error.set(this.formatError(error));
    } finally {
      this.loading.set(false);
    }
  }

  connectLive(): void {
    if (this.socket) {
      return;
    }
    this.error.set('');
    this.connecting.set(true);
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const query = this.logQuery();
    const path = query ? '/api/ws/logs?' + query : '/api/ws/logs';
    const socket = new WebSocket(protocol + '//' + location.host + path);
    this.socket = socket;
    socket.onopen = () => {
      if (this.socket !== socket) {
        return;
      }
      this.connected.set(true);
      this.connecting.set(false);
      this.liveStartedAt = new Date().toISOString();
    };
    socket.onmessage = (event) => {
      if (this.socket !== socket) {
        return;
      }
      if (this.paused()) {
        return;
      }
      this.lastLiveAt = new Date().toISOString();
      try {
        const line = JSON.parse(event.data) as LogLine;
        this.liveLines.update((lines) => [...lines.slice(-499), line]);
      } catch {
        this.liveLines.update((lines) => [...lines.slice(-499), { timestamp: new Date().toISOString(), source: 'websocket', line: String(event.data) }]);
      }
    };
    socket.onerror = () => {
      if (this.socket !== socket) {
        return;
      }
      this.connecting.set(false);
      this.error.set('Live log websocket failed. Correlation ID unavailable.');
    };
    socket.onclose = () => {
      if (this.socket !== socket) {
        return;
      }
      this.connected.set(false);
      this.connecting.set(false);
      this.socket = null;
    };
  }

  disconnectLive(): void {
    const socket = this.socket;
    this.socket = null;
    if (socket) {
      socket.onopen = null;
      socket.onmessage = null;
      socket.onerror = null;
      socket.onclose = null;
    }
    socket?.close();
    this.connected.set(false);
    this.connecting.set(false);
  }

  restartLive(): void {
    this.disconnectLive();
    window.setTimeout(() => this.connectLive(), 75);
  }

  togglePause(): void {
    this.paused.update((paused) => !paused);
  }

  clearBuffer(): void {
    this.liveLines.set([]);
  }

  async download(): Promise<void> {
    this.error.set('');
    this.downloading.set(true);
    const limit = this.normalizedLimit();
    this.limit = limit;
    try {
      const content = await this.api.downloadLogs({
        agentId: this.blankToUndefined(this.agentId),
        podId: this.blankToUndefined(this.podId),
        containerId: this.blankToUndefined(this.containerId),
        limit
      });
      const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement('a');
      anchor.href = url;
      anchor.download = `podorel-logs-${this.timestampForFilename()}.txt`;
      anchor.click();
      URL.revokeObjectURL(url);
    } catch (error) {
      this.error.set(this.formatError(error));
    } finally {
      this.downloading.set(false);
    }
  }

  clearFilters(): void {
    this.search = '';
    this.source = 'all';
    this.agentId = '';
    this.podId = '';
    this.containerId = '';
    void this.applyFilters();
  }

  onAgentChanged(): void {
    if (this.podId && !this.podOptions().some((pod) => pod.id === this.podId)) {
      this.podId = '';
    }
    if (this.containerId && !this.containerOptions().some((container) => container.id === this.containerId)) {
      this.containerId = '';
    }
  }

  onPodChanged(): void {
    if (this.containerId && !this.containerOptions().some((container) => container.id === this.containerId)) {
      this.containerId = '';
    }
  }

  onContainerChanged(): void {
    const container = this.containers().find((item) => item.id === this.containerId);
    if (!container) {
      return;
    }
    if (!this.agentId) {
      this.agentId = container.agent_id;
    }
    if (!this.podId) {
      this.podId = container.pod_id;
    }
  }

  sources(): string[] {
    return [...new Set([...this.lines(), ...this.liveLines()].map((line) => line.source))].sort();
  }

  podOptions(): PodView[] {
    const agent = this.agentId.trim();
    return this.pods()
      .filter((pod) => !agent || pod.agent_id === agent)
      .sort((left, right) => this.podLabel(left).localeCompare(this.podLabel(right)));
  }

  containerOptions(): Container[] {
    const agent = this.agentId.trim();
    const pod = this.podId.trim();
    return this.containers()
      .filter((container) => (!agent || container.agent_id === agent) && (!pod || container.pod_id === pod))
      .sort((left, right) => this.containerLabel(left).localeCompare(this.containerLabel(right)));
  }

  podLabel(pod: PodView): string {
    const identity = pod.name && pod.name !== pod.id ? `${pod.name} · ${pod.id}` : pod.id;
    return `${identity} · ${pod.state || 'unknown'}`;
  }

  containerLabel(container: Container): string {
    const identity = container.name && container.name !== container.id ? `${container.name} · ${container.id}` : container.id;
    return `${identity} · ${container.state || 'unknown'}`;
  }

  filteredHistory(): LogLine[] {
    return this.filtered(this.lines());
  }

  filteredLive(): LogLine[] {
    return this.filtered(this.liveLines());
  }

  filtered(lines: LogLine[]): LogLine[] {
    const query = this.search.trim().toLowerCase();
    return lines.filter((line) => {
      const matchesSource = this.source === 'all' || line.source === this.source;
      const matchesQuery = !query || `${line.source} ${line.line}`.toLowerCase().includes(query);
      return matchesSource && matchesQuery;
    });
  }

  activeAgentLabel(): string {
    if (!this.agentId.trim()) {
      return 'Current agent';
    }
    const agent = this.agents().find((item) => item.id === this.agentId);
    return agent ? `${agent.id} · ${agent.status}` : this.agentId;
  }

  filterSummary(): string {
    const parts = [
      this.agentId.trim() ? `agent ${this.agentId.trim()}` : 'current agent',
      this.podId.trim() ? `pod ${this.podId.trim()}` : '',
      this.containerId.trim() ? `container ${this.containerId.trim()}` : '',
      this.source !== 'all' ? `source ${this.source}` : '',
      `${this.normalizedLimit()} lines`
    ].filter(Boolean);
    return parts.join(' · ');
  }

  liveStateLabel(): string {
    if (this.connecting()) {
      return 'Connecting';
    }
    if (this.connected()) {
      return this.paused() ? 'Connected, paused' : 'Connected';
    }
    return 'Disconnected';
  }

  selectedTargetLabel(): string {
    if (this.containerId.trim()) {
      const container = this.containers().find((item) => item.id === this.containerId.trim());
      return container ? `${container.name || container.id} · ${container.state}` : this.containerId.trim();
    }
    if (this.podId.trim()) {
      const pod = this.pods().find((item) => item.id === this.podId.trim());
      return pod ? `${pod.name || pod.id} · ${pod.state}` : this.podId.trim();
    }
    return 'All targets';
  }

  lineTone(line: LogLine): string {
    const text = `${line.source} ${line.line}`.toLowerCase();
    if (/\b(error|failed|failure|panic|fatal|denied|unauthorized|timeout)\b/.test(text)) {
      return 'danger';
    }
    if (/\b(warn|warning|retry|degraded|unhealthy|slow)\b/.test(text)) {
      return 'warning';
    }
    if (/\b(ok|success|ready|started|connected|healthy)\b/.test(text)) {
      return 'success';
    }
    return 'neutral';
  }

  lineIcon(line: LogLine): string {
    const tone = this.lineTone(line);
    if (tone === 'danger') {
      return 'error';
    }
    if (tone === 'warning') {
      return 'warning';
    }
    if (tone === 'success') {
      return 'check_circle';
    }
    return 'notes';
  }

  trackLine(index: number, line: LogLine): string {
    return `${line.timestamp}:${line.source}:${line.line}:${index}`;
  }

  private timestampForFilename(): string {
    return new Date().toISOString().replace(/[:.]/g, '-');
  }

  private logQuery(): string {
    const query = new URLSearchParams();
    const agentId = this.blankToUndefined(this.agentId);
    const podId = this.blankToUndefined(this.podId);
    const containerId = this.blankToUndefined(this.containerId);
    if (agentId) {
      query.set('agent_id', agentId);
    }
    if (podId) {
      query.set('pod_id', podId);
    }
    if (containerId) {
      query.set('container_id', containerId);
    }
    return query.toString();
  }

  private blankToUndefined(value: string): string | undefined {
    const trimmed = value.trim();
    return trimmed ? trimmed : undefined;
  }

  private normalizedLimit(): number {
    const value = Number(this.limit);
    if (!Number.isFinite(value)) {
      return 500;
    }
    return Math.max(1, Math.min(5000, Math.round(value)));
  }

  private formatError(error: unknown): string {
    if (error instanceof ApiError) {
      return `${error.message} Correlation ID: ${error.correlationId}`;
    }
    return 'Logs could not be loaded.';
  }
}
