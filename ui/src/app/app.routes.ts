import { Routes } from '@angular/router';
import { AgentsPageComponent } from './pages/agents-page.component';
import { AuditLogPageComponent } from './pages/audit-log-page.component';
import { ContainerDetailPageComponent } from './pages/container-detail-page.component';
import { CreatePodPageComponent } from './pages/create-pod-page.component';
import { DashboardPageComponent } from './pages/dashboard-page.component';
import { DiagnosticsPageComponent } from './pages/diagnostics-page.component';
import { LoginPageComponent } from './pages/login-page.component';
import { LogsPageComponent } from './pages/logs-page.component';
import { PodDetailPageComponent } from './pages/pod-detail-page.component';
import { PodsPageComponent } from './pages/pods-page.component';
import { SecurityUpdatesPageComponent } from './pages/security-updates-page.component';
import { SettingsPageComponent } from './pages/settings-page.component';
import { TemplatesPageComponent } from './pages/templates-page.component';
import { authGuard } from './core/auth.guard';

export const routes: Routes = [
  { path: '', pathMatch: 'full', redirectTo: 'login' },
  { path: 'login', component: LoginPageComponent },
  { path: 'dashboard', component: DashboardPageComponent, canActivate: [authGuard] },
  { path: 'pods', component: PodsPageComponent, canActivate: [authGuard] },
  { path: 'pods/:id', component: PodDetailPageComponent, canActivate: [authGuard] },
  { path: 'containers/:id', component: ContainerDetailPageComponent, canActivate: [authGuard] },
  { path: 'logs', component: LogsPageComponent, canActivate: [authGuard] },
  { path: 'security', component: SecurityUpdatesPageComponent, canActivate: [authGuard] },
  { path: 'create-pod', component: CreatePodPageComponent, canActivate: [authGuard] },
  { path: 'templates', component: TemplatesPageComponent, canActivate: [authGuard] },
  { path: 'settings', component: SettingsPageComponent, canActivate: [authGuard] },
  { path: 'audit', component: AuditLogPageComponent, canActivate: [authGuard] },
  { path: 'agents', component: AgentsPageComponent, canActivate: [authGuard] },
  { path: 'diagnostics', component: DiagnosticsPageComponent, canActivate: [authGuard] }
];
