import { readFileSync, existsSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

const root = new URL('..', import.meta.url).pathname;
const packageJson = JSON.parse(readFileSync(join(root, 'package.json'), 'utf8'));

const requiredDeps = ['@angular/core', '@angular/material', '@angular/cdk'];
for (const dep of requiredDeps) {
  const version = packageJson.dependencies?.[dep] ?? packageJson.devDependencies?.[dep];
  if (!version || !version.startsWith('^21.')) {
    throw new Error(`${dep} must be pinned to the Angular 21 stable line; found ${version ?? 'missing'}`);
  }
}
if (!packageJson.scripts?.start?.includes('--host localhost')) {
  throw new Error('Angular dev server must bind to localhost in development.');
}
if (packageJson.scripts?.start?.includes('0.0.0.0') || packageJson.scripts?.start?.includes('127.0.0.1')) {
  throw new Error('Angular dev server must not bind to 0.0.0.0 or 127.0.0.1 in development.');
}
const angularJson = readFileSync(join(root, 'angular.json'), 'utf8');
if (!angularJson.includes('"proxyConfig": "proxy.conf.json"')) {
  throw new Error('Angular dev server must proxy /api to the localhost web API.');
}
const proxyConfig = readFileSync(join(root, 'proxy.conf.json'), 'utf8');
if (!proxyConfig.includes('"target": "http://localhost:8080"') || !proxyConfig.includes('"/api"')) {
  throw new Error('Angular proxy.conf.json must route /api to http://localhost:8080.');
}
const indexHtml = readFileSync(join(root, 'src/index.html'), 'utf8');
if (!indexHtml.includes('Material+Icons') || !indexHtml.includes('Roboto')) {
  throw new Error('Angular index.html must load Roboto and Material Icons fonts.');
}
const globalStyles = readFileSync(join(root, 'src/styles.scss'), 'utf8');
if (!globalStyles.includes('@angular/material/prebuilt-themes')) {
  throw new Error('Global styles must import an Angular Material theme.');
}
const routesSource = readFileSync(join(root, 'src/app/app.routes.ts'), 'utf8');
if (!routesSource.includes("redirectTo: 'login'") || !routesSource.includes('authGuard')) {
  throw new Error('Protected routes must redirect unauthenticated users to login.');
}

const requiredPages = [
  'login-page.component.ts',
  'dashboard-page.component.ts',
  'pods-page.component.ts',
  'pod-detail-page.component.ts',
  'container-detail-page.component.ts',
  'logs-page.component.ts',
  'security-updates-page.component.ts',
  'create-pod-page.component.ts',
  'templates-page.component.ts',
  'settings-page.component.ts',
  'audit-log-page.component.ts',
  'agents-page.component.ts',
  'diagnostics-page.component.ts'
];

for (const page of requiredPages) {
  const path = join(root, 'src/app/pages', page);
  if (!existsSync(path)) {
    throw new Error(`Missing UI page scaffold: ${page}`);
  }
  const base = page.replace('.component.ts', '.component');
  for (const suffix of ['.html', '.scss']) {
    if (!existsSync(join(root, 'src/app/pages', `${base}${suffix}`))) {
      throw new Error(`Missing separated Angular file for ${page}: ${base}${suffix}`);
    }
  }
}

const componentFiles = recursiveFiles(join(root, 'src/app')).filter((file) => file.endsWith('.component.ts'));
for (const file of componentFiles) {
  const source = readFileSync(file, 'utf8');
  if (/\btemplate\s*:\s*['"`]/.test(source) || /\bstyles\s*:\s*\[/.test(source)) {
    throw new Error(`${file} must use templateUrl/styleUrls instead of inline template/styles.`);
  }
  if (!source.includes('templateUrl:') || !source.includes('styleUrls:')) {
    throw new Error(`${file} must declare separated templateUrl and styleUrls.`);
  }
}

const podsPage = readFileSync(join(root, 'src/app/pages/pods-page.component.ts'), 'utf8');
const podsPageHtml = readFileSync(join(root, 'src/app/pages/pods-page.component.html'), 'utf8');
if (!podsPage.includes('MatCardModule') || !podsPageHtml.includes('<mat-card')) {
  throw new Error('Pods default view must use Angular Material cards.');
}
if (!podsPageHtml.includes('security_update_warning') || !podsPageHtml.includes('apps')) {
  throw new Error('Pods page must include state and security/update icon signals.');
}
if (!podsPage.includes('MatDialog') || !podsPageHtml.includes('dangerous') || !podsPageHtml.includes('delete')) {
  throw new Error('Pods destructive actions must be backed by Material dialogs and visible action buttons.');
}

const appShell = readFileSync(join(root, 'src/app/app.component.ts'), 'utf8');
if (!appShell.includes('MatSidenavModule') || !appShell.includes('MatToolbarModule')) {
  throw new Error('Application shell must use Angular Material navigation and toolbar components.');
}

const apiService = readFileSync(join(root, 'src/app/core/api.service.ts'), 'utf8');
for (const path of ['/api/auth/login', '/api/pods', '/api/security/scan', '/api/secrets', '/api/pods/create-from-template']) {
  if (!apiService.includes(path)) {
    throw new Error(`API service is missing ${path}`);
  }
}

for (const page of ['security-updates-page.component.ts', 'templates-page.component.ts', 'create-pod-page.component.ts', 'audit-log-page.component.ts']) {
  const source = readFileSync(join(root, 'src/app/pages', page), 'utf8');
  if (!source.includes('ApiService')) {
    throw new Error(`${page} must call the backend API service.`);
  }
}

console.log('UI scaffold checks passed.');

function recursiveFiles(dir) {
  const entries = readdirSync(dir);
  return entries.flatMap((entry) => {
    const path = join(dir, entry);
    return statSync(path).isDirectory() ? recursiveFiles(path) : [path];
  });
}
