import type { ImportMessagesResponse, MessagesResponse } from "./types";

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
    throw new Error(`Request failed with HTTP ${response.status}`);
  }

  return response.json() as Promise<MessagesResponse>;
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
