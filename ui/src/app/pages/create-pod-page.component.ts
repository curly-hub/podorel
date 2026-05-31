import { Component } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatDividerModule } from '@angular/material/divider';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { MatStepperModule } from '@angular/material/stepper';
import { ApiError, ApiService } from '../core/api.service';
import { ComposeStack, PodTemplate } from '../core/models';

@Component({
  selector: 'app-create-pod-page',
  standalone: true,
  imports: [FormsModule, MatButtonModule, MatCardModule, MatChipsModule, MatDividerModule, MatFormFieldModule, MatIconModule, MatInputModule, MatSelectModule, MatStepperModule],
  templateUrl: './create-pod-page.component.html',
  styleUrls: ['./create-pod-page.component.scss']
})
export class CreatePodPageComponent {
  templates: PodTemplate[] = [];
  composeStacks: ComposeStack[] = [];
  templateId = 'alpine-nodejs';
  composeStackId = '';
  composeProjectName = '';
  podName = 'podorel-node';
  agentId = '';
  templateValuesText = '{}';
  imageName = 'podorel-import:latest';
  dockerfile = 'FROM alpine:3.20';
  buildPassword = '';
  secretName = '';
  secretValue = '';
  secretPassword = '';
  secretPodId = '';
  result = '';
  buildStreamStatus = '';
  private buildSocket?: WebSocket;
  error = '';

  constructor(private readonly api: ApiService) {
    void this.loadTemplates();
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

  async loadTemplates(): Promise<void> {
    try {
      const [templates, composeStacks] = await Promise.all([this.api.templates(), this.api.composeStacks()]);
      this.templates = templates;
      this.composeStacks = composeStacks;
      if (!this.templates.some((template) => template.id === this.templateId) && this.templates[0]) {
        this.templateId = this.templates[0].id;
      }
      if (!this.composeStacks.some((stack) => stack.id === this.composeStackId) && this.composeStacks[0]) {
        this.composeStackId = this.composeStacks[0].id;
        this.composeProjectName = this.composeStacks[0].id;
      }
    } catch (error) {
      this.error = this.formatError(error);
    }
  }

  async preview(): Promise<void> {
    await this.run(async () => this.api.createFromTemplate(this.templatePayload(false)));
  }

  async create(): Promise<void> {
    await this.run(async () => this.api.createFromTemplate(this.templatePayload(true)));
  }

  async composePreview(): Promise<void> {
    await this.run(async () => this.api.deployComposeStack(this.composePayload(false)));
  }

  async composeDeploy(): Promise<void> {
    await this.run(async () => this.api.deployComposeStack(this.composePayload(true)));
  }

  async buildPreview(): Promise<void> {
    await this.run(async () => this.api.buildDockerfile({ agent_id: this.agentId || undefined, image_name: this.imageName, dockerfile: this.dockerfile }));
  }

  async buildConfirm(): Promise<void> {
    await this.run(async () => {
      const response = await this.api.buildDockerfile({ agent_id: this.agentId || undefined, image_name: this.imageName, dockerfile: this.dockerfile, confirm: true, password: this.buildPassword });
      const buildId = typeof response['build_id'] === 'string' ? response['build_id'] : '';
      if (buildId) {
        this.connectBuildStream(buildId);
      }
      return response;
    });
  }

  async createSecret(): Promise<void> {
    await this.run(async () => this.api.createSecret({
      name: this.secretName,
      value: this.secretValue,
      password: this.secretPassword,
      used_by_pod_id: this.secretPodId || undefined,
      agent_id: this.agentId || undefined
    }));
    this.secretValue = '';
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
    const trimmed = this.templateValuesText.trim();
    if (!trimmed) {
      return {};
    }
    const parsed = JSON.parse(trimmed) as Record<string, unknown>;
    const values: Record<string, string> = {};
    for (const [key, value] of Object.entries(parsed)) {
      values[key] = String(value);
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

  private async run(work: () => Promise<Record<string, unknown>>): Promise<void> {
    this.error = '';
    try {
      this.result = JSON.stringify(await work(), null, 2);
    } catch (error) {
      this.error = this.formatError(error);
    }
  }

  private formatError(error: unknown): string {
    if (error instanceof ApiError) {
      return `${error.message} Correlation ID: ${error.correlationId}`;
    }
    return 'Create request failed.';
  }
}
