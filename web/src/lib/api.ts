const BASE = "";

/** An API failure that still knows its status code.
 *
 *  It used to throw a bare Error, so a caller could only match on the message text.
 *  Anything branching on "was this a 403 or a 404" was therefore silently dead code —
 *  which is how the rooms page nearly shipped with a "this account is not an admin"
 *  explanation that could never appear (E36). */
export class ApiError extends Error {
  readonly status: number;
  constructor(message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

function getToken(): string | null {
  return localStorage.getItem("matrixctrl_token");
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const token = getToken();
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (token) headers["Authorization"] = `Bearer ${token}`;

  const res = await fetch(`${BASE}${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });

  // 401 means *this session* is invalid, so it ends the session. Nothing else may
  // answer 401: a downstream credential going stale — the Matrix admin token behind
  // the rooms page, for instance — would otherwise sign the operator out of MatrixCtrl
  // every time it expired. Those endpoints answer 409 instead (E36).
  if (res.status === 401) {
    localStorage.removeItem("matrixctrl_token");
    window.location.href = "/auth/login";
  }

  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new ApiError(err.error ?? res.statusText, res.status);
  }

  if (res.status === 204) return undefined as T;
  return res.json();
}

export const api = {
  get: <T = unknown>(path: string) => request<T>("GET", path),
  post: <T = unknown>(path: string, body: unknown) => request<T>("POST", path, body),
  put: <T = unknown>(path: string, body: unknown) => request<T>("PUT", path, body),
  delete: <T = unknown>(path: string) => request<T>("DELETE", path),
};
