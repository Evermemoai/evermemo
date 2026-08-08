export type Link = { from: string; rel: string; to: string };

export type Memory = {
  id: string;
  content: string;
  tags?: string[];
  namespace: string;
  metadata?: Record<string, unknown>;
  agent?: string;
  confidence: number;
  created_at: string;
  updated_at: string;
  expires_at?: string;
  score?: number;
  links?: Link[];
};

export type ConsolidateReport = {
  reviewed: number;
  merged: number;
  archived: number;
  kept: number;
  dry_run: boolean;
  actions?: { action: string; ids: string[]; content?: string; reason?: string }[];
};

export type Settings = { hubUrl: string; apiKey: string; agent: string };

export type Account = {
  name?: string;
  email?: string;
  title?: string;
  username?: string;
  notifications?: Record<string, boolean>;
};

export type SecurityInfo = {
  auth_mode: string;
  agents: string[];
  acl_enabled: boolean;
  rate_limited: boolean;
  caller: string;
};

export type BillingInfo = {
  plan: string;
  price: string;
  license: string;
  limits: Record<string, string>;
};

const SETTINGS_KEY = "evermemo.settings";

// Default hub: same origin when served from the binary at /ui, else env/localhost.
function defaultHubUrl(): string {
  if (typeof window !== "undefined" && window.location.pathname.startsWith("/ui")) {
    return window.location.origin;
  }
  return process.env.NEXT_PUBLIC_HUB_URL ?? "http://localhost:7777";
}

export function getSettings(): Settings {
  const fallback: Settings = { hubUrl: defaultHubUrl(), apiKey: "", agent: "dashboard" };
  if (typeof window === "undefined") return fallback;
  try {
    const saved = JSON.parse(localStorage.getItem(SETTINGS_KEY) ?? "{}");
    return { ...fallback, ...saved };
  } catch {
    return fallback;
  }
}

export function saveSettings(s: Settings) {
  localStorage.setItem(SETTINGS_KEY, JSON.stringify(s));
}

function headers(): Record<string, string> {
  const { apiKey, agent } = getSettings();
  const h: Record<string, string> = { "Content-Type": "application/json" };
  if (apiKey) h.Authorization = `Bearer ${apiKey}`;
  if (agent) h["X-Agent"] = agent;
  return h;
}

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const { hubUrl } = getSettings();
  const res = await fetch(`${hubUrl.replace(/\/$/, "")}${path}`, {
    ...init,
    headers: { ...headers(), ...init?.headers },
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error ?? `HTTP ${res.status}`);
  return data as T;
}

export const api = {
  health: () => req<{ status: string; memories: number }>("/health"),

  list: (namespace = "", limit = 50) =>
    req<{ memories: Memory[] }>(
      `/v1/memories?namespace=${encodeURIComponent(namespace)}&limit=${limit}`
    ).then((r) => r.memories),

  search: (q: string, namespace = "", limit = 50) =>
    req<{ memories: Memory[] }>(
      `/v1/memories?q=${encodeURIComponent(q)}&namespace=${encodeURIComponent(namespace)}&limit=${limit}`
    ).then((r) => r.memories),

  get: (id: string) => req<Memory>(`/v1/memories/${id}`),

  add: (content: string, tags: string[], namespace: string, ttl: string) =>
    req<Memory>("/v1/memories", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ content, tags, namespace, ttl: ttl || undefined }),
    }),

  update: (id: string, content: string, tags?: string[]) =>
    req<Memory>(`/v1/memories/${id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ content, tags }),
    }),

  remove: (id: string) => req<{ deleted: string }>(`/v1/memories/${id}`, { method: "DELETE" }),

  verify: (id: string, vote: "confirm" | "dispute", note = "") =>
    req<Memory>(`/v1/memories/${id}/verify`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ vote, note }),
    }),

  link: (from: string, rel: string, to: string) =>
    req<Link>(`/v1/memories/${from}/links`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ rel, to }),
    }),

  consolidate: (namespace: string, dryRun: boolean) =>
    req<ConsolidateReport>("/v1/consolidate", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ namespace, dry_run: dryRun }),
    }),

  account: () => req<Account>("/v1/account"),

  saveAccount: (a: Account) =>
    req<Account>("/v1/account", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(a),
    }),

  security: () => req<SecurityInfo>("/v1/account/security"),

  billing: () => req<BillingInfo>("/v1/account/billing"),

  exportJSONL: async (): Promise<Blob> => {
    const { hubUrl } = getSettings();
    const res = await fetch(`${hubUrl.replace(/\/$/, "")}/v1/export`, { headers: headers() });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return res.blob();
  },

  importJSONL: async (file: File): Promise<number> => {
    const { hubUrl } = getSettings();
    const res = await fetch(`${hubUrl.replace(/\/$/, "")}/v1/import`, {
      method: "POST",
      headers: headers(),
      body: await file.text(),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error ?? `HTTP ${res.status}`);
    return data.imported as number;
  },
};
