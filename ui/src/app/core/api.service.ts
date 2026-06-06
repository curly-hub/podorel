import { ApplicationRef, Injectable, signal } from '@angular/core';
import {
  Agent,
  ApiEnvelope,
  AppSettings,
  AuditEvent,
  ComposeStack,
  Container,
  CreatedAgentToken,
  CurrentUser,
  DebugTrace,
  HostPackageUpdate,
  ImageDigest,
  LifecycleAction,
  LogHistory,
  PasskeyBeginResponse,
  PasskeyCredential,
  PodDetail,
  PodTemplate,
  PodView,
  ResourceSample,
  RuntimeMode,
  ScannerOptions,
  SecurityFinding,
  SecurityScan,
  SecuritySummary,
  SystemStatus
} from './models';

export class ApiError extends Error {
  constructor(
    message: string,
    readonly correlationId: string,
    readonly code: string,
    readonly status: number,
    readonly details: Record<string, unknown> = {}
  ) {
    const detailText = errorDetailsLabel(details);
    super(`${message}${detailText ? ` ${detailText}` : ''}`);
  }
}

export function formatApiError(error: unknown, fallback: string): string {
  if (!(error instanceof ApiError)) {
    return fallback;
  }
  return `${error.message} Correlation ID: ${error.correlationId}`;
}

function errorDetailsLabel(details: Record<string, unknown>): string {
  const parts = [
    labeledValue('Agent', details['agent_id']),
    labeledValue('Target', details['target_id']),
    labeledValue('Action', details['action']),
    labeledValue('Reason', details['error'] ?? details['reason'])
  ].filter((value) => value !== '');
  return parts.length ? `Details: ${parts.join(' · ')}.` : '';
}

function labeledValue(label: string, value: unknown): string {
  const text = typeof value === 'string' || typeof value === 'number' ? String(value).trim() : '';
  return text ? `${label}: ${text}` : '';
}

@Injectable({ providedIn: 'root' })
export class ApiService {
  readonly csrfToken = signal<string>('');
  readonly currentUser = signal<CurrentUser | null>(null);
  private viewRefreshTimer: ReturnType<typeof setTimeout> | null = null;

  constructor(private readonly appRef: ApplicationRef) {}

  async login(username: string, password: string): Promise<void> {
    const data = await this.post<{ csrf_token: string; user: CurrentUser }>('/api/auth/login', { username, password }, false);
    this.csrfToken.set(data.csrf_token);
    this.currentUser.set(data.user);
  }

  async loginWithAgentToken(token: string): Promise<void> {
    const data = await this.post<{ csrf_token: string; scope: Record<string, unknown> }>('/api/auth/login-agent-token', { token }, false);
    this.csrfToken.set(data.csrf_token);
    this.currentUser.set({ session_type: 'agent_token', ...data.scope });
  }

  beginPasskeyLogin(): Promise<PasskeyBeginResponse> {
    return this.post<PasskeyBeginResponse>('/api/auth/passkeys/login/begin', {}, false);
  }

  async finishPasskeyLogin(flowId: string, credential: unknown): Promise<void> {
    const data = await this.post<{ csrf_token: string; user: CurrentUser }>(`/api/auth/passkeys/login/finish?flow_id=${encodeURIComponent(flowId)}`, credential, false);
    this.csrfToken.set(data.csrf_token);
    this.currentUser.set(data.user);
  }

  async passkeys(): Promise<PasskeyCredential[]> {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 5000);
    try {
      const response = await fetch('/api/auth/passkeys', {
        credentials: 'include',
        cache: 'no-store',
        headers: { Accept: 'application/json' },
        signal: controller.signal
      });
      return await this.unwrap<PasskeyCredential[]>(response);
    } catch (error) {
      if (this.isAbortError(error)) {
        throw new ApiError('Passkey list request timed out.', 'unavailable', 'REQUEST_TIMEOUT', 408);
      }
      throw error;
    } finally {
      clearTimeout(timeout);
      this.scheduleViewRefresh();
    }
  }

  beginPasskeyRegistration(name: string): Promise<PasskeyBeginResponse> {
    return this.post<PasskeyBeginResponse>('/api/auth/passkeys/register/begin', { name });
  }

  finishPasskeyRegistration(flowId: string, credential: unknown): Promise<PasskeyCredential> {
    return this.post<PasskeyCredential>(`/api/auth/passkeys/register/finish?flow_id=${encodeURIComponent(flowId)}`, credential);
  }

  deletePasskey(id: string): Promise<Record<string, unknown>> {
    return this.delete<Record<string, unknown>>(`/api/auth/passkeys/${encodeURIComponent(id)}`, {});
  }

  async changeAdminPassword(currentPassword: string, newPassword: string): Promise<Record<string, unknown>> {
    const data = await this.post<{ changed: boolean; user?: CurrentUser }>('/api/auth/change-password', {
      current_password: currentPassword,
      new_password: newPassword
    });
    if (data.user) {
      this.currentUser.set(data.user);
    }
    return data;
  }

  async logout(): Promise<void> {
    await this.post<Record<string, unknown>>('/api/auth/logout', {});
    this.csrfToken.set('');
    this.currentUser.set(null);
  }

  async me(): Promise<CurrentUser> {
    const data = await this.get<{ csrf_token?: string; user: CurrentUser }>('/api/auth/me');
    if (data.csrf_token) {
      this.csrfToken.set(data.csrf_token);
    }
    this.currentUser.set(data.user);
    return data.user;
  }

  health(): Promise<Record<string, unknown>> {
    return this.get<Record<string, unknown>>('/api/health');
  }

  async downloadTLSCA(): Promise<Blob> {
    try {
      const response = await fetch('/api/system/tls-ca', { credentials: 'include' });
      if (!response.ok) {
        await this.unwrap<never>(response);
      }
      return response.blob();
    } finally {
      this.scheduleViewRefresh();
    }
  }

  systemStatus(): Promise<SystemStatus> {
    return this.get<SystemStatus>('/api/system/status');
  }

  agents(): Promise<Agent[]> {
    return this.get<Agent[]>('/api/agents');
  }

  registerAgent(payload: Record<string, unknown>): Promise<CreatedAgentToken> {
    return this.post<CreatedAgentToken>('/api/agents/register', payload);
  }

  rotateAgentToken(agentId: string): Promise<CreatedAgentToken> {
    return this.post<CreatedAgentToken>(`/api/agents/${encodeURIComponent(agentId)}/rotate-token`, {});
  }

  agentHealth(agentId: string): Promise<Record<string, unknown>> {
    return this.get<Record<string, unknown>>(`/api/agents/${encodeURIComponent(agentId)}/health`);
  }

  pods(): Promise<PodView[]> {
    return this.get<PodView[]>('/api/pods');
  }

  pod(podId: string): Promise<PodDetail> {
    return this.get<PodDetail>(`/api/pods/${encodeURIComponent(podId)}`);
  }

  podAction(podId: string, action: LifecycleAction, payload: Record<string, unknown>): Promise<Record<string, unknown>> {
    return this.post<Record<string, unknown>>(`/api/pods/${encodeURIComponent(podId)}/${action}`, payload);
  }

  deletePod(podId: string, payload: Record<string, unknown>): Promise<Record<string, unknown>> {
    return this.delete<Record<string, unknown>>(`/api/pods/${encodeURIComponent(podId)}`, payload);
  }

  containers(podId = ''): Promise<Container[]> {
    const query = podId ? `?pod_id=${encodeURIComponent(podId)}` : '';
    return this.get<Container[]>(`/api/containers${query}`);
  }

  container(containerId: string): Promise<Container> {
    return this.get<Container>(`/api/containers/${encodeURIComponent(containerId)}`);
  }

  containerAction(containerId: string, action: LifecycleAction, payload: Record<string, unknown>): Promise<Record<string, unknown>> {
    return this.post<Record<string, unknown>>(`/api/containers/${encodeURIComponent(containerId)}/${action}`, payload);
  }

  deleteContainer(containerId: string, payload: Record<string, unknown>): Promise<Record<string, unknown>> {
    return this.delete<Record<string, unknown>>(`/api/containers/${encodeURIComponent(containerId)}`, payload);
  }

  currentStats(): Promise<ResourceSample[]> {
    return this.get<ResourceSample[]>('/api/stats/current');
  }

  statsHistory(since = '24h'): Promise<ResourceSample[]> {
    return this.get<ResourceSample[]>(`/api/stats/history?since=${encodeURIComponent(since)}`);
  }

  logsHistory(params: { agentId?: string; podId?: string; containerId?: string; limit?: number } = {}): Promise<LogHistory> {
    return this.get<LogHistory>(this.logsPath(params));
  }

  async downloadLogs(params: { agentId?: string; podId?: string; containerId?: string; limit?: number } = {}): Promise<string> {
    try {
      const response = await fetch(`${this.logsPath(params)}&download=true`, { credentials: 'include' });
      if (!response.ok) {
        return this.unwrap<string>(response);
      }
      return response.text();
    } finally {
      this.scheduleViewRefresh();
    }
  }

  async templates(): Promise<PodTemplate[]> {
    const templates = await this.get<PodTemplate[]>('/api/templates');
    return templates.map((template) => this.normalizePodTemplate(template));
  }

  async composeStacks(): Promise<ComposeStack[]> {
    const stacks = await this.get<ComposeStack[]>('/api/compose-stacks');
    return stacks.map((stack) => this.normalizeComposeStack(stack));
  }

  deployComposeStack(payload: Record<string, unknown>): Promise<Record<string, unknown>> {
    return this.post<Record<string, unknown>>('/api/compose-stacks/deploy', payload);
  }

  audit(limit = 100): Promise<AuditEvent[]> {
    return this.get<AuditEvent[]>(`/api/audit?limit=${encodeURIComponent(String(limit))}`);
  }

  securitySummary(): Promise<SecuritySummary> {
    return this.get<SecuritySummary>('/api/security/summary');
  }

  scannerOptions(): Promise<ScannerOptions> {
    return this.get<ScannerOptions>('/api/security/scanner-options');
  }

  scanSecurity(): Promise<SecurityScan> {
    return this.post<SecurityScan>('/api/security/scan', {});
  }

  securityScan(scanId: string): Promise<SecurityScan> {
    return this.get<SecurityScan>(`/api/security/scans/${encodeURIComponent(scanId)}`);
  }

  securityFindings(scanId = ''): Promise<SecurityFinding[]> {
    const query = scanId ? `?scan_id=${encodeURIComponent(scanId)}` : '';
    return this.get<SecurityFinding[]>(`/api/security/findings${query}`);
  }

  imageDigests(): Promise<ImageDigest[]> {
    return this.get<ImageDigest[]>('/api/security/image-digests');
  }

  hostPackageUpdates(): Promise<HostPackageUpdate[]> {
    return this.get<HostPackageUpdate[]>('/api/security/host-updates');
  }

  createFromTemplate(payload: Record<string, unknown>): Promise<Record<string, unknown>> {
    return this.post<Record<string, unknown>>('/api/pods/create-from-template', payload);
  }

  buildDockerfile(payload: Record<string, unknown>): Promise<Record<string, unknown>> {
    return this.post<Record<string, unknown>>('/api/images/build-from-dockerfile', payload);
  }

  createSecret(payload: Record<string, unknown>): Promise<Record<string, unknown>> {
    return this.post<Record<string, unknown>>('/api/secrets', payload);
  }

  settings(): Promise<AppSettings> {
    return this.get<AppSettings>('/api/settings');
  }

  updateSettings(payload: Record<string, unknown>): Promise<Record<string, unknown>> {
    return this.put<Record<string, unknown>>('/api/settings', payload);
  }

  runtimeMode(): Promise<RuntimeMode> {
    return this.get<RuntimeMode>('/api/diagnostics/runtime-mode');
  }

  traces(correlationId = ''): Promise<DebugTrace[] | { traces: DebugTrace[]; redacted: boolean }> {
    const query = correlationId ? `?correlation_id=${encodeURIComponent(correlationId)}` : '';
    return this.get<DebugTrace[] | { traces: DebugTrace[]; redacted: boolean }>(`/api/diagnostics/traces${query}`);
  }

  diagnosticsStats(containerId: string): Promise<ResourceSample> {
    return this.get<ResourceSample>(`/api/diagnostics/stats/${encodeURIComponent(containerId)}`);
  }

  diagnosticsBundle(payload: Record<string, unknown>): Promise<Record<string, unknown>> {
    return this.post<Record<string, unknown>>('/api/diagnostics/bundle', payload);
  }

  async get<T>(path: string): Promise<T> {
    try {
      const response = await fetch(path, { credentials: 'include' });
      return await this.unwrap<T>(response);
    } finally {
      this.scheduleViewRefresh();
    }
  }

  async post<T>(path: string, body: unknown, includeCsrf = true): Promise<T> {
    return this.mutate<T>('POST', path, body, includeCsrf);
  }

  async put<T>(path: string, body: unknown): Promise<T> {
    return this.mutate<T>('PUT', path, body, true);
  }

  async delete<T>(path: string, body: unknown): Promise<T> {
    return this.mutate<T>('DELETE', path, body, true);
  }

  private async mutate<T>(method: 'POST' | 'PUT' | 'DELETE', path: string, body: unknown, includeCsrf: boolean): Promise<T> {
    if (includeCsrf) {
      await this.ensureCsrfToken();
    }
    try {
      return await this.sendMutation<T>(method, path, body, includeCsrf);
    } catch (error) {
      if (includeCsrf && this.isCsrfError(error)) {
        await this.refreshCsrfToken();
        return this.sendMutation<T>(method, path, body, includeCsrf);
      }
      throw error;
    }
  }

  private async sendMutation<T>(method: 'POST' | 'PUT' | 'DELETE', path: string, body: unknown, includeCsrf: boolean): Promise<T> {
    try {
      const headers: Record<string, string> = { 'Content-Type': 'application/json' };
      if (includeCsrf && this.csrfToken()) {
        headers['X-CSRF-Token'] = this.csrfToken();
      }
      const response = await fetch(path, { method, credentials: 'include', headers, body: JSON.stringify(body) });
      return await this.unwrap<T>(response);
    } finally {
      this.scheduleViewRefresh();
    }
  }

  private async ensureCsrfToken(): Promise<void> {
    if (!this.csrfToken()) {
      await this.refreshCsrfToken();
    }
  }

  private async refreshCsrfToken(): Promise<void> {
    await this.me();
  }

  private isCsrfError(error: unknown): boolean {
    return error instanceof ApiError && error.status === 403 && (error.code === 'CSRF_REQUIRED' || error.code === 'CSRF_INVALID');
  }

  private normalizePodTemplate(template: PodTemplate): PodTemplate {
    return {
      ...template,
      command: Array.isArray(template.command) ? template.command : [],
      ports: Array.isArray(template.ports) ? template.ports : [],
      volumes: Array.isArray(template.volumes) ? template.volumes : [],
      environment: template.environment ?? {},
      secrets: Array.isArray(template.secrets) ? template.secrets : [],
      health_command: Array.isArray(template.health_command) ? template.health_command : [],
      resource_limits: template.resource_limits ?? { cpu: '', memory: '' },
      labels: template.labels ?? {},
      ui_notes: Array.isArray(template.ui_notes) ? template.ui_notes : []
    };
  }

  private normalizeComposeStack(stack: ComposeStack): ComposeStack {
    return {
      ...stack,
      compose_files: Array.isArray(stack.compose_files) ? stack.compose_files : [],
      services: Array.isArray(stack.services)
        ? stack.services.map((service) => ({
            ...service,
            ports: Array.isArray(service.ports) ? service.ports : undefined,
            profiles: Array.isArray(service.profiles) ? service.profiles : undefined
          }))
        : [],
      environment_files: Array.isArray(stack.environment_files) ? stack.environment_files : [],
      required_files: Array.isArray(stack.required_files) ? stack.required_files : [],
      notes: Array.isArray(stack.notes) ? stack.notes : [],
      labels: stack.labels ?? {}
    };
  }

  private isAbortError(error: unknown): boolean {
    return typeof DOMException !== 'undefined' && error instanceof DOMException && error.name === 'AbortError';
  }

  private async unwrap<T>(response: Response): Promise<T> {
    try {
      let envelope: ApiEnvelope<T>;
      try {
        envelope = (await response.json()) as ApiEnvelope<T>;
      } catch {
        throw new ApiError(`Request failed with ${response.status}`, 'unavailable', 'INVALID_RESPONSE', response.status);
      }
      if (!response.ok || !envelope.ok) {
        const message = envelope.error?.message ?? `Request failed with ${response.status}`;
        if (response.status === 401) {
          this.currentUser.set(null);
        }
        throw new ApiError(message, envelope.correlation_id, envelope.error?.code ?? 'REQUEST_FAILED', response.status, envelope.error?.details ?? {});
      }
      return envelope.data as T;
    } finally {
      this.scheduleViewRefresh();
    }
  }

  private scheduleViewRefresh(): void {
    if (this.viewRefreshTimer !== null) {
      return;
    }
    this.viewRefreshTimer = setTimeout(() => {
      this.viewRefreshTimer = null;
      try {
        this.appRef.tick();
      } catch {
        // A tick may already be running; the next scheduled API/user event will refresh the view.
      }
    }, 0);
  }

  private logsPath(params: { agentId?: string; podId?: string; containerId?: string; limit?: number }): string {
    const query = new URLSearchParams();
    query.set('limit', String(params.limit ?? 500));
    if (params.agentId) {
      query.set('agent_id', params.agentId);
    }
    if (params.podId) {
      query.set('pod_id', params.podId);
    }
    if (params.containerId) {
      query.set('container_id', params.containerId);
    }
    return `/api/logs/history?${query.toString()}`;
  }
}
