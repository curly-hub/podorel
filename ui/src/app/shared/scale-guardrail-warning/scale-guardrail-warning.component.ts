import { Component, Input } from '@angular/core';
import { MatIconModule } from '@angular/material/icon';

@Component({
  selector: 'app-scale-guardrail-warning',
  standalone: true,
  imports: [MatIconModule],
  templateUrl: './scale-guardrail-warning.component.html',
  styleUrls: ['./scale-guardrail-warning.component.scss']
})
export class ScaleGuardrailWarningComponent {
  @Input() totalPods = 0;
  @Input() runningPods = 0;
  @Input() uncappedPods = 0;
  @Input() uncappedContainers = 0;

  get active(): boolean {
    return this.uncappedPods > 0 || this.uncappedContainers > 0;
  }

  get title(): string {
    return this.active ? 'Unbounded scale risk' : 'Scale guardrail';
  }

  get message(): string {
    if (!this.active) {
      return 'Pods should not scale indefinitely. Keep resource limits and replica counts explicit before enabling automation.';
    }
    const podLabel = this.uncappedPods === 1 ? 'pod has' : 'pods have';
    const containerLabel = this.uncappedContainers === 1 ? 'container' : 'containers';
    return `${this.uncappedPods} ${podLabel} ${this.uncappedContainers} sampled ${containerLabel} without app memory caps. Add limits before restart loops or automation increase load.`;
  }

  get icon(): string {
    return this.active ? 'warning' : 'rule';
  }
}
