/**
 * The one place that talks to the API.
 *
 * Every endpoint returns either a JSON body or the error envelope the Go side
 * defines in internal/httpapi/errors.go, so unwrapping both is done here once
 * rather than at every call site.
 */

export interface ApiErrorBody {
  code: string;
  message: string;
  fields?: Record<string, string>;
  /** The 1-based character a query went wrong at, for bad_query. */
  position?: number;
  requestId?: string;
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly fields: Record<string, string>;
  readonly position?: number;
  readonly requestId?: string;

  constructor(status: number, body: ApiErrorBody) {
    super(body.message);
    this.name = "ApiError";
    this.status = status;
    this.code = body.code;
    this.fields = body.fields ?? {};
    this.position = body.position;
    this.requestId = body.requestId;
  }

  /** True when the caller is not signed in, which the router treats specially. */
  get isUnauthenticated(): boolean {
    return this.status === 401;
  }
}

// Exported because a sign-in that leaves the application is a navigation the
// browser makes itself, not a request this module sends.
export const BASE = "/api/v1";

interface RequestOptions {
  method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  body?: unknown;
  signal?: AbortSignal;
}

export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { method = "GET", body, signal } = options;

  const response = await fetch(`${BASE}${path}`, {
    method,
    signal,
    // The session lives in an HttpOnly cookie, so it has to be sent explicitly.
    credentials: "same-origin",
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  return unwrap<T>(response);
}

/**
 * Sends a file. The browser sets the multipart boundary itself, which is why
 * this does not go through request: a Content-Type header set by hand would
 * be missing it.
 */
export async function upload<T>(path: string, form: FormData): Promise<T> {
  const response = await fetch(`${BASE}${path}`, {
    method: "POST",
    credentials: "same-origin",
    body: form,
  });
  return unwrap<T>(response);
}

async function unwrap<T>(response: Response): Promise<T> {
  if (response.status === 204) {
    return undefined as T;
  }

  const text = await response.text();
  let parsed: unknown = undefined;
  if (text) {
    try {
      parsed = JSON.parse(text);
    } catch {
      // A non-JSON body from a proxy or gateway is still a failure we must
      // report usefully rather than crashing on.
      if (!response.ok) {
        throw new ApiError(response.status, {
          code: "unexpected_response",
          message: `The server returned ${response.status}.`,
        });
      }
    }
  }

  if (!response.ok) {
    const envelope = (parsed as { error?: ApiErrorBody } | undefined)?.error;
    throw new ApiError(
      response.status,
      envelope ?? { code: "unexpected_response", message: `The server returned ${response.status}.` },
    );
  }

  return parsed as T;
}
