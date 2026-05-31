import { Component, OnInit, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatChipsModule } from '@angular/material/chips';
import { MatIconModule } from '@angular/material/icon';
import { ApiError, ApiService } from '../core/api.service';
import { ComposeStack, PodTemplate } from '../core/models';

@Component({
  selector: 'app-templates-page',
  standalone: true,
  imports: [RouterLink, MatButtonModule, MatCardModule, MatChipsModule, MatIconModule],
  templateUrl: './templates-page.component.html',
  styleUrls: ['./templates-page.component.scss']
})
export class TemplatesPageComponent implements OnInit {
  readonly templates = signal<PodTemplate[]>([]);
  readonly composeStacks = signal<ComposeStack[]>([]);
  readonly error = signal('');

  constructor(private readonly api: ApiService) {}

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

  commandLabel(podTemplate: PodTemplate): string {
    const command = podTemplate.command?.length ? podTemplate.command.join(' ') : 'image default';
    return command.length > 64 ? `${command.slice(0, 61)}...` : command;
  }

  composeServiceLabel(stack: ComposeStack): string {
    const services = stack.services.map((service) => service.name).join(', ');
    return services.length > 64 ? `${services.slice(0, 61)}...` : services || 'no services listed';
  }

  private formatError(error: unknown): string {
    if (error instanceof ApiError) {
      return `${error.message} Correlation ID: ${error.correlationId}`;
    }
    return 'Templates could not be loaded.';
  }
}
