export interface SyslogMessage {
  id: string;
  received_at: string;
  timestamp?: string;
  transport?: string;
  source?: string;
  priority?: number;
  facility?: number;
  severity?: number;
  hostname?: string;
  app_name?: string;
  proc_id?: string;
  msg_id?: string;
  tag?: string;
  message?: string;
  structured_data?: Record<string, unknown>;
  raw?: Record<string, unknown>;
}

export interface MessagesResponse {
  data: SyslogMessage[];
  meta: {
    count: number;
    limit: number;
    offset: number;
    has_more: boolean;
  };
}

export interface ImportMessagesResponse {
  status: string;
  imported: number;
  skipped: number;
}

export interface NotificationDelivery {
  id: string;
  kind: "incident_opened" | "incident_resolved";
  attempts: number;
  last_error?: string;
  sent_at?: string;
  adapter?: string;
  permanent?: boolean;
}

export interface Incident {
  id: string;
  rule_version: number;
  hostname: string;
  interface: string;
  state: "pending_failure" | "active" | "pending_recovery" | "resolved";
  started_at: string;
  activated_at?: string;
  recovery_first_at?: string;
  resolved_at?: string;
  last_evidence_at: string;
  evidence_ids: string[];
  evidence_refs?: string[];
  down_after: number;
  recovered_after: number;
  deliveries: NotificationDelivery[];
}

export interface IncidentsResponse {
  data: Incident[];
  meta: MessagesResponse["meta"];
}

export interface IncidentHealth {
  running: boolean;
  stale: boolean;
  last_reconciled_at?: string;
  last_processed_at?: string;
  last_error?: string;
  pending_jobs: number;
}
