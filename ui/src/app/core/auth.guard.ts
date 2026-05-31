import { inject } from '@angular/core';
import { CanActivateFn, Router } from '@angular/router';
import { ApiService } from './api.service';

export const authGuard: CanActivateFn = async (_route, state) => {
  const api = inject(ApiService);
  const router = inject(Router);
  if (api.currentUser()) {
    return true;
  }
  try {
    await api.me();
    return true;
  } catch {
    return router.createUrlTree(['/login'], { queryParams: { returnUrl: state.url } });
  }
};
