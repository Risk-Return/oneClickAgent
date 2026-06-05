const PREFIX = import.meta.env.VITE_API_PREFIX || '';
const REFRESH_URL = PREFIX + "/api/v1/auth/refresh";
const TOKEN_REFRESH_MARGIN_MS = 60_000;
const STORAGE_KEY = 'iagent_tokens';

export class TokenManager {
  private static instance: TokenManager;
  private accessToken: string | null = null;
  private refreshToken: string | null = null;
  private expiresAt: number | null = null;
  private refreshPromise: Promise<boolean> | null = null;
  private onLogout: (() => void) | null = null;

  private constructor() {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored) {
      try {
        const data = JSON.parse(stored);
        this.refreshToken = data.refresh || null;
        this.accessToken = data.access || null;
        this.expiresAt = data.expires || null;
      } catch {
        localStorage.removeItem(STORAGE_KEY);
      }
    }
  }

  static getInstance(): TokenManager {
    if (!TokenManager.instance) {
      TokenManager.instance = new TokenManager();
    }
    return TokenManager.instance;
  }

  setLogoutHandler(handler: () => void) {
    this.onLogout = handler;
  }

  setTokens(accessToken: string, refreshToken: string, expiresIn: number) {
    this.accessToken = accessToken;
    this.refreshToken = refreshToken;
    this.expiresAt = Date.now() + expiresIn * 1000;
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      access: accessToken,
      refresh: refreshToken,
      expires: this.expiresAt,
    }));
    this.scheduleAutoRefresh();
  }

  async getAccessToken(): Promise<string | null> {
    // If no access token but we have a refresh token, try refresh first
    if (!this.accessToken && this.refreshToken) {
      const refreshed = await this.refreshAccessToken();
      if (!refreshed) return null;
    }

    if (!this.accessToken) return null;

    if (this.expiresAt && Date.now() > this.expiresAt - TOKEN_REFRESH_MARGIN_MS) {
      const refreshed = await this.refreshAccessToken();
      if (!refreshed) return null;
    }

    return this.accessToken;
  }

  isAuthenticated(): boolean {
    return this.accessToken !== null;
  }

  getUserRole(): string | null {
    if (!this.accessToken) return null;
    try {
      const payload = JSON.parse(atob(this.accessToken.split(".")[1]));
      return payload.role || null;
    } catch {
      return null;
    }
  }

  getUserId(): string | null {
    if (!this.accessToken) return null;
    try {
      const payload = JSON.parse(atob(this.accessToken.split(".")[1]));
      return payload.sub || null;
    } catch {
      return null;
    }
  }

  async refreshAccessToken(): Promise<boolean> {
    if (this.refreshPromise) {
      return this.refreshPromise;
    }

    if (!this.refreshToken) {
      return false;
    }

    this.refreshPromise = (async () => {
      try {
        const response = await fetch(REFRESH_URL, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ refresh_token: this.refreshToken }),
          credentials: "include",
        });

        if (!response.ok) {
          this.clearTokens();
          return false;
        }

        const data = await response.json();
        this.setTokens(data.access_token, data.refresh_token, data.expires_in);
        return true;
      } catch {
        return false;
      } finally {
        this.refreshPromise = null;
      }
    })();

    return this.refreshPromise;
  }

  async login(email: string, password: string): Promise<boolean> {
    try {
      const response = await fetch(PREFIX + "/api/v1/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
        credentials: "include",
      });

      if (!response.ok) return false;

      const data = await response.json();
      this.setTokens(data.access_token, data.refresh_token, data.expires_in);
      return true;
    } catch {
      return false;
    }
  }

  async register(email: string, username: string, password: string): Promise<boolean> {
    try {
      const response = await fetch(PREFIX + "/api/v1/auth/register", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, username, password }),
        credentials: "include",
      });

      if (!response.ok) return false;

      const data = await response.json();
      this.setTokens(data.access_token, data.refresh_token, data.expires_in);
      return true;
    } catch {
      return false;
    }
  }

  async logout(): Promise<void> {
    try {
      if (this.refreshToken) {
        await fetch(PREFIX + "/api/v1/auth/logout", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ refresh_token: this.refreshToken }),
          credentials: "include",
        });
      }
    } catch {
      // ignore logout errors
    }
    this.clearTokens();
    this.onLogout?.();
  }

  private clearTokens() {
    this.accessToken = null;
    this.refreshToken = null;
    this.expiresAt = null;
    localStorage.removeItem(STORAGE_KEY);
  }

  private scheduleAutoRefresh() {
    if (!this.expiresAt) return;
    const delay = this.expiresAt - Date.now() - TOKEN_REFRESH_MARGIN_MS;
    if (delay > 0) {
      setTimeout(() => this.refreshAccessToken(), Math.max(delay, 1000));
    }
  }
}
