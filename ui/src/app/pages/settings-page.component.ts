import { Component } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { JsonPipe } from '@angular/common';
import { MatButtonModule } from '@angular/material/button';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatSnackBar, MatSnackBarModule } from '@angular/material/snack-bar';
import { MatTooltipModule } from '@angular/material/tooltip';
import { ApiError, ApiService } from '../core/api.service';
import { AppSettings, PasskeyCredential } from '../core/models';
import { credentialToJSON, formatPasskeyError, passkeySecureContext, passkeysSupported, passkeyUnavailableMessage, toPublicKeyCreationOptions } from '../core/passkeys';

interface SettingsDraft {
  execEnabled: boolean;
  automationEnabled: boolean;
  scheduledScans: boolean;
  securitySchedule: string;
  metricsRetentionHours: number;
  logsRetentionHours: number;
  logPerPodLimitMB: number;
  logTotalLimitMB: number;
}

@Component({
  selector: 'app-settings-page',
  standalone: true,
  imports: [FormsModule, JsonPipe, MatButtonModule, MatFormFieldModule, MatIconModule, MatInputModule, MatSnackBarModule, MatTooltipModule],
  templateUrl: './settings-page.component.html',
  styleUrls: ['./settings-page.component.scss']
})
export class SettingsPageComponent {
  settings: AppSettings | null = null;
  passkeys: PasskeyCredential[] = [];
  passkeyName = this.defaultPasskeyName();
  passkeyBusy = false;
  passkeyError = '';
  passkeyResult = '';
  caDownloadBusy = false;
  caResult = '';
  settingsBusy = false;
  saving = false;
  execEnabled = false;
  automationEnabled = false;
  scheduledScans = false;
  securitySchedule = 'daily';
  logTotalLimitMB = 5120;
  logPerPodLimitMB = 100;
  metricsRetentionHours = 168;
  logsRetentionHours = 168;
  adminPassword = '';
  result = '';
  saveMessage = '';
  saveWarning = false;
  lastUpdatedKeys: string[] = [];
  requiresRestart = false;
  error = '';
  private savedDraft: SettingsDraft | null = null;

  constructor(private readonly api: ApiService, private readonly snackBar: MatSnackBar) {
    void this.load();
  }

  get accessedOverHttp(): boolean {
    return typeof location !== 'undefined' && location.protocol === 'http:';
  }

  get httpsMode(): string {
    if (!this.settings) {
      return this.currentBrowserIsHttps ? 'HTTPS' : 'loading';
    }
    if (this.nativeHttpsConfigured) {
      return 'native HTTPS';
    }
    if (this.publicURLUsesHttps) {
      return 'HTTPS via proxy';
    }
    if (this.currentBrowserIsHttps) {
      return 'HTTPS active';
    }
    return 'HTTP';
  }

  get httpsTone(): string {
    if (this.accessedOverHttp) {
      return this.settings ? 'danger' : 'warning';
    }
    if (!this.settings) {
      return this.currentBrowserIsHttps ? 'good' : 'warning';
    }
    if (this.nativeHttpsConfigured || this.publicURLUsesHttps) {
      return 'good';
    }
    return this.currentBrowserIsHttps ? 'warning' : 'danger';
  }

  get httpsDetail(): string {
    if (!this.settings) {
      return this.currentBrowserIsHttps ? 'Secure browser session active' : 'Checking server config';
    }
    if (this.accessedOverHttp) {
      return 'Browser sessions are not protected';
    }
    if (this.nativeHttpsConfigured) {
      return 'TLS terminates inside PoDorel';
    }
    if (this.publicURLUsesHttps) {
      return 'HTTPS is expected before requests reach PoDorel';
    }
    if (this.currentBrowserIsHttps) {
      return 'Secure session active; Public URL still says HTTP';
    }
    return 'Browser sessions are not protected';
  }

  get publicURL(): string {
    return this.settings?.server?.public_url || 'not configured';
  }

  get listenAddr(): string {
    return this.settings?.server?.listen_addr || 'not loaded';
  }

  get proxyModeLabel(): string {
    return this.settings?.server?.trusted_proxy_mode ? 'trusted proxy headers' : 'direct listener';
  }

  get runtimeMode(): string {
    return this.settings?.mode || 'loading';
  }

  get currentSessionLabel(): string {
    const user = this.api.currentUser();
    if (!user) {
      return 'not loaded';
    }
    if (user.session_type === 'agent_token') {
      return 'agent token';
    }
    if (user.session_type === 'passkey') {
      return 'passkey admin';
    }
    return user.username || user.session_type || 'admin';
  }

  get sessionTTLLabel(): string {
    return this.durationLabel(this.settings?.auth?.session_ttl);
  }

  get failedLoginWindowLabel(): string {
    return this.durationLabel(this.settings?.auth?.failed_login_window);
  }

  get failedLoginLimitLabel(): string {
    const value = this.settings?.auth?.failed_login_limit;
    return value ? `${value} attempts` : 'unknown';
  }

  get metricsRetentionLabel(): string {
    return this.hoursLabel(this.metricsRetentionHours);
  }

  get logsRetentionLabel(): string {
    return this.hoursLabel(this.logsRetentionHours);
  }

  get metricsSamplingLabel(): string {
    const live = this.durationLabel(this.settings?.metrics?.live_interval);
    const persist = this.durationLabel(this.settings?.metrics?.persist_interval);
    return `${live} live · ${persist} persisted`;
  }

  get logCapacityLabel(): string {
    return `${this.logPerPodLimitMB} MB per pod · ${this.logTotalLimitMB} MB total`;
  }

  get scannerLabel(): string {
    return this.settings?.security?.scanner || 'not configured';
  }

  get scanScheduleLabel(): string {
    if (!this.scheduledScans) {
      return 'manual scans';
    }
    return this.securitySchedule || 'scheduled';
  }

  get actionSummary(): string {
    const exec = this.execEnabled ? 'Exec on' : 'Exec off';
    const automation = this.automationEnabled ? 'Automation on' : 'Automation off';
    return `${exec} · ${automation}`;
  }

  get hasUnsavedChanges(): boolean {
    return this.savedDraft !== null && !this.draftsEqual(this.savedDraft, this.currentDraft());
  }

  get saveStateLabel(): string {
    if (this.settingsBusy) {
      return 'Loading settings';
    }
    return this.hasUnsavedChanges ? 'Unsaved changes' : 'Saved';
  }

  get saveStateDetail(): string {
    if (this.settingsBusy) {
      return 'Refreshing current values.';
    }
    if (this.hasUnsavedChanges) {
      return 'Save changes to apply the current values.';
    }
    return 'Current values match the loaded settings.';
  }

  get passkeyCount(): number {
    return this.safePasskeys.length;
  }

  get passkeyItems(): PasskeyCredential[] {
    return this.safePasskeys;
  }

  get passkeyTone(): string {
    if (!this.passkeyReady || this.agentScopedSession) {
      return 'warning';
    }
    return this.passkeyCount > 0 ? 'good' : 'neutral';
  }

  get hasSaveFeedback(): boolean {
    return this.saveMessage !== '' || this.requiresRestart || this.lastUpdatedKeys.length > 0;
  }

  get passkeyReady(): boolean {
    return passkeysSupported() && passkeySecureContext();
  }

  get passkeyWarning(): string {
    if (this.agentScopedSession) {
      return 'Agent-token sessions cannot manage passkeys.';
    }
    return passkeyUnavailableMessage();
  }

  get showPasskeyTrustHelp(): boolean {
    return true;
  }

  get passkeySetupTone(): string {
    if (this.agentScopedSession) {
      return 'warning';
    }
    if (!passkeySecureContext()) {
      return 'danger';
    }
    if (!passkeysSupported()) {
      return 'warning';
    }
    return 'good';
  }

  get passkeySetupTitle(): string {
    if (this.passkeyReady && !this.agentScopedSession) {
      return 'Passkey setup is ready';
    }
    return 'Trust PoDorel for passkeys';
  }

  get passkeySetupDetail(): string {
    if (this.agentScopedSession) {
      return 'Sign in as the admin user to manage passkeys.';
    }
    if (!passkeySecureContext()) {
      return 'This browser still does not trust the current HTTPS origin.';
    }
    if (!passkeysSupported()) {
      return 'This browser session does not expose the passkey API.';
    }
    return 'The browser reports a secure context and passkey support.';
  }

  get secureContextLabel(): string {
    return passkeySecureContext() ? 'trusted by browser' : 'not trusted yet';
  }

  get passkeySupportLabel(): string {
    return passkeysSupported() ? 'available' : 'unavailable';
  }

  get currentHTTPSURL(): string {
    if (typeof location === 'undefined') {
      return 'https://curly-hub.local:9095';
    }
    const url = new URL(location.href);
    url.protocol = 'https:';
    return url.href;
  }

  get firefoxCAImportURL(): string {
    return '/api/system/tls-ca?inline=1';
  }

  get passkeyStatus(): string {
    if (this.passkeyCount === 0) {
      return 'No passkeys registered';
    }
    if (this.passkeyCount === 1) {
      return '1 passkey registered';
    }
    return `${this.passkeyCount} passkeys registered`;
  }

  get canManagePasskeys(): boolean {
    return !this.passkeyBusy;
  }

  private get agentScopedSession(): boolean {
    return this.api.currentUser()?.session_type === 'agent_token';
  }

  private get safePasskeys(): PasskeyCredential[] {
    return Array.isArray(this.passkeys) ? this.passkeys : [];
  }

  private get currentBrowserIsHttps(): boolean {
    return typeof location !== 'undefined' && location.protocol === 'https:';
  }

  private get nativeHttpsConfigured(): boolean {
    return Boolean(this.settings?.server?.tls_cert_file && this.settings.server.tls_key_file);
  }

  private get publicURLUsesHttps(): boolean {
    return this.settings?.server?.public_url?.trim().toLowerCase().startsWith('https://') ?? false;
  }

  async load(showFeedback = false): Promise<void> {
    this.error = '';
    this.settingsBusy = true;
    try {
      const [settings] = await Promise.all([this.api.settings(), this.api.me()]);
      this.settings = settings;
      this.execEnabled = this.settings.actions?.exec_enabled ?? false;
      this.automationEnabled = this.settings.actions?.automation_enabled ?? false;
      this.scheduledScans = this.settings.security?.scheduled_scans_enabled ?? false;
      this.securitySchedule = this.settings.security?.schedule ?? 'daily';
      this.logTotalLimitMB = this.settings.logs?.total_limit_mb ?? this.logTotalLimitMB;
      this.logPerPodLimitMB = this.settings.logs?.per_pod_limit_mb ?? this.logPerPodLimitMB;
      this.metricsRetentionHours = this.hoursFromDuration(this.settings.metrics?.retention, this.metricsRetentionHours);
      this.logsRetentionHours = this.hoursFromDuration(this.settings.logs?.retention, this.logsRetentionHours);
      this.savedDraft = this.currentDraft();
    } catch (error) {
      this.error = this.formatError(error);
    }
    await this.loadPasskeys();
    this.settingsBusy = false;
    if (showFeedback && !this.error && !this.passkeyError) {
      this.snackBar.open('Settings refreshed.', 'Dismiss', { duration: 2500 });
    }
  }

  async loadPasskeys(): Promise<void> {
    this.passkeyError = '';
    try {
      const passkeys = await this.api.passkeys();
      this.passkeys = this.normalizePasskeys(passkeys);
    } catch (error) {
      this.passkeys = [];
      this.passkeyError = this.formatError(error);
    }
  }

  async registerPasskey(): Promise<void> {
    this.passkeyError = '';
    this.passkeyResult = '';
    this.caResult = '';
    try {
      if (!this.passkeyReady || this.agentScopedSession) {
        throw new Error(this.passkeyWarning || 'Passkeys are not available.');
      }
      this.passkeyBusy = true;
      const begin = await this.api.beginPasskeyRegistration(this.passkeyName);
      const credential = await navigator.credentials.create({ publicKey: toPublicKeyCreationOptions(begin.public_key) });
      if (!(credential instanceof PublicKeyCredential)) {
        throw new Error('Passkey registration was cancelled.');
      }
      const stored = await this.api.finishPasskeyRegistration(begin.flow_id, credentialToJSON(credential));
      this.passkeyName = this.defaultPasskeyName();
      this.passkeyResult = `Registered ${stored.name}.`;
      this.snackBar.open(this.passkeyResult, 'Dismiss', { duration: 3500 });
      await this.loadPasskeys();
    } catch (error) {
      this.passkeyError = this.formatError(error);
      this.snackBar.open(this.passkeyError, 'Dismiss', { duration: 6500 });
    } finally {
      this.passkeyBusy = false;
    }
  }

  async downloadLocalCA(): Promise<void> {
    this.caDownloadBusy = true;
    this.caResult = '';
    try {
      const blob = await this.api.downloadTLSCA();
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = 'podorel-local-ca.crt';
      link.rel = 'noopener';
      document.body.appendChild(link);
      link.click();
      link.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
      this.caResult = 'Local CA downloaded. Open it, trust it for websites, then reopen PoDorel over HTTPS.';
      this.snackBar.open(this.caResult, 'Dismiss', { duration: 6500 });
    } catch (error) {
      this.passkeyError = this.formatError(error);
      this.snackBar.open(this.passkeyError, 'Dismiss', { duration: 6500 });
    } finally {
      this.caDownloadBusy = false;
    }
  }

  async copyHTTPSURL(): Promise<void> {
    this.caResult = '';
    const url = this.currentHTTPSURL;
    try {
      if (!navigator.clipboard) {
        throw new Error('Clipboard unavailable.');
      }
      await navigator.clipboard.writeText(url);
      this.caResult = `Copied ${url}`;
    } catch {
      this.caResult = `Open ${url}`;
    }
    this.snackBar.open(this.caResult, 'Dismiss', { duration: 4500 });
  }

  async deletePasskey(passkey: PasskeyCredential): Promise<void> {
    this.passkeyBusy = true;
    this.passkeyError = '';
    this.passkeyResult = '';
    this.caResult = '';
    try {
      await this.api.deletePasskey(passkey.id);
      this.passkeyResult = `Removed ${passkey.name}.`;
      this.snackBar.open(this.passkeyResult, 'Dismiss', { duration: 3500 });
      await this.loadPasskeys();
    } catch (error) {
      this.passkeyError = this.formatError(error);
      this.snackBar.open(this.passkeyError, 'Dismiss', { duration: 6500 });
    } finally {
      this.passkeyBusy = false;
    }
  }

  async save(): Promise<void> {
    this.error = '';
    this.saveWarning = false;
    if (this.adminPassword.trim() === '') {
      this.saveMessage = 'Enter the admin password to save changes.';
      this.saveWarning = true;
      this.lastUpdatedKeys = [];
      this.requiresRestart = false;
      this.snackBar.open(this.saveMessage, 'Dismiss', { duration: 5000 });
      return;
    }
    this.saving = true;
    try {
      const payload = {
        password: this.adminPassword,
        actions: {
          exec_enabled: this.execEnabled,
          automation_enabled: this.automationEnabled
        },
        metrics: {
          retention_hours: this.metricsRetentionHours
        },
        logs: {
          retention_hours: this.logsRetentionHours,
          per_pod_limit_mb: this.logPerPodLimitMB,
          total_limit_mb: this.logTotalLimitMB
        },
        security: {
          scheduled_scans_enabled: this.scheduledScans,
          schedule: this.securitySchedule
        }
      };
      const response = await this.api.updateSettings(payload);
      this.result = JSON.stringify(response, null, 2);
      this.lastUpdatedKeys = this.updatedKeys(response);
      this.requiresRestart = response['requires_restart'] === true;
      this.saveMessage = this.lastUpdatedKeys.length ? `Saved ${this.lastUpdatedKeys.join(', ')}.` : 'Settings saved.';
      this.savedDraft = this.currentDraft();
      this.adminPassword = '';
      this.snackBar.open(this.saveMessage, 'Dismiss', { duration: 3500 });
    } catch (error) {
      this.error = this.formatError(error);
      this.saveMessage = '';
      this.snackBar.open(this.error, 'Dismiss', { duration: 7000 });
    } finally {
      this.saving = false;
    }
  }

  toggleExec(): void {
    this.execEnabled = !this.execEnabled;
  }

  toggleAutomation(): void {
    this.automationEnabled = !this.automationEnabled;
  }

  toggleScheduledScans(): void {
    this.scheduledScans = !this.scheduledScans;
  }

  passkeyNameLabel(passkey: PasskeyCredential): string {
    return this.nonEmptyText(passkey.name) || 'Unnamed passkey';
  }

  passkeyCreatedLabel(passkey: PasskeyCredential): string {
    return this.dateTimeLabel(passkey.created_at, 'Created date unknown');
  }

  passkeyLastUsedLabel(passkey: PasskeyCredential): string {
    return this.dateTimeLabel(passkey.last_used_at, 'Never used');
  }

  passkeyCredentialLabel(passkey: PasskeyCredential): string {
    const credential = this.nonEmptyText(passkey.credential_id);
    if (!credential) {
      return 'Credential ID unavailable';
    }
    return `Credential ${credential.slice(0, 12)}...`;
  }

  passkeyTitle(passkey: PasskeyCredential): string {
    const name = this.passkeyNameLabel(passkey);
    const credential = this.nonEmptyText(passkey.credential_id);
    return credential ? `${name} · ${credential}` : name;
  }

  private normalizePasskeys(value: unknown): PasskeyCredential[] {
    if (!Array.isArray(value)) {
      return [];
    }
    return value
      .filter((item): item is PasskeyCredential => this.isPasskeyCredential(item))
      .map((passkey) => ({
        ...passkey,
        name: this.nonEmptyText(passkey.name) || 'Unnamed passkey',
        last_used_at: this.validDateValue(passkey.last_used_at) ?? null
      }));
  }

  private isPasskeyCredential(value: unknown): value is PasskeyCredential {
    if (value === null || typeof value !== 'object') {
      return false;
    }
    const item = value as Partial<PasskeyCredential>;
    return typeof item.id === 'string'
      && typeof item.user_id === 'string'
      && typeof item.credential_id === 'string'
      && typeof item.created_at === 'string'
      && typeof item.updated_at === 'string';
  }

  private currentDraft(): SettingsDraft {
    return {
      execEnabled: this.execEnabled,
      automationEnabled: this.automationEnabled,
      scheduledScans: this.scheduledScans,
      securitySchedule: this.securitySchedule.trim(),
      metricsRetentionHours: this.numberValue(this.metricsRetentionHours),
      logsRetentionHours: this.numberValue(this.logsRetentionHours),
      logPerPodLimitMB: this.numberValue(this.logPerPodLimitMB),
      logTotalLimitMB: this.numberValue(this.logTotalLimitMB)
    };
  }

  private draftsEqual(left: SettingsDraft, right: SettingsDraft): boolean {
    return left.execEnabled === right.execEnabled
      && left.automationEnabled === right.automationEnabled
      && left.scheduledScans === right.scheduledScans
      && left.securitySchedule === right.securitySchedule
      && left.metricsRetentionHours === right.metricsRetentionHours
      && left.logsRetentionHours === right.logsRetentionHours
      && left.logPerPodLimitMB === right.logPerPodLimitMB
      && left.logTotalLimitMB === right.logTotalLimitMB;
  }

  private numberValue(value: number): number {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : 0;
  }

  private hoursFromDuration(value: number | undefined, fallback: number): number {
    if (!value) {
      return fallback;
    }
    return Math.round(value / 1000000000 / 60 / 60);
  }

  private durationLabel(value: number | undefined): string {
    if (!value) {
      return 'unknown';
    }
    const seconds = Math.round(value / 1000000000);
    if (seconds < 60) {
      return `${seconds}s`;
    }
    const minutes = Math.round(seconds / 60);
    if (minutes < 60) {
      return `${minutes}m`;
    }
    return this.hoursLabel(Math.round(minutes / 60));
  }

  private hoursLabel(hours: number): string {
    if (!Number.isFinite(hours) || hours <= 0) {
      return 'unknown';
    }
    if (hours % 24 === 0) {
      const days = hours / 24;
      return days === 1 ? '1 day' : `${days} days`;
    }
    return hours === 1 ? '1 hour' : `${hours} hours`;
  }

  private updatedKeys(response: Record<string, unknown>): string[] {
    const updated = response['updated'];
    if (!Array.isArray(updated)) {
      return [];
    }
    return updated.filter((value): value is string => typeof value === 'string' && value.trim() !== '');
  }

  private defaultPasskeyName(): string {
    if (typeof location !== 'undefined' && location.hostname) {
      return `PoDorel on ${location.hostname}`;
    }
    return 'PoDorel passkey';
  }

  private dateTimeLabel(value: string | null | undefined, fallback: string): string {
    const valid = this.validDateValue(value);
    if (!valid) {
      return fallback;
    }
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: 'medium',
      timeStyle: 'short'
    }).format(new Date(valid));
  }

  private validDateValue(value: string | null | undefined): string | null {
    const text = this.nonEmptyText(value);
    if (!text || text.startsWith('0001-01-01')) {
      return null;
    }
    const timestamp = Date.parse(text);
    return Number.isFinite(timestamp) ? text : null;
  }

  private nonEmptyText(value: unknown): string {
    return typeof value === 'string' ? value.trim() : '';
  }

  private formatError(error: unknown): string {
    if (error instanceof ApiError) {
      return `${error.message} Correlation ID: ${error.correlationId}`;
    }
    const passkeyError = formatPasskeyError(error, '');
    if (passkeyError) {
      return passkeyError;
    }
    if (error instanceof Error) {
      return error.message;
    }
    return 'Settings request failed.';
  }
}
