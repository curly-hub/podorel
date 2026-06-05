import { Component, OnInit } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { MatTooltipModule } from '@angular/material/tooltip';
import { ApiError, ApiService } from '../core/api.service';
import { Agent, ComposeStack, PodTemplate, PodView } from '../core/models';

type CreateMode = 'template' | 'compose' | 'image' | 'secret';

interface TemplateValueRow {
  key: string;
  value: string;
}

@Component({
  selector: 'app-create-pod-page',
  standalone: true,
  imports: [FormsModule, RouterLink, MatButtonModule, MatCardModule, MatChipsModule, MatFormFieldModule, MatIconModule, MatInputModule, MatSelectModule, MatTooltipModule],
  templateUrl: './create-pod-page.component.html',
  styleUrls: ['./create-pod-page.component.scss']
})
export class CreatePodPageComponent implements OnInit {
  templates: PodTemplate[] = [];
  composeStacks: ComposeStack[] = [];
  agents: Agent[] = [];
  pods: PodView[] = [];
  mode: CreateMode = 'template';
  templateId = 'alpine-nodejs';
  templateSearch = '';
  composeStackId = '';
  composeProjectName = '';
  composeSearch = '';
  podName = 'podorel-node';
  agentId = '';
  templatePortValues: Record<string, string> = {};
  templateVariableValues: Record<string, string> = {};
  templateValueRows: TemplateValueRow[] = [];
  imageName = 'podorel-import:latest';
  dockerfile = 'FROM alpine:3.20';
  buildPassword = '';
  secretName = '';
  secretValue = '';
  secretPassword = '';
  secretPodId = '';
  result = '';
  resultTitle = 'No action yet';
  buildStreamStatus = '';
  executing = false;
  error = '';
  private buildSocket?: WebSocket;

  constructor(private readonly api: ApiService, private readonly route: ActivatedRoute) {}

  async ngOnInit(): Promise<void> {
    await this.loadTemplates();
    this.applyRouteState();
  }

  get selectedTemplate(): PodTemplate | undefined {
    return this.templates.find((template) => template.id === this.templateId);
  }

  get selectedComposeStack(): ComposeStack | undefined {
    return this.composeStacks.find((stack) => stack.id === this.composeStackId);
  }

  get accessedOverHttp(): boolean {
    return typeof location !== 'undefined' && location.protocol !== 'https:';
  }

  get modeTitle(): string {
    switch (this.mode) {
      case 'compose':
        return 'Deploy compose stack';
      case 'image':
        return 'Build image';
      case 'secret':
        return 'Create secret';
      default:
        return 'Deploy from template';
    }
  }

  async loadTemplates(): Promise<void> {
    this.error = '';
    try {
      const [templates, composeStacks, agents, pods] = await Promise.all([
        this.api.templates(),
        this.api.composeStacks(),
        this.api.agents().catch(() => []),
        this.api.pods().catch(() => [])
      ]);
      this.templates = templates;
      this.composeStacks = composeStacks;
      this.agents = agents;
      this.pods = pods;
      if (!this.templates.some((template) => template.id === this.templateId) && this.templates[0]) {
        this.selectTemplate(this.templates[0].id, true);
      } else {
        this.syncTemplateDefaults();
      }
      if (!this.composeStacks.some((stack) => stack.id === this.composeStackId) && this.composeStacks[0]) {
        this.selectComposeStack(this.composeStacks[0].id);
      }
    } catch (error) {
      this.error = this.formatError(error);
    }
  }

  selectMode(mode: CreateMode): void {
    this.mode = mode;
  }

  onAgentChanged(): void {
    if (this.secretPodId && !this.secretPodOptions().some((pod) => pod.id === this.secretPodId)) {
      this.secretPodId = '';
    }
  }

  selectTemplate(templateId: string, resetName = false): void {
    this.templateId = templateId;
    this.templatePortValues = {};
    this.templateVariableValues = {};
    this.syncTemplateDefaults(resetName);
  }

  selectComposeStack(stackId: string): void {
    this.composeStackId = stackId;
    const stack = this.selectedComposeStack;
    this.composeProjectName = stack ? this.safeName(stack.id) : stackId;
  }

  filteredTemplates(): PodTemplate[] {
    const query = this.templateSearch.trim().toLowerCase();
    if (!query) {
      return this.templates;
    }
    return this.templates.filter((template) => `${template.name} ${template.id} ${template.description} ${template.image}`.toLowerCase().includes(query));
  }

  filteredComposeStacks(): ComposeStack[] {
    const query = this.composeSearch.trim().toLowerCase();
    if (!query) {
      return this.composeStacks;
    }
    return this.composeStacks.filter((stack) => `${stack.name} ${stack.id} ${stack.description} ${stack.source_path}`.toLowerCase().includes(query));
  }

  templateVariables(): string[] {
    const template = this.selectedTemplate;
    if (!template) {
      return [];
    }
    const values = [
      template.image,
      ...template.command,
      ...Object.values(template.environment ?? {}),
      ...Object.values(template.labels ?? {})
    ];
    const found = new Set<string>();
    const pattern = /\$\{([A-Za-z0-9_.-]+)\}|\{\{([A-Za-z0-9_.-]+)\}\}/g;
    for (const value of values) {
      let match: RegExpExecArray | null;
      while ((match = pattern.exec(value)) !== null) {
        found.add(match[1] || match[2]);
      }
    }
    return [...found].sort();
  }

  templateValueCount(): number {
    return Object.keys(this.templateValues()).length;
  }

  templatePortKey(port: PodTemplate['ports'][number]): string {
    return `host_port_${port.container}`;
  }

  templatePortValue(port: PodTemplate['ports'][number]): string {
    return this.templatePortValues[this.templatePortKey(port)] ?? String(port.host || '');
  }

  setTemplatePortValue(port: PodTemplate['ports'][number], value: string): void {
    this.templatePortValues[this.templatePortKey(port)] = value;
  }

  templateVariableValue(key: string): string {
    return this.templateVariableValues[key] ?? '';
  }

  setTemplateVariableValue(key: string, value: string): void {
    this.templateVariableValues[key] = value;
  }

  addTemplateValueRow(): void {
    this.templateValueRows = [...this.templateValueRows, { key: '', value: '' }];
  }

  removeTemplateValueRow(index: number): void {
    this.templateValueRows = this.templateValueRows.filter((_, rowIndex) => rowIndex !== index);
  }

  commandLabel(command: string[] | undefined): string {
    if (!command?.length) {
      return 'image default';
    }
    const joined = command.join(' ');
    return joined.length > 86 ? `${joined.slice(0, 83)}...` : joined;
  }

  portsLabel(template: PodTemplate | undefined): string {
    if (!template?.ports?.length) {
      return 'no ports';
    }
    return template.ports.map((port) => `${port.host || 'auto'}:${port.container}/${port.protocol}`).join(', ');
  }

  composeServiceLabel(stack: ComposeStack | undefined): string {
    const services = stack?.services?.map((service) => service.name).join(', ') ?? '';
    return services || 'no services listed';
  }

  previewCommand(): string[] {
    const parsed = this.resultObject();
    const command = parsed['preview_command'];
    return Array.isArray(command) ? command.map((part) => String(part)) : [];
  }

  resultObject(): Record<string, unknown> {
    if (!this.result) {
      return {};
    }
    try {
      return JSON.parse(this.result) as Record<string, unknown>;
    } catch {
      return {};
    }
  }

  resultStatus(): string {
    const parsed = this.resultObject();
    if (parsed['pod_id']) {
      return `pod ${parsed['pod_id']}`;
    }
    if (parsed['build_id']) {
      return `build ${parsed['build_id']}`;
    }
    if (parsed['requires_password']) {
      return 'password required';
    }
    if (this.previewCommand().length > 0) {
      return 'preview ready';
    }
    return 'waiting';
  }

  defaultAgentLabel(): string {
    const sessionAgent = this.api.currentUser()?.agent_id;
    return `Default agent (${sessionAgent || 'primary'})`;
  }

  agentOptionLabel(agent: Agent): string {
    const owner = agent.linux_username ? ` · ${agent.linux_username}` : '';
    const status = agent.status ? ` · ${agent.status}` : '';
    return `${agent.id}${owner}${status}`;
  }

  selectedAgentLabel(): string {
    const agentId = this.agentId.trim();
    if (!agentId) {
      return this.defaultAgentLabel();
    }
    const agent = this.agents.find((item) => item.id === agentId);
    return agent ? this.agentOptionLabel(agent) : agentId;
  }

  agentHint(): string {
    return this.agentId.trim()
      ? 'Runs on the selected registered PoDorel agent.'
      : 'Uses the default agent for this session.';
  }

  secretPodOptions(): PodView[] {
    const agent = this.agentId.trim();
    return this.pods
      .filter((pod) => !agent || pod.agent_id === agent)
      .sort((left, right) => this.podOptionLabel(left).localeCompare(this.podOptionLabel(right)));
  }

  podOptionLabel(pod: PodView): string {
    const identity = pod.name && pod.name !== pod.id ? `${pod.name} · ${pod.id}` : pod.id;
    return `${identity} · ${pod.state || 'unknown'}`;
  }

  emptyResultTitle(): string {
    if (this.executing) {
      return 'Running action';
    }
    if (this.error) {
      return 'Action failed';
    }
    return 'No output yet';
  }

  emptyResultMessage(): string {
    if (this.executing) {
      return 'Waiting for the API response.';
    }
    if (this.error) {
      return 'Check the error message above, then adjust the request and run it again.';
    }
    switch (this.mode) {
      case 'compose':
        return 'Preview or deploy a compose stack to see the Podman command and response.';
      case 'image':
        return 'Preview or build an image to see the build response and stream status.';
      case 'secret':
        return 'Create a secret to see the registered secret response.';
      default:
        return 'Preview or create from a template to see the generated request and response.';
    }
  }

  async preview(): Promise<void> {
    await this.run('Template preview', async () => this.api.createFromTemplate(this.templatePayload(false)));
  }

  async create(): Promise<void> {
    await this.run('Template create', async () => this.api.createFromTemplate(this.templatePayload(true)));
  }

  async composePreview(): Promise<void> {
    await this.run('Compose preview', async () => this.api.deployComposeStack(this.composePayload(false)));
  }

  async composeDeploy(): Promise<void> {
    await this.run('Compose deploy', async () => this.api.deployComposeStack(this.composePayload(true)));
  }

  async buildPreview(): Promise<void> {
    await this.run('Image build preview', async () => this.api.buildDockerfile({ agent_id: this.agentId || undefined, image_name: this.imageName, dockerfile: this.dockerfile }));
  }

  async buildConfirm(): Promise<void> {
    await this.run('Image build', async () => {
      const response = await this.api.buildDockerfile({ agent_id: this.agentId || undefined, image_name: this.imageName, dockerfile: this.dockerfile, confirm: true, password: this.buildPassword });
      const buildId = typeof response['build_id'] === 'string' ? response['build_id'] : '';
      if (buildId) {
        this.connectBuildStream(buildId);
      }
      return response;
    });
  }

  async createSecret(): Promise<void> {
    await this.run('Secret create', async () => this.api.createSecret({
      name: this.secretName,
      value: this.secretValue,
      password: this.secretPassword,
      used_by_pod_id: this.secretPodId || undefined,
      agent_id: this.agentId || undefined
    }));
    this.secretValue = '';
  }

  private applyRouteState(): void {
    const params = this.route.snapshot.queryParamMap;
    const mode = params.get('mode') as CreateMode | null;
    if (mode && ['template', 'compose', 'image', 'secret'].includes(mode)) {
      this.mode = mode;
    }
    const template = params.get('template');
    if (template && this.templates.some((item) => item.id === template)) {
      this.selectTemplate(template, true);
      this.mode = 'template';
    }
    const stack = params.get('stack');
    if (stack && this.composeStacks.some((item) => item.id === stack)) {
      this.selectComposeStack(stack);
      this.mode = 'compose';
    }
  }

  private syncTemplateDefaults(resetName = false): void {
    const template = this.selectedTemplate;
    if (!template) {
      return;
    }
    if (resetName || !this.podName || this.podName === 'podorel-node') {
      this.podName = this.safeName(template.id);
    }
    for (const port of template.ports ?? []) {
      this.templatePortValues[this.templatePortKey(port)] = String(port.host || '');
    }
  }

  private templatePayload(confirm: boolean): Record<string, unknown> {
    return {
      agent_id: this.agentId || undefined,
      template_id: this.templateId,
      pod_name: this.podName,
      values: this.templateValues(),
      confirm
    };
  }

  private composePayload(confirm: boolean): Record<string, unknown> {
    return {
      agent_id: this.agentId || undefined,
      stack_id: this.composeStackId,
      project_name: this.composeProjectName || this.composeStackId,
      confirm
    };
  }

  private templateValues(): Record<string, string> {
    const values: Record<string, string> = {};
    for (const port of this.selectedTemplate?.ports ?? []) {
      const key = this.templatePortKey(port);
      const value = (this.templatePortValues[key] ?? '').trim();
      if (value) {
        values[key] = value;
      }
    }
    for (const key of this.templateVariables()) {
      const value = (this.templateVariableValues[key] ?? '').trim();
      if (value) {
        values[key] = value;
      }
    }
    for (const row of this.templateValueRows) {
      const key = row.key.trim();
      if (key) {
        values[key] = row.value.trim();
      }
    }
    return values;
  }

  private connectBuildStream(buildId: string): void {
    this.buildSocket?.close();
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const socket = new WebSocket(`${protocol}//${location.host}/api/ws/builds?build_id=${encodeURIComponent(buildId)}`);
    this.buildSocket = socket;
    this.buildStreamStatus = `Build stream connected for ${buildId}`;
    socket.onmessage = (event) => {
      const payload = JSON.parse(event.data) as Record<string, unknown>;
      this.result = JSON.stringify(payload['build'] ?? payload, null, 2);
    };
    socket.onerror = () => {
      this.buildStreamStatus = `Build stream failed for ${buildId}`;
    };
    socket.onclose = () => {
      this.buildStreamStatus = `Build stream closed for ${buildId}`;
    };
  }

  private async run(title: string, work: () => Promise<Record<string, unknown>>): Promise<void> {
    this.error = '';
    this.executing = true;
    this.resultTitle = title;
    this.result = '';
    try {
      this.result = JSON.stringify(await work(), null, 2);
    } catch (error) {
      this.error = this.formatError(error);
    } finally {
      this.executing = false;
    }
  }

  private safeName(input: string): string {
    const value = input.toLowerCase().trim().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '');
    return value || 'pod';
  }

  private formatError(error: unknown): string {
    if (error instanceof ApiError) {
      return `${error.message} Correlation ID: ${error.correlationId}`;
    }
    return 'Create request failed.';
  }
}
