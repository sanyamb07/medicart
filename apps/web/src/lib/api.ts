// Thin fetch wrapper around the MediCart API.
//
// Every call sends credentials (cookies) so the guest-cart cookie and, for
// admin pages, the staff session cookie are included automatically. Errors
// from the API's { code, message, fields } shape are thrown as ApiError so
// callers can branch on `code` (e.g. "prescription_required") instead of
// string-matching the message.
const BASE_URL = import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";

// The storefront a shopper is currently browsing. Production would resolve
// this from the Host header (business.medicart.com); locally there is one
// host, so every storefront call instead carries this header — see
// services/api/internal/tenancy.ResolveStorefrontBusiness. null means "the
// original default storefront," sent as no header at all.
let currentBusinessSlug: string | null = null;
export function setBusinessSlug(slug: string | null) {
  currentBusinessSlug = slug;
}
export function getBusinessSlug() {
  return currentBusinessSlug;
}

export class ApiError extends Error {
  code: string;
  fields?: Record<string, string>;
  status: number;
  constructor(status: number, code: string, message: string, fields?: Record<string, string>) {
    super(message);
    this.status = status;
    this.code = code;
    this.fields = fields;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE_URL}${path}`, {
    ...init,
    credentials: "include",
    headers: {
      ...(typeof init?.body === "string" ? { "Content-Type": "application/json" } : {}),
      ...(currentBusinessSlug ? { "X-Business-Slug": currentBusinessSlug } : {}),
      ...init?.headers,
    },
  });

  if (res.status === 204) {
    return undefined as T;
  }

  const body = await res.json().catch(() => ({}));

  if (!res.ok) {
    throw new ApiError(
      res.status,
      body.code ?? "unknown_error",
      body.message ?? "Something went wrong. Please try again.",
      body.fields ?? undefined,
    );
  }
  // List/OK responses are wrapped as { data, meta? }.
  return body as T;
}

export const api = {
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, data?: unknown) =>
    request<T>(path, { method: "POST", body: data ? JSON.stringify(data) : undefined }),
  patch: <T>(path: string, data?: unknown) =>
    request<T>(path, { method: "PATCH", body: data ? JSON.stringify(data) : undefined }),
  put: <T>(path: string, data?: unknown) =>
    request<T>(path, { method: "PUT", body: data ? JSON.stringify(data) : undefined }),
  delete: <T>(path: string) => request<T>(path, { method: "DELETE" }),
  // Multipart upload. The browser sets its own Content-Type (with the
  // boundary); request() must not override it with application/json here.
  upload: <T>(path: string, form: FormData) => request<T>(path, { method: "POST", body: form }),
};

export interface Envelope<T> {
  data: T;
  meta?: { page: number; pageSize: number; totalItems: number; totalPages: number };
}
