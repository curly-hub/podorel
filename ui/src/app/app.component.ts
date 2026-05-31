import { Component, signal } from '@angular/core';
import { Router, RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { MatButtonModule } from '@angular/material/button';
import { MatIconModule } from '@angular/material/icon';
import { MatListModule } from '@angular/material/list';
import { MatSidenavModule } from '@angular/material/sidenav';
import { MatToolbarModule } from '@angular/material/toolbar';
import { MatTooltipModule } from '@angular/material/tooltip';
import { ApiService } from './core/api.service';

type AppTheme = 'light' | 'black';

const themeStorageKey = 'podorel-theme';

@Component({
  selector: 'app-root',
  standalone: true,
  imports: [RouterLink, RouterLinkActive, RouterOutlet, MatButtonModule, MatIconModule, MatListModule, MatSidenavModule, MatToolbarModule, MatTooltipModule],
  templateUrl: './app.component.html',
  styleUrls: ['./app.component.scss']
})
export class AppComponent {
  readonly navOpened = signal(true);
  readonly currentUser = this.api.currentUser;
  readonly navSections = [
    {
      label: 'Operate',
      items: [
        { path: '/dashboard', icon: 'dashboard', label: 'Dashboard' },
        { path: '/pods', icon: 'apps', label: 'Pods' },
        { path: '/logs', icon: 'article', label: 'Logs' },
        { path: '/security', icon: 'security', label: 'Security' }
      ]
    },
    {
      label: 'Build',
      items: [
        { path: '/create-pod', icon: 'add_box', label: 'Create Pod' },
        { path: '/templates', icon: 'view_module', label: 'Templates' }
      ]
    },
    {
      label: 'System',
      items: [
        { path: '/agents', icon: 'hub', label: 'Agents' },
        { path: '/settings', icon: 'settings', label: 'Settings' },
        { path: '/audit', icon: 'fact_check', label: 'Audit' },
        { path: '/diagnostics', icon: 'monitor_heart', label: 'Diagnostics' }
      ]
    }
  ];

  readonly theme = signal<AppTheme>(this.initialTheme());

  constructor(private readonly api: ApiService, private readonly router: Router) {
    this.applyTheme(this.theme());
  }

  toggleNav(): void {
    this.navOpened.update((opened) => !opened);
  }

  toggleTheme(): void {
    const next = this.theme() === 'black' ? 'light' : 'black';
    this.theme.set(next);
    this.applyTheme(next);
    this.saveTheme(next);
  }

  themeIcon(): string {
    return this.theme() === 'black' ? 'light_mode' : 'dark_mode';
  }

  themeTooltip(): string {
    return this.theme() === 'black' ? 'Use light theme' : 'Use black theme';
  }

  private initialTheme(): AppTheme {
    if (typeof window === 'undefined') {
      return 'light';
    }
    try {
      const stored = window.localStorage.getItem(themeStorageKey);
      if (stored === 'black' || stored === 'light') {
        return stored;
      }
      return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'black' : 'light';
    } catch {
      return 'light';
    }
  }

  private applyTheme(theme: AppTheme): void {
    if (typeof document === 'undefined') {
      return;
    }
    document.documentElement.dataset['theme'] = theme;
    document.body?.setAttribute('data-theme', theme);
  }

  private saveTheme(theme: AppTheme): void {
    if (typeof window === 'undefined') {
      return;
    }
    try {
      window.localStorage.setItem(themeStorageKey, theme);
    } catch {
      // localStorage may be disabled; the active document theme is still applied.
    }
  }

  async logout(): Promise<void> {
    try {
      await this.api.logout();
    } finally {
      await this.router.navigateByUrl('/login');
    }
  }
}
