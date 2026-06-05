import { Component, OnInit, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { MatSnackBar, MatSnackBarModule } from '@angular/material/snack-bar';
import { ApiError, ApiService } from '../core/api.service';
import { ComposeStack, PodTemplate } from '../core/models';

type CatalogFilter = 'all' | 'pods' | 'compose';
type DraftMode = 'pod' | 'compose';

interface TemplateDraft {
  id: string;
  version: string;
  name: string;
  description: string;
  image: string;
  commandText: string;
  hostPort: string;
  containerPort: string;
  protocol: string;
  cpu: string;
  memory: string;
  restartPolicy: string;
  environmentText: string;
  labelsText: string;
  notesText: string;
}

interface ComposeDraft {
  id: string;
  version: string;
  name: string;
  description: string;
  presetId: string;
  composeYamlText: string;
  environmentFilesText: string;
  requiredFilesText: string;
  labelsText: string;
  notesText: string;
}

interface ComposePreset {
  id: string;
  icon: string;
  name: string;
  description: string;
  draft: ComposeDraft;
}

@Component({
  selector: 'app-templates-page',
  standalone: true,
  imports: [FormsModule, RouterLink, MatButtonModule, MatCardModule, MatChipsModule, MatFormFieldModule, MatIconModule, MatInputModule, MatSelectModule, MatSnackBarModule],
  templateUrl: './templates-page.component.html',
  styleUrls: ['./templates-page.component.scss']
})
export class TemplatesPageComponent implements OnInit {
  readonly templates = signal<PodTemplate[]>([]);
  readonly composeStacks = signal<ComposeStack[]>([]);
  readonly error = signal('');
  search = '';
  filter: CatalogFilter = 'all';
  draftMode: DraftMode = 'pod';
  draft: TemplateDraft = {
    id: 'custom-web',
    version: '1.0.0',
    name: 'Custom Web Service',
    description: 'HTTP service template with explicit CPU, memory, and port defaults.',
    image: 'docker.io/library/nginx:1.27-alpine',
    commandText: '',
    hostPort: '8080',
    containerPort: '80',
    protocol: 'tcp',
    cpu: '0.5',
    memory: '256MiB',
    restartPolicy: 'on-failure',
    environmentText: '',
    labelsText: 'io.podorel.template=custom-web',
    notesText: 'Review ports, memory, and restart policy before production use.'
  };
  composeDraft: ComposeDraft = {
    id: 'custom-compose-app',
    version: '1.0.0',
    name: 'Custom Compose App',
    description: 'Web and database Compose stack with explicit ports, restart policy, and persistent data.',
    presetId: 'web-db',
    composeYamlText: [
      'services:',
      '  web:',
      '    image: docker.io/library/nginx:1.27-alpine',
      '    container_name: podorel-custom-web',
      '    restart: unless-stopped',
      '    ports:',
      '      - "${WEB_PORT:-8080}:80"',
      '    depends_on:',
      '      - postgres',
      '',
      '  postgres:',
      '    image: docker.io/library/postgres:16-alpine',
      '    container_name: podorel-custom-postgres',
      '    restart: unless-stopped',
      '    environment:',
      '      POSTGRES_USER: ${POSTGRES_USER:-podorel}',
      '      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-podorel-dev-password}',
      '      POSTGRES_DB: ${POSTGRES_DB:-podorel}',
      '    ports:',
      '      - "${POSTGRES_PORT:-5432}:5432"',
      '    volumes:',
      '      - postgres-data:/var/lib/postgresql/data',
      '',
      'volumes:',
      '  postgres-data:'
    ].join('\n') + '\n',
    environmentFilesText: '.env.example',
    requiredFilesText: '',
    labelsText: 'io.podorel.compose.draft=true',
    notesText: 'Change the development database password before sharing the stack.'
  };
  readonly composePresets: ComposePreset[] = [
    {
      id: 'web',
      icon: 'public',
      name: 'Web',
      description: 'Single Nginx service with one published port.',
      draft: {
        id: 'custom-compose-web',
        version: '1.0.0',
        name: 'Custom Compose Web',
        description: 'Single web service Compose stack with explicit restart and port defaults.',
        presetId: 'web',
        composeYamlText: [
          'services:',
          '  web:',
          '    image: docker.io/library/nginx:1.27-alpine',
          '    container_name: podorel-custom-web',
          '    restart: unless-stopped',
          '    ports:',
          '      - "${WEB_PORT:-8080}:80"'
        ].join('\n') + '\n',
        environmentFilesText: '.env.example',
        requiredFilesText: '',
        labelsText: 'io.podorel.compose.draft=true',
        notesText: 'Set WEB_PORT in .env before deployment if 8080 is already used.'
      }
    },
    {
      id: 'web-db',
      icon: 'storage',
      name: 'Web + DB',
      description: 'Nginx, Postgres, and a persistent database volume.',
      draft: { ...this.composeDraft }
    },
    {
      id: 'api-redis',
      icon: 'hub',
      name: 'API + Redis',
      description: 'HTTP app placeholder with a Redis dependency.',
      draft: {
        id: 'custom-compose-api-redis',
        version: '1.0.0',
        name: 'Custom API and Redis',
        description: 'API service with a Redis cache dependency.',
        presetId: 'api-redis',
        composeYamlText: [
          'services:',
          '  api:',
          '    image: docker.io/library/nginx:1.27-alpine',
          '    container_name: podorel-custom-api',
          '    restart: unless-stopped',
          '    ports:',
          '      - "${API_PORT:-8081}:80"',
          '    environment:',
          '      REDIS_URL: redis://redis:6379',
          '    depends_on:',
          '      - redis',
          '',
          '  redis:',
          '    image: docker.io/library/redis:7-alpine',
          '    container_name: podorel-custom-redis',
          '    restart: unless-stopped',
          '    ports:',
          '      - "${REDIS_PORT:-6379}:6379"',
          '    command: ["redis-server", "--save", "", "--appendonly", "no"]'
        ].join('\n') + '\n',
        environmentFilesText: '.env.example',
        requiredFilesText: '',
        labelsText: 'io.podorel.compose.draft=true',
        notesText: 'Replace the API image with your application image before deployment.'
      }
    },
    {
      id: 'blank',
      icon: 'edit_note',
      name: 'Blank',
      description: 'Empty Compose shell for pasted or handwritten YAML.',
      draft: {
        id: 'custom-compose-stack',
        version: '1.0.0',
        name: 'Custom Compose Stack',
        description: 'Custom PoDorel Compose stack.',
        presetId: 'blank',
        composeYamlText: [
          'services:',
          '  app:',
          '    image: docker.io/library/nginx:1.27-alpine',
          '    restart: unless-stopped'
        ].join('\n') + '\n',
        environmentFilesText: '',
        requiredFilesText: '',
        labelsText: 'io.podorel.compose.draft=true',
        notesText: ''
      }
    }
  ];

  constructor(private readonly api: ApiService, private readonly snackBar: MatSnackBar) {}

  ngOnInit(): void {
    void this.refresh();
  }

  async refresh(): Promise<void> {
    this.error.set('');
    try {
      const [templates, composeStacks] = await Promise.all([this.api.templates(), this.api.composeStacks()]);
      this.templates.set(templates);
      this.composeStacks.set(composeStacks);
    } catch (error) {
      this.error.set(this.formatError(error));
    }
  }

  filteredTemplates(): PodTemplate[] {
    if (this.filter === 'compose') {
      return [];
    }
    const query = this.search.trim().toLowerCase();
    if (!query) {
      return this.templates();
    }
    return this.templates().filter((template) => `${template.id} ${template.name} ${template.description} ${template.image}`.toLowerCase().includes(query));
  }

  filteredComposeStacks(): ComposeStack[] {
    if (this.filter === 'pods') {
      return [];
    }
    const query = this.search.trim().toLowerCase();
    if (!query) {
      return this.composeStacks();
    }
    return this.composeStacks().filter((stack) => `${stack.id} ${stack.name} ${stack.description} ${stack.source_path} ${this.composeServiceLabel(stack)}`.toLowerCase().includes(query));
  }

  commandLabel(podTemplate: PodTemplate): string {
    const command = podTemplate.command?.length ? podTemplate.command.join(' ') : 'image default';
    return command.length > 74 ? `${command.slice(0, 71)}...` : command;
  }

  portLabel(podTemplate: PodTemplate): string {
    if (!podTemplate.ports.length) {
      return 'no ports';
    }
    return podTemplate.ports.map((port) => `${port.host || 'auto'}:${port.container}/${port.protocol}`).join(', ');
  }

  resourceLabel(podTemplate: PodTemplate): string {
    return `${podTemplate.resource_limits.cpu || 'CPU default'} / ${podTemplate.resource_limits.memory || 'memory default'}`;
  }

  secretLabel(podTemplate: PodTemplate): string {
    if (!podTemplate.secrets.length) {
      return 'no secrets';
    }
    const required = podTemplate.secrets.filter((secret) => secret.required).length;
    return `${podTemplate.secrets.length} secret${podTemplate.secrets.length === 1 ? '' : 's'} · ${required} required`;
  }

  composeServiceLabel(stack: ComposeStack): string {
    const services = stack.services.map((service) => service.name).join(', ');
    return services.length > 74 ? `${services.slice(0, 71)}...` : services || 'no services listed';
  }

  draftManifest(): string {
    return JSON.stringify(this.draftTemplate(), null, 2);
  }

  async copyDraftManifest(): Promise<void> {
    await this.copyText(this.draftManifest(), 'Template manifest copied');
  }

  downloadDraftManifest(): void {
    this.downloadText(`${this.safeId(this.draft.id)}.json`, this.draftManifest(), 'application/json');
  }

  composeManifest(): string {
    return JSON.stringify(this.composeStackDraft(), null, 2);
  }

  composeYaml(): string {
    return `${this.composeDraft.composeYamlText.trimEnd()}\n`;
  }

  composeFolderPath(): string {
    return `server/templates/compose/${this.safeIdWithFallback(this.composeDraft.id, 'custom-compose-stack')}`;
  }

  composeServices(): ComposeStack['services'] {
    return this.parseComposeServices(this.composeYaml());
  }

  composeSummaryLabel(): string {
    const services = this.composeServices();
    const ports = services.reduce((count, service) => count + (service.ports?.length ?? 0), 0);
    return `${services.length} service${services.length === 1 ? '' : 's'} · ${ports} port${ports === 1 ? '' : 's'}`;
  }

  composeStatusLabel(): string {
    if (!this.composeYaml().trim()) {
      return 'empty YAML';
    }
    if (!this.composeServices().length) {
      return 'no services parsed';
    }
    return 'manifest ready';
  }

  loadComposePreset(preset: ComposePreset): void {
    this.composeDraft = { ...preset.draft, presetId: preset.id };
  }

  async copyComposeManifest(): Promise<void> {
    await this.copyText(this.composeManifest(), 'Compose manifest copied');
  }

  async copyComposeYaml(): Promise<void> {
    await this.copyText(this.composeYaml(), 'Compose YAML copied');
  }

  downloadComposeManifest(): void {
    this.downloadText('podorel-compose.json', this.composeManifest(), 'application/json');
  }

  downloadComposeYaml(): void {
    this.downloadText('docker-compose.yml', this.composeYaml(), 'text/yaml');
  }

  private draftTemplate(): PodTemplate {
    const host = Number.parseInt(this.draft.hostPort, 10);
    const container = Number.parseInt(this.draft.containerPort, 10);
    const ports = Number.isFinite(container) && container > 0 ? [{
      host: Number.isFinite(host) && host >= 0 ? host : 0,
      container,
      protocol: this.draft.protocol || 'tcp'
    }] : [];
    return {
      id: this.safeId(this.draft.id),
      version: this.draft.version.trim() || '1.0.0',
      name: this.draft.name.trim() || 'Custom Template',
      description: this.draft.description.trim() || 'Custom PoDorel pod template.',
      image: this.draft.image.trim() || 'docker.io/library/alpine:3.20',
      command: this.lines(this.draft.commandText),
      ports,
      volumes: [],
      environment: this.keyValues(this.draft.environmentText),
      secrets: [],
      health_command: [],
      resource_limits: {
        cpu: this.draft.cpu.trim(),
        memory: this.draft.memory.trim()
      },
      restart_policy: this.draft.restartPolicy.trim() || 'on-failure',
      labels: this.keyValues(this.draft.labelsText),
      ui_notes: this.lines(this.draft.notesText)
    };
  }

  private composeStackDraft(): ComposeStack {
    const stackId = this.safeIdWithFallback(this.composeDraft.id, 'custom-compose-stack');
    const labels = this.keyValues(this.composeDraft.labelsText);
    if (!Object.keys(labels).length) {
      labels['io.podorel.compose.draft'] = 'true';
    }
    return {
      id: stackId,
      version: this.composeDraft.version.trim() || '1.0.0',
      name: this.composeDraft.name.trim() || 'Custom Compose Stack',
      description: this.composeDraft.description.trim() || 'Custom PoDorel compose stack.',
      source_path: this.composeFolderPath(),
      compose_files: ['docker-compose.yml'],
      services: this.composeServices(),
      environment_files: this.lines(this.composeDraft.environmentFilesText),
      required_files: this.lines(this.composeDraft.requiredFilesText),
      notes: this.lines(this.composeDraft.notesText),
      labels
    };
  }

  private parseComposeServices(yaml: string): ComposeStack['services'] {
    const lines = yaml.replace(/\t/g, '  ').split('\n');
    const servicesIndex = lines.findIndex((line) => /^\s*services\s*:\s*(?:#.*)?$/.test(line));
    if (servicesIndex < 0) {
      return [];
    }
    const servicesIndent = this.indentOf(lines[servicesIndex]);
    const serviceIndent = this.firstChildIndent(lines, servicesIndex + 1, servicesIndent);
    if (serviceIndent < 0) {
      return [];
    }

    const services: ComposeStack['services'] = [];
    let index = servicesIndex + 1;
    while (index < lines.length) {
      const raw = lines[index];
      const trimmed = raw.trim();
      if (!trimmed || trimmed.startsWith('#')) {
        index += 1;
        continue;
      }
      const indent = this.indentOf(raw);
      if (indent <= servicesIndent) {
        break;
      }
      const serviceMatch = indent === serviceIndent ? trimmed.match(/^([A-Za-z0-9_.-]+):\s*(?:#.*)?$/) : null;
      if (!serviceMatch) {
        index += 1;
        continue;
      }

      const block: string[] = [];
      index += 1;
      while (index < lines.length) {
        const blockRaw = lines[index];
        const blockTrimmed = blockRaw.trim();
        if (blockTrimmed && !blockTrimmed.startsWith('#')) {
          const blockIndent = this.indentOf(blockRaw);
          if (blockIndent <= servicesIndent || (blockIndent === serviceIndent && /^[A-Za-z0-9_.-]+:\s*(?:#.*)?$/.test(blockTrimmed))) {
            break;
          }
        }
        block.push(blockRaw);
        index += 1;
      }
      services.push(this.parseComposeServiceBlock(serviceMatch[1], block, serviceIndent));
    }
    return services;
  }

  private parseComposeServiceBlock(name: string, block: string[], serviceIndent: number): ComposeStack['services'][number] {
    const fieldIndent = this.firstChildIndent(block, 0, serviceIndent);
    const service: ComposeStack['services'][number] = { name };
    if (fieldIndent < 0) {
      return service;
    }

    const image = this.scalarField(block, fieldIndent, 'image');
    const build = this.scalarField(block, fieldIndent, 'build');
    const containerName = this.scalarField(block, fieldIndent, 'container_name');
    const restart = this.scalarField(block, fieldIndent, 'restart');
    const ports = this.listField(block, fieldIndent, 'ports');
    const profiles = this.listField(block, fieldIndent, 'profiles');

    if (image) {
      service.image = image;
    }
    if (build) {
      service.build = build;
    }
    if (containerName) {
      service.container_name = containerName;
    }
    if (restart) {
      service.restart = restart;
    }
    if (ports.length) {
      service.ports = ports;
    }
    if (profiles.length) {
      service.profiles = profiles;
    }
    return service;
  }

  private scalarField(block: string[], fieldIndent: number, fieldName: string): string {
    const prefix = `${fieldName}:`;
    const line = block.find((entry) => this.indentOf(entry) === fieldIndent && entry.trim().startsWith(prefix));
    if (!line) {
      return '';
    }
    const raw = line.trim().slice(prefix.length).trim();
    if (!raw && fieldName === 'build') {
      return '.';
    }
    return this.cleanYamlScalar(raw);
  }

  private listField(block: string[], fieldIndent: number, fieldName: string): string[] {
    const prefix = `${fieldName}:`;
    const start = block.findIndex((entry) => this.indentOf(entry) === fieldIndent && entry.trim().startsWith(prefix));
    if (start < 0) {
      return [];
    }

    const inline = this.cleanYamlList(block[start].trim().slice(prefix.length).trim());
    if (inline.length) {
      return inline;
    }

    const values: string[] = [];
    for (let index = start + 1; index < block.length; index += 1) {
      const line = block[index];
      const trimmed = line.trim();
      if (!trimmed || trimmed.startsWith('#')) {
        continue;
      }
      const indent = this.indentOf(line);
      if (indent <= fieldIndent) {
        break;
      }
      if (trimmed.startsWith('- ')) {
        const value = this.cleanYamlScalar(trimmed.slice(2));
        if (value) {
          values.push(value);
        }
      }
    }
    return values;
  }

  private cleanYamlList(value: string): string[] {
    const trimmed = value.trim();
    if (!trimmed.startsWith('[') || !trimmed.endsWith(']')) {
      return [];
    }
    return trimmed.slice(1, -1)
      .split(',')
      .map((item) => this.cleanYamlScalar(item))
      .filter((item) => item.length > 0);
  }

  private cleanYamlScalar(value: string): string {
    let cleaned = value.replace(/\s+#.*$/, '').trim();
    if ((cleaned.startsWith('"') && cleaned.endsWith('"')) || (cleaned.startsWith("'") && cleaned.endsWith("'"))) {
      cleaned = cleaned.slice(1, -1);
    }
    return cleaned;
  }

  private firstChildIndent(lines: string[], start: number, parentIndent: number): number {
    for (let index = start; index < lines.length; index += 1) {
      const trimmed = lines[index].trim();
      if (!trimmed || trimmed.startsWith('#')) {
        continue;
      }
      const indent = this.indentOf(lines[index]);
      if (indent <= parentIndent) {
        return -1;
      }
      return indent;
    }
    return -1;
  }

  private indentOf(line: string): number {
    return line.length - line.trimStart().length;
  }

  private async copyText(value: string, success: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(value);
      this.snackBar.open(success, 'Dismiss', { duration: 2500 });
    } catch {
      this.snackBar.open('Copy failed; select the text manually.', 'Dismiss', { duration: 4000 });
    }
  }

  private downloadText(filename: string, content: string, type: string): void {
    const blob = new Blob([content], { type });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = filename;
    anchor.click();
    URL.revokeObjectURL(url);
  }

  private keyValues(input: string): Record<string, string> {
    const values: Record<string, string> = {};
    for (const line of this.lines(input)) {
      const separator = line.indexOf('=');
      if (separator <= 0) {
        continue;
      }
      const key = line.slice(0, separator).trim();
      if (key) {
        values[key] = line.slice(separator + 1).trim();
      }
    }
    return values;
  }

  private lines(input: string): string[] {
    return input.split('\n').map((line) => line.trim()).filter((line) => line.length > 0);
  }

  private safeId(input: string): string {
    return this.safeIdWithFallback(input, 'custom-template');
  }

  private safeIdWithFallback(input: string, fallback: string): string {
    return input.toLowerCase().trim().replace(/[^a-z0-9_.-]+/g, '-').replace(/(^-|-$)/g, '') || fallback;
  }

  private formatError(error: unknown): string {
    if (error instanceof ApiError) {
      return `${error.message} Correlation ID: ${error.correlationId}`;
    }
    return 'Templates could not be loaded.';
  }
}
