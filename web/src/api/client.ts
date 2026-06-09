import { TokenManager } from "@/auth/TokenManager";

let tokenManager: TokenManager;

function getTokenManager(): TokenManager {
  if (!tokenManager) {
    tokenManager = TokenManager.getInstance();
  }
  return tokenManager;
}

async function getAccessToken(): Promise<string | null> {
  return getTokenManager().getAccessToken();
}

type RequestOptions = {
  method?: string;
  body?: unknown;
  headers?: Record<string, string>;
};

export class ApiError extends Error {
  code: string;
  status: number;
  requestId?: string;

  constructor(status: number, code: string, message: string, requestId?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.requestId = requestId;
  }
}

async function handleResponse<T>(response: Response): Promise<T> {
  if (response.status === 204) {
    return undefined as T;
  }
  const data = await response.json();
  if (!response.ok) {
    const err = data?.error;
    throw new ApiError(
      response.status,
      err?.code || "UNKNOWN",
      err?.message || "An unexpected error occurred",
      err?.request_id
    );
  }
  return data as T;
}

const API_PREFIX = import.meta.env.VITE_API_PREFIX || '';

export const apiClient = {
  async request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const url = `${API_PREFIX}/api/v1${path}`;
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      ...options.headers,
    };

    const token = await getAccessToken();
    if (token) {
      headers["Authorization"] = `Bearer ${token}`;
    }

    const response = await fetch(url, {
      method: options.method || "GET",
      headers,
      body: options.body ? JSON.stringify(options.body) : undefined,
      credentials: "include",
    });

    if (response.status === 401) {
      const refreshed = await getTokenManager().refreshAccessToken();
      if (refreshed) {
        const newToken = await getAccessToken();
        headers["Authorization"] = `Bearer ${newToken}`;
        const retryResponse = await fetch(url, {
          method: options.method || "GET",
          headers,
          body: options.body ? JSON.stringify(options.body) : undefined,
          credentials: "include",
        });
        return handleResponse<T>(retryResponse);
      }
    }

    return handleResponse<T>(response);
  },

  async uploadFile(path: string, formData: FormData): Promise<unknown> {
    const url = `${API_PREFIX}/api/v1${path}`;
    const headers: Record<string, string> = {};
    const token = await getAccessToken();
    if (token) {
      headers["Authorization"] = `Bearer ${token}`;
    }

    const response = await fetch(url, {
      method: "POST",
      headers,
      body: formData,
      credentials: "include",
    });

    if (response.status === 401) {
      const refreshed = await getTokenManager().refreshAccessToken();
      if (refreshed) {
        const newToken = await getAccessToken();
        headers["Authorization"] = `Bearer ${newToken}`;
        const retryResponse = await fetch(url, {
          method: "POST",
          headers,
          body: formData,
          credentials: "include",
        });
        return handleResponse(retryResponse);
      }
    }

    return handleResponse(response);
  },

  get: <T>(path: string) => apiClient.request<T>(path),
  getList: <T>(path: string) => apiClient.request<{ items: T[] | null; has_more: boolean }>(path).then(r => r.items ?? []),
  post: <T>(path: string, body?: unknown) => apiClient.request<T>(path, { method: "POST", body }),
  patch: <T>(path: string, body?: unknown) => apiClient.request<T>(path, { method: "PATCH", body }),
  delete: <T>(path: string, body?: unknown) => apiClient.request<T>(path, { method: "DELETE", body }),

  async getBlob(path: string): Promise<Blob> {
    const url = `${API_PREFIX}/api/v1${path}`;
    const token = getAccessToken();
    const headers: Record<string, string> = {};
    if (token) headers["Authorization"] = `Bearer ${token}`;

    let response = await fetch(url, { headers });
    if (response.status === 401) {
      const newToken = await getTokenManager().refreshAccessToken();
      if (newToken) {
        headers["Authorization"] = `Bearer ${newToken}`;
        response = await fetch(url, { headers });
      }
    }

    if (!response.ok) {
      let errBody: { error?: { code?: string; message?: string } } = {};
      try { errBody = await response.json(); } catch { /* ignore */ }
      throw new ApiError(
        response.status,
        errBody.error?.code ?? "UNKNOWN",
        errBody.error?.message ?? `HTTP ${response.status}`,
      );
    }

    return response.blob();
  },
};

export function getAuthHeaders(): Record<string, string> {
  return {};
}
