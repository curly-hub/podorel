import { DatePipe, JsonPipe } from '@angular/common';
import { AfterViewInit, Component, ElementRef, OnDestroy, OnInit, ViewChild, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ActivatedRoute } from '@angular/router';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatDialog, MatDialogModule } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatListModule } from '@angular/material/list';
import { MatSelectModule } from '@angular/material/select';
import { MatSnackBar, MatSnackBarModule } from '@angular/material/snack-bar';
import { MatTabsModule } from '@angular/material/tabs';
import { MatTooltipModule } from '@angular/material/tooltip';
import { FitAddon } from '@xterm/addon-fit';
import { Terminal } from '@xterm/xterm';
import { firstValueFrom } from 'rxjs';
import { ApiError, ApiService } from '../core/api.service';
import { AppSettings, AuditEvent, Container, LifecycleAction, LogLine, ResourceSample } from '../core/models';
import { formatBytes, formatCpuPercent } from '../core/stats';
import { ConfirmationDialogComponent, ConfirmationDialogResult } from '../shared/confirmation-dialog/confirmation-dialog.component';

type ShellMessage = {
  type?: string;
  stream?: string;
  data?: string;
  status?: string;
  message?: string;
  shell?: string;
  exit_code?: number;
  terminal_mode?: string;
};

type DisposableLike = { dispose(): void };

@Component({
  selector: 'app-container-detail-page',
  standalone: true,
  imports: [DatePipe, FormsModule, JsonPipe, MatButtonModule, MatCardModule, MatChipsModule, MatDialogModule, MatFormFieldModule, MatIconModule, MatListModule, MatSelectModule, MatSnackBarModule, MatTabsModule, MatTooltipModule],
  templateUrl: './container-detail-page.component.html',
  styleUrls: ['./container-detail-page.component.scss']
})
export class ContainerDetailPageComponent implements OnInit, AfterViewInit, OnDestroy {
  @ViewChild('terminalHost') private terminalHost?: ElementRef<HTMLElement>;

  readonly container = signal<Container | null>(null);
  readonly stats = signal<ResourceSample | null>(null);
  readonly logs = signal<LogLine[]>([]);
  readonly audit = signal<AuditEvent[]>([]);
  readonly error = signal('');
  readonly loading = signal(true);
  readonly shellConnected = signal(false);
  readonly shellConnecting = signal(false);
  readonly shellStatus = signal('Closed');
  readonly shellError = signal('');
  readonly selectedTabIndex = signal(0);

  execShell = 'sh';
  execEnabled = false;
  settings: AppSettings | null = null;

  private containerId = '';
  private shellSocket: WebSocket | null = null;
  private terminal: Terminal | null = null;
  private fitAddon: FitAddon | null = null;
  private resizeObserver: ResizeObserver | null = null;
  private terminalDisposables: DisposableLike[] = [];

  constructor(
    private readonly route: ActivatedRoute,
    private readonly api: ApiService,
    private readonly dialog: MatDialog,
    private readonly snackBar: MatSnackBar
  ) {}

  ngOnInit(): void {
    this.containerId = this.route.snapshot.paramMap.get('id') ?? '';
    if (this.route.snapshot.queryParamMap.get('tab') === 'shell') {
      this.selectedTabIndex.set(3);
    }
    void this.refresh();
  }

  ngAfterViewInit(): void {
    if (this.selectedTabIndex() === 3) {
      this.initializeTerminal();
    }
  }

  ngOnDestroy(): void {
    this.closeShell();
    this.resizeObserver?.disconnect();
    for (const disposable of this.terminalDisposables) {
      disposable.dispose();
    }
    this.terminal?.dispose();
  }

  async refresh(): Promise<void> {
    this.loading.set(true);
    this.error.set('');
    try {
      const container = await this.api.container(this.containerId);
      this.container.set(container);
      const [logs, audit, settings] = await Promise.allSettled([
        this.api.logsHistory({ containerId: container.id, limit: 200 }),
        this.api.audit(100),
        this.api.settings()
      ]);
      if (logs.status === 'fulfilled') {
        this.logs.set(logs.value.lines);
      }
      if (audit.status === 'fulfilled') {
        this.audit.set(audit.value.filter((event) => event.target_id === container.id));
      }
      if (settings.status === 'fulfilled') {
        this.settings = settings.value;
        this.execEnabled = settings.value.actions?.exec_enabled === true;
      } else {
        this.execEnabled = false;
      }
      try {
        this.stats.set(await this.api.diagnosticsStats(container.id));
      } catch {
        this.stats.set(null);
      }
    } catch (error) {
      this.error.set(this.formatError(error));
    } finally {
      this.loading.set(false);
    }
  }

  selectTab(index: number): void {
    this.selectedTabIndex.set(index);
    if (index === 3) {
      this.initializeTerminal();
      this.fitTerminalAndResize();
    }
  }

  async action(action: LifecycleAction | 'delete'): Promise<void> {
    const container = this.container();
    if (!container) {
      return;
    }
    const destructive = action === 'kill' || action === 'delete';
    const result = await this.confirm(container.name, action, destructive);
    if (!result?.confirmed) {
      return;
    }
    try {
      if (action === 'delete') {
        await this.api.deleteContainer(container.id, { confirm_name: result.confirm_name });
      } else {
        await this.api.containerAction(container.id, action, action === 'kill' ? { confirm_name: result.confirm_name } : { confirm: true });
      }
      this.snackBar.open(`${container.name} ${action} requested`, 'Dismiss', { duration: 3500 });
      await this.refresh();
    } catch (error) {
      this.snackBar.open(this.formatError(error), 'Dismiss', { duration: 7000 });
    }
  }

  openShell(): void {
    const container = this.container();
    if (!container || !this.canOpenShell()) {
      this.shellError.set(this.execDisabledReason());
      return;
    }
    this.initializeTerminal();
    this.fitTerminalAndResize();
    this.closeShell();
    this.terminal?.reset();
    this.shellError.set('');
    this.shellConnecting.set(true);
    this.shellStatus.set('Connecting');
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const query = new URLSearchParams({
      container_id: container.id,
      shell: this.execShell,
      cols: String(this.terminal?.cols ?? 80),
      rows: String(this.terminal?.rows ?? 24)
    });
    const socket = new WebSocket(`${protocol}//${location.host}/api/ws/exec?${query.toString()}`);
    this.shellSocket = socket;

    socket.onopen = () => {
      this.shellConnecting.set(false);
      this.shellConnected.set(true);
      this.shellStatus.set('Connected');
      this.writeTerminalLine(`PoDorel opened ${this.execShell} in ${container.name}`);
      this.sendResize();
      this.focusTerminal();
    };
    socket.onmessage = (event) => this.handleShellMessage(String(event.data));
    socket.onerror = () => {
      this.shellError.set('Shell websocket failed. Check Diagnostics for the agent/socket layer.');
      this.shellConnecting.set(false);
    };
    socket.onclose = () => {
      this.shellConnected.set(false);
      this.shellConnecting.set(false);
      if (this.shellStatus() !== 'Closed') {
        this.shellStatus.set('Closed');
      }
    };
  }

  focusTerminal(): void {
    this.terminal?.focus();
  }

  sendCtrlC(): void {
    this.sendShellInput('\u0003');
  }

  closeShell(): void {
    const socket = this.shellSocket;
    this.shellSocket = null;
    if (socket && socket.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: 'close' }));
      socket.close();
    } else if (socket && socket.readyState === WebSocket.CONNECTING) {
      socket.close();
    }
    this.shellConnected.set(false);
    this.shellConnecting.set(false);
    this.shellStatus.set('Closed');
  }

  clearShell(): void {
    this.terminal?.clear();
  }

  canOpenShell(): boolean {
    return this.execEnabled && this.isRunning() && !this.shellConnected() && !this.shellConnecting();
  }

  execDisabledReason(): string {
    if (!this.execEnabled) {
      return 'Exec shell is disabled. Enable Actions -> Exec shell in Settings first.';
    }
    if (!this.isRunning()) {
      return 'Container must be running before opening a shell.';
    }
    if (this.shellConnected()) {
      return 'A shell is already connected.';
    }
    if (this.shellConnecting()) {
      return 'Shell is connecting.';
    }
    return '';
  }

  isRunning(): boolean {
    return (this.container()?.state || '').toLowerCase() === 'running';
  }

  formatMemory(bytes = 0): string {
    return formatBytes(bytes);
  }

  formatCpu(value = 0): string {
    return formatCpuPercent(value);
  }

  canAction(action: LifecycleAction | 'delete'): boolean {
    const state = (this.container()?.state || 'unknown').toLowerCase();
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

  private initializeTerminal(): void {
    if (this.terminal || !this.terminalHost?.nativeElement) {
      return;
    }
    this.terminal = new Terminal({
      allowTransparency: false,
      convertEol: true,
      cursorBlink: true,
      cursorStyle: 'block',
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
      fontSize: 13,
      lineHeight: 1.2,
      scrollback: 8000,
      tabStopWidth: 4,
      theme: {
        background: '#050807',
        foreground: '#dff8ef',
        cursor: '#ffffff',
        selectionBackground: '#1c5a61',
        black: '#071011',
        red: '#ff8b81',
        green: '#5bd691',
        yellow: '#ffd36a',
        blue: '#7fcaf4',
        magenta: '#d7a6ff',
        cyan: '#46d7df',
        white: '#f0f8f7',
        brightBlack: '#879d99',
        brightRed: '#ffb0a8',
        brightGreen: '#9ff0be',
        brightYellow: '#ffe29a',
        brightBlue: '#a8ddff',
        brightMagenta: '#e6c6ff',
        brightCyan: '#9af4f7',
        brightWhite: '#ffffff'
      }
    });
    this.fitAddon = new FitAddon();
    this.terminal.loadAddon(this.fitAddon);
    this.terminal.open(this.terminalHost.nativeElement);
    this.terminalDisposables.push(
      this.terminal.onData((data) => this.sendShellInput(data)),
      this.terminal.onResize(() => this.sendResize())
    );
    this.resizeObserver = new ResizeObserver(() => this.fitTerminalAndResize());
    this.resizeObserver.observe(this.terminalHost.nativeElement);
    this.fitTerminalAndResize();
  }

  private fitTerminalAndResize(): void {
    if (!this.terminal || !this.fitAddon || !this.terminalHost?.nativeElement) {
      return;
    }
    requestAnimationFrame(() => {
      try {
        this.fitAddon?.fit();
        this.sendResize();
      } catch {
        // The tab can briefly be hidden while Angular Material animates; the next resize/open will refit.
      }
    });
  }

  private sendShellInput(data: string): void {
    if (!this.shellSocket || this.shellSocket.readyState !== WebSocket.OPEN) {
      return;
    }
    this.shellSocket.send(JSON.stringify({ type: 'input', data }));
  }

  private sendResize(): void {
    if (!this.terminal || !this.shellSocket || this.shellSocket.readyState !== WebSocket.OPEN) {
      return;
    }
    this.shellSocket.send(JSON.stringify({ type: 'resize', cols: this.terminal.cols, rows: this.terminal.rows }));
  }

  private handleShellMessage(raw: string): void {
    let message: ShellMessage;
    try {
      message = JSON.parse(raw) as ShellMessage;
    } catch {
      this.terminal?.write(raw);
      return;
    }
    if (message.type === 'output') {
      this.terminal?.write(message.data ?? '');
      return;
    }
    if (message.type === 'status') {
      const status = message.status === 'closed' && typeof message.exit_code === 'number' ? `Closed (${message.exit_code})` : (message.status ?? 'status');
      this.shellStatus.set(status);
      if (message.status === 'closed') {
        this.writeTerminalLine(`PoDorel shell ${status}`);
      }
      return;
    }
    if (message.type === 'error') {
      const detail = message.message ?? 'Shell failed.';
      this.shellError.set(detail);
      this.writeTerminalLine(`PoDorel error: ${detail}`);
    }
  }

  private writeTerminalLine(text: string): void {
    this.terminal?.writeln(`\x1b[38;5;51m${text}\x1b[0m`);
  }

  private async confirm(name: string, action: LifecycleAction | 'delete', destructive: boolean): Promise<ConfirmationDialogResult | undefined> {
    const label = action.charAt(0).toUpperCase() + action.slice(1);
    const dialogRef = this.dialog.open(ConfirmationDialogComponent, {
      data: {
        title: `${label} container`,
        message: destructive ? `Type ${name} to continue.` : `Confirm ${action} for ${name}.`,
        confirmLabel: label,
        icon: destructive ? 'warning' : 'task_alt',
        warn: destructive,
        expectedName: destructive ? name : undefined
      }
    });
    return firstValueFrom(dialogRef.afterClosed());
  }

  private formatError(error: unknown): string {
    if (error instanceof ApiError) {
      return `${error.message} Correlation ID: ${error.correlationId}`;
    }
    return 'Container request failed.';
  }
}
