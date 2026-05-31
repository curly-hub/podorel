import { Component, Input } from '@angular/core';
import { MatIconModule } from '@angular/material/icon';
import { MatTooltipModule } from '@angular/material/tooltip';

@Component({
  selector: 'app-help-tooltip',
  standalone: true,
  imports: [MatIconModule, MatTooltipModule],
  templateUrl: './help-tooltip.component.html',
  styleUrls: ['./help-tooltip.component.scss']
})
export class HelpTooltipComponent {
  @Input() text = '';
  @Input() label = 'More information';
}
