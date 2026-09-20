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

/** An authenticated POST that sends a body as-is.
 *
 *  request() sets Content-Type: application/json and JSON-encodes, which is wrong for a
 *  file: multipart needs the boundary the browser generates, and setting the header by
 *  hand breaks it. This existed as a private helper on the backup page; the setup page
 *  needs the same thing now, and two copies of a fetch wrapper is how one of them ends
 *  up with a different error message than the other. */
async function upload<T>(path: string, body: Blob, onProgress?: (sent: number) => void): Promise<T> {
  const token = getToken();

  // XMLHttpRequest, not fetch.
  //
  // fetch cannot report how much of a request body has gone out, and an archive is
  // tens of megabytes over a home upstream. Without a number the page is silent for
  // minutes and looks broken — which is exactly how it was reported: "dann ist es da
  // aber dann nichts mehr". The download path already counts bytes for the same
  // reason (§4.73); the upload was the half nobody had watched.
  return new Promise<T>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", `${BASE}${path}`);
    if (token) xhr.setRequestHeader("Authorization", `Bearer ${token}`);

    if (onProgress) {
      xhr.upload.onprogress = (e) => onProgress(e.loaded);
    }
    xhr.onerror = () => reject(new ApiError("Die Verbindung ist beim Hochladen abgebrochen.", 0));
    xhr.onabort = () => reject(new ApiError("Hochladen abgebrochen.", 0));
    xhr.onload = () => {
      const text = xhr.responseText;
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          resolve((text ? JSON.parse(text) : undefined) as T);
        } catch {
          reject(new ApiError("Die Antwort des Servers war nicht lesbar.", xhr.status));
        }
        return;
      }
      // 413 does not come from this application — it is whatever sits in front of it
      // refusing the size, and saying so saves an hour of looking in the wrong place.
      if (xhr.status === 413) {
        reject(new ApiError(
          "Das Archiv wurde unterwegs abgewiesen, weil es zu groß ist — nicht von MatrixCtrl, " +
          "sondern von einem Proxy davor (Cloudflare lässt im kostenlosen Tarif 100 MB durch). " +
          "Direkt auf den Server hochladen oder den Proxy für diesen Namen umgehen.", 413));
        return;
      }
      let message = `HTTP ${xhr.status}`;
      try { message = (JSON.parse(text || "{}") as { error?: string }).error ?? message; } catch { /* not JSON */ }
      reject(new ApiError(message, xhr.status));
    };
    xhr.send(body);
  });
}

export const api = {
  upload,
  get: <T = unknown>(path: string) => request<T>("GET", path),
  post: <T = unknown>(path: string, body: unknown) => request<T>("POST", path, body),
  put: <T = unknown>(path: string, body: unknown) => request<T>("PUT", path, body),
  delete: <T = unknown>(path: string) => request<T>("DELETE", path),
};
