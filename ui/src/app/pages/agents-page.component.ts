import { DatePipe } from '@angular/common';
import { Component, OnDestroy, OnInit, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatDialog, MatDialogModule } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatSnackBar, MatSnackBarModule } from '@angular/material/snack-bar';
import { MatTooltipModule } from '@angular/material/tooltip';
import { firstValueFrom } from 'rxjs';
import { ApiError, ApiService } from '../core/api.service';
import { Agent, SystemStatus } from '../core/models';
import { ConfirmationDialogComponent } from '../shared/confirmation-dialog/confirmation-dialog.component';
import { HelpTooltipComponent } from '../shared/help-tooltip/help-tooltip.component';

type AgentHealth = Record<string, unknown>;
type HealthLayer = { key: string; label: string; help: string; value: string; ok: boolean; bad: boolean };

@Component({
  selector: 'app-agents-page',
  standalone: true,
  imports: [DatePipe, FormsModule, HelpTooltipComponent, MatButtonModule, MatCardModule, MatChipsModule, MatDialogModule, MatFormFieldModule, MatIconModule, MatInputModule, MatProgressBarModule, MatSnackBarModule, MatTooltipModule],
  templateUrl: './agents-page.component.html',
  styleUrls: ['./agents-page.component.scss']
})
export class AgentsPageComponent implements OnInit, OnDestroy {
  readonly agents = signal<Agent[]>([]);
  readonly healthByAgent = signal<Record<string, AgentHealth>>({});
  readonly systemStatus = signal<SystemStatus | null>(null);
  readonly selectedAgentId = signal('');
  readonly loading = signal(true);
  readonly refreshing = signal(false);
  readonly lastRefreshedAt = signal<Date | null>(null);

  newAgentId = 'primary';
  linuxUsername = '';
  linuxUid = 1000;
  socketPath = '';
  token = '';
  error = '';

  private refreshTimer: number | undefined;
  readonly helpTopics = {
    page: 'Agents are small local processes that let the web app inspect and control rootless Podman for a Linux user.',
    agentSocketPath: 'Unix socket file used by the web server to talk to the PoDorel agent. If this path is wrong or missing, live Podman actions will fail.',
    lastSeen: 'The last time the web server received a successful health response from this agent.',
    token: 'One-time secret used to authenticate an agent or scoped agent login. Store it now because it is not shown again.',
    agentUser: 'Linux account that owns the rootless Podman session this agent controls.',
    agentMode: 'How the agent is running, such as development, test, or normal runtime mode.',
    podmanSocketGuide: 'For rootless Podman, enable the user podman.socket unit for the same Linux user that runs the PoDorel agent.',
    supervisor: 'The development supervisor owns the local agent, web API, and UI processes. Foreground mode stops when the terminal or user session exits.',
    register: 'Registering creates an agent record and token. It does not install the agent service by itself.'
  };

  private readonly layerDefinitions = [
    { key: 'ui_proxy', label: 'UI proxy', help: 'Checks whether the browser-facing UI can route /api requests to the web server.' },
    { key: 'web_server', label: 'Web server', help: 'Checks whether the Go web API is reachable and answering health requests.' },
    { key: 'agent_socket_available', label: 'Agent socket', help: 'Checks whether the web server can reach the local Unix socket exposed by the PoDorel agent.' },
    { key: 'token_available', label: 'Token', help: 'Checks whether the web server has a valid shared token for authenticating to the agent.' },
    { key: 'agent_api', label: 'Agent API', help: 'Checks whether the agent itself responds after socket and token authentication succeed.' },
    { key: 'podman_socket_available', label: 'Podman socket', help: 'Checks whether the agent can reach the rootless Podman API socket for the target Linux user. Enable it with systemctl --user enable --now podman.socket for that user.' },
    { key: 'podman_cli_available', label: 'Podman CLI', help: 'Checks whether the agent can run the podman command as a fallback when the Podman socket is unavailable.' }
  ];

  constructor(private readonly api: ApiService, private readonly dialog: MatDialog, private readonly snackBar: MatSnackBar) {}

  ngOnInit(): void {
    void this.refresh();
    this.refreshTimer = window.setInterval(() => void this.refresh(true), 10_000);
  }

  ngOnDestroy(): void {
    if (this.refreshTimer !== undefined) {
      window.clearInterval(this.refreshTimer);
    }
  }

  async refresh(silent = false): Promise<void> {
    if (this.refreshing()) {
      return;
    }
    this.error = '';
    this.refreshing.set(true);
    if (!silent && this.agents().length === 0) {
      this.loading.set(true);
    }
    try {
      const [agents, systemStatus] = await Promise.all([this.api.agents(), this.api.systemStatus().catch(() => null)]);
      this.systemStatus.set(systemStatus);
      if (agents.length > 0 && !this.selectedAgentId()) {
        this.selectedAgentId.set(agents[0].id);
      }
      const healthByAgent = await this.loadAgentHealth(agents);
      this.healthByAgent.set(healthByAgent);
      this.agents.set(agents.map((agent) => this.mergeAgentHealth(agent, healthByAgent[agent.id])));
      const primary = agents.find((agent) => agent.id === 'primary') ?? agents[0];
      if (primary && !this.socketPath) {
        this.socketPath = primary.socket_path;
      }
      this.lastRefreshedAt.set(new Date());
    } catch (error) {
      this.error = this.formatError(error);
    } finally {
      this.loading.set(false);
      this.refreshing.set(false);
    }
  }

  async register(): Promise<void> {
    this.error = '';
    try {
      const created = await this.api.registerAgent({
        id: this.newAgentId,
        linux_username: this.linuxUsername,
        linux_uid: this.linuxUid,
        socket_path: this.socketPath
      });
      this.token = created.token;
      this.selectedAgentId.set(created.agent.id);
      await this.refresh();
      this.snackBar.open(`Agent ${created.agent.id} registered`, 'Dismiss', { duration: 3500 });
    } catch (error) {
      this.error = this.formatError(error);
    }
  }

  async rotate(agent: Agent): Promise<void> {
    const dialogRef = this.dialog.open(ConfirmationDialogComponent, {
      data: {
        title: 'Rotate agent token',
        message: `Type ${agent.id} to revoke existing tokens and issue a new one.`,
        confirmLabel: 'Rotate',
        icon: 'vpn_key',
        warn: true,
        expectedName: agent.id
      }
    });
    const result = await firstValueFrom(dialogRef.afterClosed());
    if (!result?.confirmed) {
      return;
    }
    try {
      const created = await this.api.rotateAgentToken(agent.id);
      this.token = created.token;
      this.snackBar.open(`Token rotated for ${agent.id}`, 'Dismiss', { duration: 3500 });
    } catch (error) {
      this.error = this.formatError(error);
    }
  }

  async checkHealth(agent: Agent): Promise<void> {
    this.error = '';
    this.selectedAgentId.set(agent.id);
    try {
      const health = await this.api.agentHealth(agent.id);
      this.healthByAgent.update((current) => ({ ...current, [agent.id]: health }));
      this.agents.update((agents) => agents.map((item) => item.id === agent.id ? this.mergeAgentHealth(item, health) : item));
      this.lastRefreshedAt.set(new Date());
    } catch (error) {
      const health = this.unavailableHealth(agent.id, error);
      this.healthByAgent.update((current) => ({ ...current, [agent.id]: health }));
      this.agents.update((agents) => agents.map((item) => item.id === agent.id ? this.mergeAgentHealth(item, health) : item));
      this.error = this.formatError(error);
    }
  }

  onlineCount(): number {
    return this.agents().filter((agent) => this.isOnline(agent)).length;
  }

  offlineCount(): number {
    return this.agents().filter((agent) => !this.isOnline(agent)).length;
  }

  agentStatus(agent: Agent): string {
    return this.valueText(this.agentHealth(agent)?.['status'] ?? agent.status ?? 'unknown');
  }

  statusClass(agent: Agent): string {
    const status = this.agentStatus(agent).toLowerCase().replace(/[^a-z0-9]+/g, '-');
    return `status-${status || 'unknown'}`;
  }

  statusIcon(agent: Agent): string {
    if (this.isOnline(agent)) {
      return 'check_circle';
    }
    return this.agentStatus(agent) === 'registered' ? 'pending' : 'error';
  }

  isOnline(agent: Agent): boolean {
    const status = this.agentStatus(agent).toLowerCase();
    return status === 'ok' || status === 'online';
  }

  agentHealth(agent: Agent): AgentHealth | null {
    return this.healthByAgent()[agent.id] ?? null;
  }

  selectedAgent(): Agent | null {
    return this.agents().find((agent) => agent.id === this.selectedAgentId()) ?? this.agents()[0] ?? null;
  }

  selectedHealthRecord(): AgentHealth | null {
    const selected = this.selectedAgent();
    return selected ? this.agentHealth(selected) : null;
  }

  healthLayers(agent: Agent): HealthLayer[] {
    return this.healthDetails(this.agentHealth(agent));
  }

  healthDetails(health: AgentHealth | null): HealthLayer[] {
    return this.layerDefinitions.map((layer) => {
      const value = this.valueText(health?.[layer.key]);
      return { ...layer, value, ok: this.isHealthyValue(value), bad: this.isBadValue(value) };
    });
  }

  healthError(health: AgentHealth | null): string {
    return this.valueText(health?.['last_error']) === 'unknown' ? '' : this.valueText(health?.['last_error']);
  }

  healthMeta(health: AgentHealth | null, key: string): string {
    return this.valueText(health?.[key]);
  }

  podmanSocketUnavailable(): boolean {
    const health = this.selectedHealthRecord();
    if (!health) {
      return false;
    }
    return this.isBadValue(this.valueText(health['podman_socket_available']));
  }

  podmanSocketPath(): string {
    const healthPath = this.valueText(this.selectedHealthRecord()?.['podman_socket_path']);
    if (healthPath !== 'unknown') {
      return healthPath;
    }
    const agent = this.selectedAgent();
    if (agent?.linux_uid !== undefined) {
      return `/run/user/${agent.linux_uid}/podman/podman.sock`;
    }
    return '$XDG_RUNTIME_DIR/podman/podman.sock';
  }

  podmanSocketCommands(): Array<{ label: string; command: string; note: string }> {
    const agent = this.selectedAgent();
    const username = agent?.linux_username || '<agent-user>';
    const socketPath = this.podmanSocketPath();
    return [
      {
        label: 'Enable socket as the agent user',
        command: 'systemctl --user enable --now podman.socket',
        note: `Run this while logged in as ${username}, the Linux user that owns this rootless Podman session.`
      },
      {
        label: 'Verify the socket file',
        command: `systemctl --user status podman.socket --no-pager
ls -l ${socketPath}`,
        note: 'The socket file should exist and be owned by the agent user.'
      },
      {
        label: 'Keep it available after logout',
        command: `sudo loginctl enable-linger ${username}`,
        note: 'Run once as an administrator if the agent must keep working when that user is not logged in.'
      }
    ];
  }

  async copyCommand(command: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(command);
      this.snackBar.open('Command copied', 'Dismiss', { duration: 2500 });
    } catch {
      this.snackBar.open('Copy failed; select the command manually.', 'Dismiss', { duration: 4000 });
    }
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
      return 'PoDorel is running in foreground dev mode. If that terminal or user session exits, the agent, API, and UI will stop.';
    }
    if (status !== 'unknown' && status !== 'running' && status !== 'ok') {
      return this.devSupervisorMessage();
    }
    return '';
  }

  lastSeen(agent: Agent): string {
    const fromHealth = this.valueText(this.agentHealth(agent)?.['last_seen_at']);
    return fromHealth === 'unknown' ? (agent.last_seen_at ?? '') : fromHealth;
  }

  private async loadAgentHealth(agents: Agent[]): Promise<Record<string, AgentHealth>> {
    const entries = await Promise.all(agents.map(async (agent) => {
      try {
        return [agent.id, await this.api.agentHealth(agent.id)] as const;
      } catch (error) {
        return [agent.id, this.unavailableHealth(agent.id, error)] as const;
      }
    }));
    return Object.fromEntries(entries);
  }

  private mergeAgentHealth(agent: Agent, health: AgentHealth | undefined): Agent {
    if (!health) {
      return agent;
    }
    const status = this.valueText(health['status']);
    const lastSeenAt = this.valueText(health['last_seen_at']);
    return {
      ...agent,
      status: status === 'unknown' ? agent.status : status,
      last_seen_at: lastSeenAt === 'unknown' ? agent.last_seen_at : lastSeenAt
    };
  }

  private unavailableHealth(agentId: string, error: unknown): AgentHealth {
    return {
      agent_id: agentId,
      status: 'offline',
      agent_api: 'error',
      last_error: this.formatError(error)
    };
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

  private isHealthyValue(value: string): boolean {
    const normalized = value.toLowerCase();
    return normalized === 'ok' || normalized === 'online' || normalized === 'available' || normalized === 'true';
  }

  private isBadValue(value: string): boolean {
    const normalized = value.toLowerCase();
    return normalized === 'offline' || normalized === 'unavailable' || normalized === 'false' || normalized === 'error' || normalized === 'missing' || normalized.includes('unavailable');
  }

  private formatError(error: unknown): string {
    if (error instanceof ApiError) {
      return `${error.message} Correlation ID: ${error.correlationId}`;
    }
    return 'Agent request failed.';
  }
}
