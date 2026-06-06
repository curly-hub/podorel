const { expect, test } = require('@playwright/test');
const { installFixtureRoutes } = require('./fixtures');

const defaultAdminPassword = 'podorel-development-password';
const firstChangedAdminPassword = 'podorel-e2e-admin-password';
const settingsChangedAdminPassword = 'podorel-e2e-admin-password-settings';
let adminPassword = defaultAdminPassword;

test.describe.serial('PoDorel full UI click coverage', () => {
  test.beforeEach(async ({ context, page }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write']);
    await page.addInitScript(() => {
      try {
        delete window.PublicKeyCredential;
      } catch {
        // Keep passkey prompts out of deterministic HTTP E2E runs.
      }
      try {
        Object.defineProperty(Navigator.prototype, 'credentials', {
          configurable: true,
          get: () => undefined
        });
      } catch {
        // Browser did not allow overriding credentials; button assertions still guard this path.
      }
    });
    await installFixtureRoutes(page);
    attachBrowserErrorGuards(page);
  });

  test.afterEach(async ({ page }) => {
    const failures = page.__podorelBrowserFailures || [];
    expect(failures, `Browser errors:\n${failures.join('\n')}`).toEqual([]);
  });

  test('auth, app shell, navigation, and sign out', async ({ page }) => {
    await page.goto('/pods');
    await expect(page).toHaveURL(/\/login\?returnUrl=%2Fpods/);
    await expect(page.getByText('Use a passkey, admin password, or an agent-scoped token.')).toBeVisible();

    await expect(page.getByRole('button', { name: /Continue/i })).toBeDisabled();
    await page.getByRole('tab', { name: 'Agent Token' }).click();
    await page.getByRole('textbox', { name: 'Agent token' }).fill('not-a-token');

    await page.getByRole('tab', { name: 'Password' }).click();
    await page.getByLabel('Username').fill('admin');
    await page.getByRole('textbox', { name: 'Password' }).fill(adminPassword);
    await expect(page.getByRole('button', { name: /Sign In/i })).toBeEnabled();
    await page.getByRole('button', { name: /Sign In/i }).click();
    await expect(page).toHaveURL(/\/settings\?changePassword=1$/);
    await expect(page.getByText('Change admin password').first()).toBeVisible();
    await expect(page.getByLabel('Current admin password', { exact: true })).toBeVisible();
    await changeAdminPassword(page, adminPassword, firstChangedAdminPassword);
    await page.goto('/pods');
    await expect(page.getByRole('heading', { name: /Pods/ })).toBeVisible();

    await page.getByRole('button', { name: 'Toggle navigation' }).click();
    await expect(page.getByRole('link', { name: /Dashboard/ })).toBeHidden();
    await page.getByRole('button', { name: 'Toggle navigation' }).click();
    await expect(page.getByRole('link', { name: /Dashboard/ })).toBeVisible();

    const initialTheme = await page.locator('html').getAttribute('data-theme');
    await page.getByRole('button', { name: /Use .* theme/i }).click();
    await expect(page.locator('html')).not.toHaveAttribute('data-theme', initialTheme || '');

    await navigate(page, 'Dashboard', /\/dashboard$/);
    await navigate(page, 'Pods', /\/pods$/);
    await navigate(page, 'Logs', /\/logs$/);
    await navigate(page, 'Security', /\/security$/);
    await navigate(page, 'Create Pod', /\/create-pod$/);
    await navigate(page, 'Templates', /\/templates$/);
    await navigate(page, 'Agents', /\/agents$/);
    await navigate(page, 'Settings', /\/settings$/);
    await navigate(page, 'Audit', /\/audit$/);
    await navigate(page, 'Diagnostics', /\/diagnostics$/);

    await page.getByRole('button', { name: 'Settings' }).click();
    await expect(page).toHaveURL(/\/settings$/);
    await page.getByRole('button', { name: 'Diagnostics' }).click();
    await expect(page).toHaveURL(/\/diagnostics$/);

    await page.getByRole('button', { name: /Sign out/i }).click();
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.getByText('Use a passkey, admin password, or an agent-scoped token.')).toBeVisible();
  });

  test('dashboard, pods, pod detail, and container detail controls', async ({ page }) => {
    await login(page);

    await page.goto('/dashboard');
    await expect(page.getByRole('heading', { name: /Dashboard/ })).toBeVisible();
    await page.getByRole('button', { name: /Refresh/i }).click();
    await hoverHelp(page, 'Dashboard help');
    await hoverHelp(page, 'Pods help');
    await hoverHelp(page, 'CPU help');

    await page.goto('/pods');
    await expect(page.getByRole('heading', { name: /Pods/ })).toBeVisible();
    await page.getByRole('button', { name: /Refresh/i }).click();
    await page.getByLabel('Search pods').fill('e2e');
    await selectOption(page, 'State', /degraded/i);
    await expect(page.getByRole('link', { name: 'e2e-web' })).toBeVisible();

    await page.getByRole('button', { name: 'Create' }).click();
    await expect(page).toHaveURL(/\/create-pod$/);
    await page.goBack();
    await expect(page).toHaveURL(/\/pods$/);

    await page.getByRole('button', { name: 'Open shell' }).click();
    await expect(page).toHaveURL(/\/containers\/ctr-e2e\?tab=shell/);
    await expect(page.getByRole('tab', { name: 'Shell' })).toHaveAttribute('aria-selected', 'true');

    await page.goto('/pods');
    await page.getByRole('link', { name: 'e2e-web' }).click();
    await expect(page).toHaveURL(/\/pods\/pod-e2e$/);
    await expect(page.getByRole('heading', { name: 'e2e-web' }).first()).toBeVisible();
    await page.getByRole('button', { name: /Refresh/i }).click();
    await page.getByRole('button', { name: 'Shell', exact: true }).click();
    await expect(page).toHaveURL(/\/containers\/ctr-e2e\?tab=shell/);
    await page.goBack();
    await page
      .getByRole('article')
      .filter({ hasText: 'e2e-web-main' })
      .getByRole('button', { name: 'Open shell' })
      .click();
    await expect(page).toHaveURL(/\/containers\/ctr-e2e\?tab=shell/);
    await page.goBack();

    for (const tab of ['Overview', 'Stats', 'Logs', 'Security', 'Actions', 'Rules', 'Audit']) {
      await page.getByRole('tab', { name: tab }).click();
      await expect(page.getByRole('tab', { name: tab })).toHaveAttribute('aria-selected', 'true');
    }

    await page.getByRole('tab', { name: 'Overview' }).click();
    await page.locator('.container-actions').nth(1).locator('button').nth(1).click();
    await confirmOpenDialog(page, 'Start');
    await page.locator('.container-actions').first().locator('button').nth(2).click();
    await confirmOpenDialog(page, 'Stop');
    await page.locator('.container-actions').first().locator('button').nth(3).click();
    await confirmOpenDialog(page, 'Restart');
    await page.locator('.container-actions').first().locator('button').nth(4).click();
    await confirmOpenDialog(page, 'Kill', 'e2e-web-main');
    await page.locator('.container-actions').first().locator('button').nth(5).click();
    await confirmOpenDialog(page, 'Delete', 'e2e-web-main');

    await page.getByRole('tab', { name: 'Actions' }).click();
    await page.getByRole('button', { name: 'Stop', exact: true }).click();
    await confirmOpenDialog(page, 'Stop');
    await page.getByRole('button', { name: 'Restart', exact: true }).click();
    await confirmOpenDialog(page, 'Restart');
    await page.getByRole('button', { name: 'Kill', exact: true }).click();
    await confirmOpenDialog(page, 'Kill', 'e2e-web');
    await page.getByRole('button', { name: 'Delete', exact: true }).click();
    await confirmOpenDialog(page, 'Delete', 'e2e-web');

    await page.goto('/containers/ctr-e2e');
    await expect(page.getByRole('heading', { name: 'e2e-web-main' }).first()).toBeVisible();
    await page.getByRole('button', { name: /Refresh/i }).click();
    for (const tab of ['Metadata', 'Stats', 'Logs', 'Shell', 'Actions', 'Audit']) {
      await page.getByRole('tab', { name: tab }).click();
      await expect(page.getByRole('tab', { name: tab })).toHaveAttribute('aria-selected', 'true');
    }
    await page.getByRole('tab', { name: 'Shell' }).click();
    await selectOption(page, 'Shell', 'bash');
    await expect(page.getByRole('button', { name: /Open Shell/i })).toBeDisabled();
    await page.getByRole('button', { name: /Clear/i }).click();
    await expect(page.getByRole('button', { name: /Ctrl-C/i })).toBeDisabled();
    await expect(page.getByRole('button', { name: /^Close$/ })).toBeDisabled();

    await page.getByRole('tab', { name: 'Actions' }).click();
    await page.getByRole('button', { name: 'Stop', exact: true }).click();
    await confirmOpenDialog(page, 'Stop');
    await page.getByRole('button', { name: 'Restart', exact: true }).click();
    await confirmOpenDialog(page, 'Restart');
    await page.getByRole('button', { name: 'Kill', exact: true }).click();
    await confirmOpenDialog(page, 'Kill', 'e2e-web-main');
    await page.getByRole('button', { name: 'Delete', exact: true }).click();
    await confirmOpenDialog(page, 'Delete', 'e2e-web-main');
  });

  test('logs, security, audit, agents, settings, and diagnostics controls', async ({ page }) => {
    await login(page);

    await page.goto('/logs');
    await expect(page.getByRole('heading', { name: 'Logs' })).toBeVisible();
    await page.getByRole('button', { name: /Refresh/i }).first().click();
    await downloadFrom(page, page.getByRole('button', { name: /^Download$/ }));
    await page.getByLabel('Search text').fill('ready');
    await selectOption(page, 'Agent', /primary/);
    await selectOption(page, 'Pod', /e2e-web/);
    await selectOption(page, 'Container', /e2e-web-main/);
    await selectOption(page, 'Source', /e2e-web-main/);
    await page.getByLabel('Lines').fill('25');
    await page.getByRole('button', { name: 'Apply' }).click();
    await page.getByRole('button', { name: 'Clear log filters' }).click();
    await page.getByRole('button', { name: 'Start live' }).click();
    await page.waitForTimeout(500);
    if (await page.getByRole('button', { name: /Disconnect|Stop/ }).first().isVisible().catch(() => false)) {
      await page.getByRole('button', { name: /Disconnect|Stop/ }).first().click();
    }
    await page.getByRole('button', { name: /Pause logs|Resume logs/ }).first().click();
    await page.getByRole('button', { name: /Pause logs|Resume logs/ }).first().click();
    await page.getByRole('button', { name: 'Clear live buffer' }).first().click();
    await page.getByRole('tab', { name: 'Historical' }).click();
    await page.getByRole('button', { name: 'Refresh historical logs' }).click();
    await downloadFrom(page, page.getByRole('button', { name: 'Download historical logs' }));
    await page.getByRole('tab', { name: 'Live' }).click();
    await page.getByRole('button', { name: 'Connect live logs' }).click();
    await page.waitForTimeout(500);
    if (await page.getByRole('button', { name: 'Disconnect live logs' }).isVisible().catch(() => false)) {
      await page.getByRole('button', { name: 'Disconnect live logs' }).click();
    }

    await page.goto('/security');
    await expect(page.getByRole('heading', { name: /Security/ })).toBeVisible();
    await page.getByRole('button', { name: /Refresh/i }).click();
    await page.getByRole('button', { name: /Rescan/i }).click();
    await hoverHelp(page, 'Security help');
    await expect(page.getByText('CVE-2099-E2E')).toBeVisible();

    await page.goto('/audit');
    await expect(page.getByRole('heading', { name: 'Audit Log' })).toBeVisible();
    await page.getByRole('button', { name: /Refresh/i }).click();
    await page.getByLabel('Search').fill('pods.restart');
    await page.getByLabel('Limit').fill('10');
    await expect(page.getByText('corr-e2e-pod')).toBeVisible();

    await page.goto('/agents');
    await expect(page.getByRole('heading', { name: /Agents/ })).toBeVisible();
    await page.getByRole('button', { name: /Refresh/i }).click();
    await page.locator('.row-actions button[aria-label="Check health"]').first().click({ force: true });
    await page.locator('.row-actions button[aria-label="Rotate token"]').first().click({ force: true });
    await confirmOpenDialog(page, 'Rotate', 'primary');
    await expect(page.getByText('podorel-e2e-rotated-token')).toBeVisible();
    await page.getByRole('button', { name: 'Copy' }).click();
    await page.getByLabel('Agent ID').fill('e2e-extra');
    await page.getByLabel('Linux username').fill('alice');
    await page.getByLabel('Linux UID').fill('1001');
    await page.getByRole('textbox', { name: 'Socket path' }).fill('/tmp/podorel-ui-e2e/run/e2e-extra.sock');
    await page.getByRole('button', { name: 'Register' }).click();
    await expect(page.getByText('podorel-e2e-agent-token')).toBeVisible();

    await page.goto('/settings');
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible();
    await page.getByRole('button', { name: /Refresh|Refreshing/ }).click();
    await expect(page.getByText('No passkey is registered, so a distinct admin password is required.').first()).toBeVisible();
    await page.getByRole('button', { name: /Change password/i }).click();
    await expect(page.getByRole('complementary').getByText('Enter the current admin password.')).toBeVisible();
    await page.getByLabel('Current admin password', { exact: true }).fill(adminPassword);
    await page.getByLabel('New admin password', { exact: true }).fill('podorel-e2e-admin-password-mismatch');
    await page.getByLabel('Confirm new admin password', { exact: true }).fill('podorel-e2e-admin-password-other');
    await page.getByRole('button', { name: /Change password/i }).click();
    await expect(page.getByRole('complementary').getByText('New password and confirmation do not match.')).toBeVisible();
    await page.getByLabel('Current admin password', { exact: true }).fill('wrong-password');
    await page.getByLabel('New admin password', { exact: true }).fill('podorel-e2e-admin-password-rejected');
    await page.getByLabel('Confirm new admin password', { exact: true }).fill('podorel-e2e-admin-password-rejected');
    await page.getByRole('button', { name: /Change password/i }).click();
    await expect(page.getByRole('complementary').getByText('Current admin password is invalid.')).toBeVisible();
    await changeAdminPassword(page, adminPassword, settingsChangedAdminPassword);
    await page.getByRole('button', { name: 'Reload passkeys' }).click();
    await page.getByRole('button', { name: 'Remove passkey' }).click();
    await expect(page.locator('.passkeys-section .inline-message.good').getByText('Removed E2E passkey.')).toBeVisible();
    await expect(page.locator('.passkey-list').getByText('No passkeys registered.')).toBeVisible();
    await downloadFrom(page, page.getByRole('button', { name: /Download CA|Downloading/ }));
    await page.getByRole('button', { name: 'Copy HTTPS URL' }).click();
    await page.getByLabel('Passkey name').fill('E2E disabled passkey');
    await page.getByRole('button', { name: /Register/ }).click();
    await expect(page.locator('.passkeys-section .inline-message.danger').getByText('This browser does not support passkeys.')).toBeVisible();
    await page.getByRole('switch', { name: /Exec shell/ }).click();
    await page.getByRole('switch', { name: /Automation rules/ }).click();
    await page.getByRole('switch', { name: /Scheduled scans/ }).click();
    await page.getByLabel('Scan schedule').fill('hourly');
    await page.getByLabel('Metrics retention hours').fill('48');
    await page.getByLabel('Log retention hours').fill('72');
    await page.getByLabel('Log limit per pod in megabytes').fill('150');
    await page.getByLabel('Total log limit in megabytes').fill('6000');
    await page.getByRole('button', { name: /Save changes/i }).click();
    await expect(page.getByRole('complementary').getByText('Enter the admin password to save changes.')).toBeVisible();
    await page.getByRole('complementary').getByLabel('Admin password', { exact: true }).fill(adminPassword);
    await page.getByRole('button', { name: /Save changes/i }).click();
    await expect(page.getByRole('complementary').getByText(/Saved actions, metrics, logs, security/i)).toBeVisible();

    await page.goto('/diagnostics');
    await expect(page.getByRole('heading', { name: /Diagnostics/ })).toBeVisible();
    await page.getByRole('button', { name: /Refresh/i }).click();
    await page.getByRole('tab', { name: 'Runtime' }).click();
    await hoverHelp(page, 'Diagnostics help');
    await page.getByRole('tab', { name: 'Traces' }).click();
    await page.getByRole('textbox', { name: 'Correlation ID' }).fill('corr-e2e-pod');
    await page.getByRole('button', { name: 'Search' }).click();
    await expect(page.getByText('pods.refresh')).toBeVisible();
    await page.getByRole('tab', { name: 'Support' }).click();
    await downloadFrom(page, page.getByRole('button', { name: /Get Logs/i }));
    await page.getByLabel('Admin password confirmation').fill(adminPassword);
    await page.getByRole('button', { name: /Create Bundle/i }).click();
    await expect(page.getByText('bundle-e2e')).toBeVisible();
  });

  test('templates catalog and create-pod builder controls', async ({ page }) => {
    await login(page);

    await page.goto('/templates');
    await expect(page.getByRole('heading', { name: 'Templates', exact: true })).toBeVisible();
    const templatesReload = page.waitForResponse((response) => response.url().endsWith('/api/templates') && response.status() === 200);
    const stacksReload = page.waitForResponse((response) => response.url().endsWith('/api/compose-stacks') && response.status() === 200);
    await page.getByRole('button', { name: /Refresh/i }).click();
    await Promise.all([templatesReload, stacksReload]);
    await page.getByLabel('Search catalog').fill('redis');
    await selectOption(page, 'Type', 'Pod templates');
    await selectOption(page, 'Type', 'Compose stacks');
    await selectOption(page, 'Type', 'All');
    await page.getByLabel('Search catalog').clear();

    await page.locator('.catalog-main').getByRole('button', { name: 'Use', exact: true }).first().click();
    await expect(page).toHaveURL(/\/create-pod\?template=/);
    await expect(page.getByRole('heading', { name: 'Create Pod' })).toBeVisible();
    await page.goBack();
    await page.locator('.catalog-main').getByRole('button', { name: 'Deploy', exact: true }).first().click();
    await expect(page).toHaveURL(/\/create-pod\?mode=compose&stack=/);
    await page.goBack();
    await page.getByRole('button', { name: 'Create Pod', exact: true }).click();
    await expect(page).toHaveURL(/\/create-pod$/);
    await page.goBack();

    await page.locator('.draft-switch').getByRole('button', { name: 'Pod', exact: true }).click();
    await page.getByLabel('ID').fill('e2e-template');
    await page.getByLabel('Name').fill('E2E Template');
    await page.getByLabel('Description').fill('E2E manifest draft');
    await page.getByLabel('Image').fill('docker.io/library/alpine:3.20');
    await page.getByLabel('Host port').fill('18080');
    await page.getByLabel('Container port').fill('8080');
    await page.getByLabel('CPU').fill('0.25');
    await page.getByLabel('Memory').fill('128MiB');
    await selectOption(page, 'Restart policy', 'unless-stopped');
    await page.getByLabel('Command lines').fill('sleep\n3600');
    await page.getByLabel('Environment lines').fill('E2E=true');
    await page.getByLabel('Notes').fill('Generated by E2E.');
    await page.getByRole('button', { name: /Copy JSON/i }).click();
    await expect(page.getByText(/Template manifest copied|Copy failed/)).toBeVisible();
    await downloadFrom(page, page.getByRole('button', { name: /^Download$/ }));

    await page.locator('.draft-switch').getByRole('button', { name: 'Compose', exact: true }).click();
    for (const preset of ['Web', 'Web + DB', 'API + Redis', 'Blank']) {
      await page.locator('.preset-grid button').filter({
        has: page.locator('strong', { hasText: new RegExp(`^${escapeRegExp(preset)}$`) })
      }).click();
    }
    await page.getByLabel('Stack ID').fill('e2e-compose');
    await page.getByLabel('Name').fill('E2E Compose');
    await page.getByLabel('Version').fill('1.2.3');
    await page.getByLabel('Description').fill('E2E compose draft');
    await page.getByLabel('docker-compose.yml').fill('services:\n  app:\n    image: docker.io/library/nginx:1.27-alpine\n    ports:\n      - "18081:80"\n');
    await page.getByLabel('Env files').fill('.env.e2e');
    await page.getByLabel('Required files').fill('README.md');
    await page.getByLabel('Labels').fill('e2e=true');
    await page.getByLabel('Notes').fill('Compose E2E note.');
    await expect(page.getByText('1 service · 1 port')).toBeVisible();
    await page.getByRole('button', { name: /Copy manifest/i }).click();
    await expect(page.getByText(/Compose manifest copied|Copy failed/)).toBeVisible();
    await page.getByRole('button', { name: /Copy YAML/i }).click();
    await expect(page.getByText(/Compose YAML copied|Copy failed/)).toBeVisible();
    const composeDraftActions = page.locator('.draft-actions').last();
    await downloadFrom(page, composeDraftActions.getByRole('button', { name: 'Manifest', exact: true }));
    await downloadFrom(page, composeDraftActions.getByRole('button', { name: 'Compose', exact: true }));

    await page.goto('/create-pod');
    await expect(page.getByRole('heading', { name: 'Create Pod' })).toBeVisible();
    await page.getByRole('button', { name: /^Templates$/ }).first().click();
    await expect(page).toHaveURL(/\/templates$/);
    await page.goto('/create-pod');
    await expect(page.getByRole('heading', { name: 'Create Pod' })).toBeVisible();
    await page.getByRole('button', { name: /Refresh/i }).click();

    await clickCreateMode(page, 'Template');
    await selectOption(page, 'Template', /PostgreSQL|Redis|Node|Nginx|Alpine/i);
    await page.getByLabel('Pod name').fill('e2e-template-pod');
    await selectOption(page, 'Target agent', /primary/);
    await page.getByLabel('Find template').fill('redis');
    await page.locator('.template-option').first().click();
    const hostPortInput = page.locator('input[type="number"]').first();
    if (await hostPortInput.isVisible().catch(() => false)) {
      await hostPortInput.fill('16379');
    }
    await page.getByRole('button', { name: /^Add$/ }).click();
    await page.getByRole('textbox', { name: 'Key' }).last().fill('E2E_VALUE');
    await page.getByRole('textbox', { name: 'Value' }).last().fill('enabled');
    await page.getByRole('button', { name: 'Remove value' }).click();
    await page.getByRole('button', { name: /^Add$/ }).click();
    await page.getByRole('textbox', { name: 'Key' }).last().fill('E2E_VALUE');
    await page.getByRole('textbox', { name: 'Value' }).last().fill('enabled');
    await page.getByRole('button', { name: /^Preview$/ }).click();
    await expect(page.locator('.command-preview code').getByText(/podman run/)).toBeVisible();
    await page.getByRole('button', { name: /^Create Pod$/ }).click();
    await expect(page.locator('.result-box pre').getByText(/pod-e2e-created/)).toBeVisible();

    await clickCreateMode(page, 'Compose');
    await selectOption(page, 'Compose stack', /Redis|Postgres|Web|Compose/i);
    await selectOption(page, 'Target agent', /primary/);
    await page.getByLabel('Find stack').fill('redis');
    await page.locator('.stack-options .template-option').first().click();
    await page.getByLabel('Project name').fill('e2e-compose-project');
    await expect(page.getByLabel('Project name')).toHaveValue('e2e-compose-project');
    await page.getByRole('button', { name: /^Preview Stack$/ }).click();
    await expect(page.locator('.command-preview code').getByText(/podman-compose/)).toBeVisible();
    await page.getByRole('button', { name: /^Deploy Stack$/ }).click();
    await expect(page.locator('.result-box pre').getByText(/e2e-compose-project/)).toBeVisible();

    await clickCreateMode(page, 'Image');
    await page.getByLabel('Image name').fill('podorel-e2e-image:latest');
    await selectOption(page, 'Target agent', /primary/);
    await page.getByLabel('Dockerfile').fill('FROM scratch\nLABEL e2e=true\n');
    await page.getByRole('button', { name: /^Preview Build$/ }).click();
    await expect(page.locator('.command-preview code').getByText(/podman build/)).toBeVisible();
    await page.getByLabel('Password confirmation').fill(adminPassword);
    await page.getByRole('button', { name: /^Build Image$/ }).click();
    await expect(page.locator('.result-box pre').getByText(/build-e2e|queued/)).toBeVisible();

    await clickCreateMode(page, 'Secret');
    await page.getByLabel('Secret name').fill('podorel-e2e-secret-ui-all');
    await selectOption(page, 'Used by pod', /e2e-web|No pod scope/);
    await selectOption(page, 'Target agent', /primary/);
    await page.getByLabel('Password confirmation').fill(adminPassword);
    await page.getByLabel('Secret value').fill('e2e-secret-value');
    await page.getByRole('button', { name: /^Create Secret$/ }).click();
    await expect(page.locator('.result-box pre').getByText(/podorel-e2e-secret-ui-all/)).toBeVisible();
    await expect(page.getByLabel('Secret value')).toHaveValue('');
  });
});

async function login(page) {
  await page.goto('/login');
  await expect(page.getByText('Use a passkey, admin password, or an agent-scoped token.')).toBeVisible();
  await page.getByLabel('Username').fill('admin');
  await page.getByRole('textbox', { name: 'Password' }).fill(adminPassword);
  await page.getByRole('button', { name: /Sign In/i }).click();
  await expect(page).toHaveURL(/\/dashboard$|\/settings\?changePassword=1$/);
  if (/\/settings\?changePassword=1$/.test(page.url())) {
    await expect(page.getByText('Change admin password').first()).toBeVisible();
    await page.goto('/dashboard');
  }
  await expect(page).toHaveURL(/\/dashboard$/);
}

async function changeAdminPassword(page, currentPassword, newPassword) {
  await page.getByLabel('Current admin password', { exact: true }).fill(currentPassword);
  await page.getByLabel('New admin password', { exact: true }).fill(newPassword);
  await page.getByLabel('Confirm new admin password', { exact: true }).fill(newPassword);
  const responsePromise = page.waitForResponse((response) => response.url().endsWith('/api/auth/change-password') && response.status() === 200);
  await page.getByRole('button', { name: /Change password/i }).click();
  await responsePromise;
  await expect(page.getByRole('complementary').getByText('Admin password changed.')).toBeVisible();
  adminPassword = newPassword;
}

async function navigate(page, label, urlPattern) {
  await page.getByRole('link', { name: new RegExp(label) }).click();
  await expect(page).toHaveURL(urlPattern);
  await expect(page.getByRole('heading', { name: new RegExp(label === 'Audit' ? 'Audit Log' : label) })).toBeVisible();
}

async function selectOption(page, label, option) {
  const combo = page.getByRole('combobox', { name: label });
  await expect(combo).toBeVisible();
  let lastError;
  for (let attempt = 0; attempt < 3; attempt += 1) {
    try {
      await combo.click();
      await page.getByRole('option', { name: option }).first().click();
      return;
    } catch (error) {
      lastError = error;
      await page.keyboard.press('Escape').catch(() => {});
      await page.waitForTimeout(100);
    }
  }
  throw lastError;
}

async function clickCreateMode(page, mode) {
  await page.locator('.mode-rail button').filter({
    has: page.locator('strong', { hasText: new RegExp(`^${escapeRegExp(mode)}$`) })
  }).click();
}

async function confirmOpenDialog(page, label, expectedName = '') {
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  if (expectedName) {
    await dialog.getByLabel(new RegExp(`Type ${escapeRegExp(expectedName)}`)).fill(expectedName);
  }
  await dialog.getByRole('button', { name: new RegExp(`^${escapeRegExp(label)}(?:\\s+.+)?$`) }).click();
  await expect(dialog).toBeHidden();
}

async function downloadFrom(page, locator) {
  const downloadPromise = page.waitForEvent('download');
  await locator.click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toBeTruthy();
}

async function hoverHelp(page, label) {
  const target = page.getByLabel(label).first();
  if (await target.isVisible().catch(() => false)) {
    await target.hover();
    await page.waitForTimeout(100);
  }
}

function attachBrowserErrorGuards(page) {
  const failures = [];
  page.__podorelBrowserFailures = failures;
  page.on('pageerror', (error) => {
    failures.push(error.message);
  });
  page.on('console', (message) => {
    if (message.type() !== 'error') {
      return;
    }
    const text = message.text();
    if (/favicon|404|401 \(Unauthorized\)|400 \(Bad Request\)|403 \(Forbidden\)/.test(text)) {
      return;
    }
    failures.push(text);
  });
}

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
