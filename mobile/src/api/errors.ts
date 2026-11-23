export type ErrorKind = 'http' | 'network' | 'timeout' | 'cancelled' | 'invalid_response' | 'invalid_request';

export class ApiError extends Error {
  readonly kind: ErrorKind;
  readonly status?: number;
  readonly code?: string;
  readonly requestId?: string;
  readonly retryAfterMs?: number;
  constructor(kind: ErrorKind, message: string, details: { status?: number; code?: string; requestId?: string; retryAfterMs?: number } = {}) {
    super(message);
    this.name = 'ApiError';
    this.kind = kind;
    Object.assign(this, details);
  }
}
