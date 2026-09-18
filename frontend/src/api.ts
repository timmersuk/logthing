import type { IncidentHealth, IncidentsResponse, ImportMessagesResponse, MessagesResponse } from "./types";

interface ListMessagesParams {
  query: string;
  hosts: string[];
  limit: number;
  offset: number;
}

export async function listMessages(
  params: ListMessagesParams,
  signal?: AbortSignal
): Promise<MessagesResponse> {
  const query = new URLSearchParams();
  query.set("limit", String(params.limit));
  query.set("offset", String(params.offset));
  for (const host of params.hosts) {
    const trimmed = host.trim();
    if (trimmed !== "") {
      query.append("host", trimmed);
    }
  }
  if (params.query.trim() !== "") {
    query.set("q", params.query.trim());
  }

  const response = await fetch(`/api/v1/messages?${query.toString()}`, {
    signal,
    credentials: "same-origin"
  });

  if (!response.ok) {
    if (response.status === 401) {
      throw new Error("Authentication required");
    }
    const body = await response.json().catch(() => null) as { error?: string } | null;
    throw new Error(body?.error ?? `Request failed with HTTP ${response.status}`);
  }

  return response.json() as Promise<MessagesResponse>;
}

export function openMessageStream(query: string): EventSource {
  return new EventSource(`/api/v1/messages/stream?${new URLSearchParams({ q: query })}`);
}

export async function sendTestEvent(signal?: AbortSignal): Promise<void> {
  const response = await fetch("/api/v1/test-event", {
    method: "POST",
    signal,
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json"
    },
    body: JSON.stringify({
      message: `logthing browser test event ${new Date().toISOString()}`
    })
  });

  if (!response.ok) {
    if (response.status === 401) {
      throw new Error("Authentication required");
    }
    throw new Error(`Request failed with HTTP ${response.status}`);
  }
}

export async function importMessages(
  file: File,
  signal?: AbortSignal
): Promise<ImportMessagesResponse> {
  const response = await fetch("/api/v1/messages/import", {
    method: "POST",
    signal,
    credentials: "same-origin",
    headers: {
      "Content-Type": file.type || "application/x-ndjson"
    },
    body: file
  });

  if (!response.ok) {
    if (response.status === 401) {
      throw new Error("Authentication required");
    }
    const body = (await response.json().catch(() => null)) as { error?: string } | null;
    throw new Error(body?.error ?? `Request failed with HTTP ${response.status}`);
  }

  return response.json() as Promise<ImportMessagesResponse>;
}

export async function fetchBuildID(): Promise<string> {
  const response = await fetch("/healthcheck", {
    credentials: "same-origin"
  });

  if (!response.ok) {
    throw new Error(`Failed to fetch build ID: ${response.status}`);
  }

  const data = await response.json() as { build_id?: string };
  return data.build_id || "Unknown";
}

async function checkedJSON<T>(response: Response): Promise<T> {
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { error?: string } | null;
    throw new Error(body?.error ?? `Request failed with HTTP ${response.status}`);
  }
  return response.json() as Promise<T>;
}

export async function listIncidents(state: "pending_failure" | "active" | "resolved", signal?: AbortSignal): Promise<IncidentsResponse> {
  const query = new URLSearchParams({ state, limit: "100" });
  return checkedJSON<IncidentsResponse>(await fetch(`/api/v1/incidents?${query}`, {
    signal,
    credentials: "same-origin"
  }));
}

export async function getIncidentHealth(signal?: AbortSignal): Promise<IncidentHealth> {
  return checkedJSON<IncidentHealth>(await fetch("/api/v1/incidents/health", {
    signal,
    credentials: "same-origin"
  }));
}

export async function sendTestNotification(signal?: AbortSignal): Promise<void> {
  await checkedJSON(await fetch("/api/v1/notifications/test", {
    method: "POST",
    signal,
    credentials: "same-origin"
  }));
}
