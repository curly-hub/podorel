import { Component, OnInit, signal } from '@angular/core';
import { DatePipe, JsonPipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MatCardModule } from '@angular/material/card';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';
import { MatTableModule } from '@angular/material/table';
import { ApiError, ApiService } from '../core/api.service';
import { AuditEvent } from '../core/models';

@Component({
  selector: 'app-audit-log-page',
  standalone: true,
  imports: [DatePipe, FormsModule, JsonPipe, MatButtonModule, MatCardModule, MatFormFieldModule, MatIconModule, MatInputModule, MatTableModule],
  templateUrl: './audit-log-page.component.html',
  styleUrls: ['./audit-log-page.component.scss']
})
export class AuditLogPageComponent implements OnInit {
  readonly events = signal<AuditEvent[]>([]);
  readonly error = signal('');
  readonly columns = ['created_at', 'action', 'target', 'result', 'correlation_id', 'details'];
  search = '';
  limit = 100;

  constructor(private readonly api: ApiService) {}

  ngOnInit(): void {
    void this.refresh();
  }

  async refresh(): Promise<void> {
    this.error.set('');
    try {
      this.events.set(await this.api.audit(this.limit));
    } catch (error) {
      this.error.set(this.formatError(error));
    }
  }

  filtered(): AuditEvent[] {
    const query = this.search.trim().toLowerCase();
    if (!query) {
      return this.events();
    }
    return this.events().filter((event) => JSON.stringify(event).toLowerCase().includes(query));
  }

  private formatError(error: unknown): string {
    if (error instanceof ApiError) {
      return `${error.message} Correlation ID: ${error.correlationId}`;
    }
    return 'Audit events could not be loaded.';
  }
}
