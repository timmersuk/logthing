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
