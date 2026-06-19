import type { MessagesResponse } from "./types";

interface ListMessagesParams {
  query: string;
  limit: number;
}

export async function listMessages(
  params: ListMessagesParams,
  signal?: AbortSignal
): Promise<MessagesResponse> {
  const query = new URLSearchParams();
  query.set("limit", String(params.limit));
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
