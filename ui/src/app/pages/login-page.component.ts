import { Component, OnInit } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatTabsModule } from '@angular/material/tabs';
import { ApiError, ApiService } from '../core/api.service';

@Component({
  selector: 'app-login-page',
  standalone: true,
  imports: [FormsModule, MatButtonModule, MatCardModule, MatFormFieldModule, MatIconModule, MatInputModule, MatTabsModule],
  templateUrl: './login-page.component.html',
  styleUrls: ['./login-page.component.scss']
})
export class LoginPageComponent implements OnInit {
  username = 'admin';
  password = '';
  agentToken = '';
  error = '';
  busy = false;

  constructor(private readonly api: ApiService, private readonly router: Router, private readonly route: ActivatedRoute) {}

  get showDevelopmentHint(): boolean {
    return typeof location !== 'undefined' && (location.hostname === 'localhost' || location.hostname === '127.0.0.1');
  }

  ngOnInit(): void {
    void this.redirectIfAlreadyAuthenticated();
  }

  async login(): Promise<void> {
    await this.runLogin(() => this.api.login(this.username, this.password));
  }

  async loginAgent(): Promise<void> {
    await this.runLogin(() => this.api.loginWithAgentToken(this.agentToken));
  }

  private async runLogin(work: () => Promise<void>): Promise<void> {
    this.busy = true;
    this.error = '';
    try {
      await work();
      await this.router.navigateByUrl(this.destinationUrl());
    } catch (error) {
      this.error = this.formatError(error);
    } finally {
      this.busy = false;
    }
  }

  private async redirectIfAlreadyAuthenticated(): Promise<void> {
    try {
      await this.api.me();
      await this.router.navigateByUrl(this.destinationUrl());
    } catch {
      this.api.currentUser.set(null);
    }
  }

  private destinationUrl(): string {
    const returnUrl = this.route.snapshot.queryParamMap.get('returnUrl')?.trim() || '/dashboard';
    if (!returnUrl.startsWith('/') || returnUrl.startsWith('//') || returnUrl.startsWith('/login')) {
      return '/dashboard';
    }
    return returnUrl;
  }

  private formatError(error: unknown): string {
    if (error instanceof ApiError) {
      return `${error.message} Correlation ID: ${error.correlationId}`;
    }
    return 'Sign in failed. Correlation ID unavailable.';
  }
}
