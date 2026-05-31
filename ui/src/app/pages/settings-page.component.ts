import { Component } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { JsonPipe } from '@angular/common';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatSlideToggleModule } from '@angular/material/slide-toggle';
import { ApiError, ApiService } from '../core/api.service';
import { AppSettings } from '../core/models';

@Component({
  selector: 'app-settings-page',
  standalone: true,
  imports: [FormsModule, JsonPipe, MatButtonModule, MatCardModule, MatChipsModule, MatFormFieldModule, MatIconModule, MatInputModule, MatSlideToggleModule],
  templateUrl: './settings-page.component.html',
  styleUrls: ['./settings-page.component.scss']
})
export class SettingsPageComponent {
  settings: AppSettings | null = null;
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
  error = '';

  constructor(private readonly api: ApiService) {
    void this.load();
  }

  get accessedOverHttp(): boolean {
    return typeof location !== 'undefined' && location.protocol !== 'https:';
  }

  async load(): Promise<void> {
    this.error = '';
    try {
      this.settings = await this.api.settings();
      this.execEnabled = this.settings.actions?.exec_enabled ?? false;
      this.automationEnabled = this.settings.actions?.automation_enabled ?? false;
      this.scheduledScans = this.settings.security?.scheduled_scans_enabled ?? false;
      this.securitySchedule = this.settings.security?.schedule ?? 'daily';
      this.logTotalLimitMB = this.settings.logs?.total_limit_mb ?? this.logTotalLimitMB;
      this.logPerPodLimitMB = this.settings.logs?.per_pod_limit_mb ?? this.logPerPodLimitMB;
      this.metricsRetentionHours = this.hoursFromDuration(this.settings.metrics?.retention, this.metricsRetentionHours);
      this.logsRetentionHours = this.hoursFromDuration(this.settings.logs?.retention, this.logsRetentionHours);
    } catch (error) {
      this.error = this.formatError(error);
    }
  }

  async save(): Promise<void> {
    this.error = '';
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
      this.result = JSON.stringify(await this.api.updateSettings(payload), null, 2);
    } catch (error) {
      this.error = this.formatError(error);
    }
  }

  private hoursFromDuration(value: number | undefined, fallback: number): number {
    if (!value) {
      return fallback;
    }
    return Math.round(value / 1000000000 / 60 / 60);
  }

  private formatError(error: unknown): string {
    if (error instanceof ApiError) {
      return `${error.message} Correlation ID: ${error.correlationId}`;
    }
    return 'Settings request failed.';
  }
}
