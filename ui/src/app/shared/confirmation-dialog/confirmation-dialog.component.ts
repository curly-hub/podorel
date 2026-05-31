import { Component, Inject } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MatButtonModule } from '@angular/material/button';
import { MAT_DIALOG_DATA, MatDialogModule, MatDialogRef } from '@angular/material/dialog';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatIconModule } from '@angular/material/icon';
import { MatInputModule } from '@angular/material/input';

export interface ConfirmationDialogData {
  title: string;
  message: string;
  confirmLabel: string;
  icon: string;
  warn?: boolean;
  expectedName?: string;
}

export interface ConfirmationDialogResult {
  confirmed: boolean;
  confirm_name?: string;
}

@Component({
  selector: 'app-confirmation-dialog',
  standalone: true,
  imports: [FormsModule, MatButtonModule, MatDialogModule, MatFormFieldModule, MatIconModule, MatInputModule],
  templateUrl: './confirmation-dialog.component.html',
  styleUrls: ['./confirmation-dialog.component.scss']
})
export class ConfirmationDialogComponent {
  typedName = '';

  constructor(
    @Inject(MAT_DIALOG_DATA) readonly data: ConfirmationDialogData,
    private readonly dialogRef: MatDialogRef<ConfirmationDialogComponent, ConfirmationDialogResult>
  ) {}

  get canConfirm(): boolean {
    return !this.data.expectedName || this.typedName === this.data.expectedName;
  }

  cancel(): void {
    this.dialogRef.close({ confirmed: false });
  }

  confirm(): void {
    if (!this.canConfirm) {
      return;
    }
    this.dialogRef.close({ confirmed: true, confirm_name: this.typedName || undefined });
  }
}
