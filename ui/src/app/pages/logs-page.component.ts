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
import { Agent, LogLine } from '../core/models';

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
  readonly error = signal('');
  readonly paused = signal(false);
  readonly connected = signal(false);
  search = '';
  source = 'all';
  agentId = '';
  podId = '';
  containerId = '';
  limit = 500;

  private socket: WebSocket | null = null;

  constructor(private readonly api: ApiService) {}

  ngOnInit(): void {
    void this.loadInitialData();
  }

  ngOnDestroy(): void {
    this.socket?.close();
  }

  async loadInitialData(): Promise<void> {
    this.error.set('');
    try {
      const [agents, history] = await Promise.all([
        this.api.agents(),
        this.api.logsHistory({ limit: this.limit })
      ]);
      this.agents.set(agents);
      this.lines.set(history.lines);
    } catch (error) {
      this.error.set(this.formatError(error));
    }
  }

  async loadHistory(): Promise<void> {
    this.error.set('');
    try {
      const history = await this.api.logsHistory({
        agentId: this.blankToUndefined(this.agentId),
        podId: this.blankToUndefined(this.podId),
        containerId: this.blankToUndefined(this.containerId),
        limit: this.limit
      });
      this.lines.set(history.lines);
    } catch (error) {
      this.error.set(this.formatError(error));
    }
  }

  connectLive(): void {
    if (this.socket) {
      return;
    }
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const query = this.logQuery();
    const path = query ? '/api/ws/logs?' + query : '/api/ws/logs';
    this.socket = new WebSocket(protocol + '//' + location.host + path);
    this.socket.onopen = () => this.connected.set(true);
    this.socket.onmessage = (event) => {
      if (this.paused()) {
        return;
      }
      try {
        const line = JSON.parse(event.data) as LogLine;
        this.liveLines.update((lines) => [...lines.slice(-499), line]);
      } catch {
        this.liveLines.update((lines) => [...lines.slice(-499), { timestamp: new Date().toISOString(), source: 'websocket', line: String(event.data) }]);
      }
    };
    this.socket.onerror = () => this.error.set('Live log websocket failed. Correlation ID unavailable.');
    this.socket.onclose = () => {
      this.connected.set(false);
      this.socket = null;
    };
  }

  togglePause(): void {
    this.paused.update((paused) => !paused);
  }

  clearBuffer(): void {
    this.liveLines.set([]);
  }

  async download(): Promise<void> {
    try {
      const content = await this.api.downloadLogs({
        agentId: this.blankToUndefined(this.agentId),
        podId: this.blankToUndefined(this.podId),
        containerId: this.blankToUndefined(this.containerId),
        limit: this.limit
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
    }
  }

  sources(): string[] {
    return [...new Set([...this.lines(), ...this.liveLines()].map((line) => line.source))].sort();
  }

  filtered(lines: LogLine[]): LogLine[] {
    const query = this.search.trim().toLowerCase();
    return lines.filter((line) => {
      const matchesSource = this.source === 'all' || line.source === this.source;
      const matchesQuery = !query || `${line.source} ${line.line}`.toLowerCase().includes(query);
      return matchesSource && matchesQuery;
    });
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

  private formatError(error: unknown): string {
    if (error instanceof ApiError) {
      return `${error.message} Correlation ID: ${error.correlationId}`;
    }
    return 'Logs could not be loaded.';
  }
}
