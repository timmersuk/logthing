import { useCallback, useEffect, useMemo, useRef, useState, type ChangeEvent } from "react";
import {
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Database,
  Download,
  Pause,
  Play,
  RefreshCcw,
  Search,
  Send,
  Server,
  Shield,
  Upload,
  Wifi
} from "lucide-react";
import { importMessages, listMessages, openMessageStream, sendTestEvent } from "./api";
import type { SyslogMessage } from "./types";

const messageLimit = 500;

function formatDate(value?: string): string {
  if (!value) {
    return "";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

function formatJSON(value?: Record<string, unknown>): string {
  if (!value || Object.keys(value).length === 0) {
    return "";
  }
  return JSON.stringify(value);
}

function numberText(value?: number): string {
  return value === undefined || value === null ? "" : String(value);
}

function filenameTimestamp(date: Date): string {
  return date.toISOString().replace(/[:.]/g, "-");
}

function hostLabel(hosts: string[]): string {
  if (hosts.length === 0) {
    return "All hosts";
  }
  if (hosts.length === 1) {
    return hosts[0];
  }
  return `${hosts.length} hosts`;
}

function sortedUniqueHosts(hosts: string[]): string[] {
  return Array.from(new Set(hosts.map((host) => host.trim()).filter(Boolean))).sort((a, b) =>
    a.localeCompare(b)
  );
}

function messageSearchText(message: SyslogMessage): string {
  return [
    message.id,
    message.transport,
    message.source,
    message.hostname,
    message.app_name,
    message.proc_id,
    message.msg_id,
    message.tag,
    message.message,
    formatJSON(message.structured_data),
    formatJSON(message.raw)
  ]
    .join(" ")
    .toLowerCase();
}

function matchesVisibleFilters(message: SyslogMessage, filter: string, hosts: string[]): boolean {
  if (hosts.length > 0 && !hosts.includes(message.hostname ?? "")) {
    return false;
  }

  const query = filter.trim().toLowerCase();
  if (query !== "" && !messageSearchText(message).includes(query)) {
    return false;
  }

  return true;
}

function receivedAtMillis(message: SyslogMessage): number {
  const parsed = Date.parse(message.received_at);
  return Number.isNaN(parsed) ? 0 : parsed;
}

function compareMessagesLatestFirst(left: SyslogMessage, right: SyslogMessage): number {
  return receivedAtMillis(right) - receivedAtMillis(left);
}

export default function App() {
  const importInputRef = useRef<HTMLInputElement | null>(null);
  const messagesRef = useRef<SyslogMessage[]>([]);
  const [messages, setMessages] = useState<SyslogMessage[]>([]);
  const [filterInput, setFilterInput] = useState("");
  const [filter, setFilter] = useState("");
  const [selectedHosts, setSelectedHosts] = useState<string[]>([]);
  const [knownHosts, setKnownHosts] = useState<string[]>([]);
  const [hostMenuOpen, setHostMenuOpen] = useState(false);
  const [page, setPage] = useState(0);
  const [hasMore, setHasMore] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [liveUnavailable, setLiveUnavailable] = useState(false);
  const [loading, setLoading] = useState(false);
  const [importing, setImporting] = useState(false);
  const [sendingTest, setSendingTest] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);

  useEffect(() => {
    const handle = window.setTimeout(() => {
      setFilter(filterInput);
      setPage(0);
    }, 250);
    return () => window.clearTimeout(handle);
  }, [filterInput]);

  const offset = page * messageLimit;

  const hostOptions = useMemo(
    () => sortedUniqueHosts([...knownHosts, ...selectedHosts]),
    [knownHosts, selectedHosts]
  );
  const liveDisabled = page !== 0 || liveUnavailable;
  const liveTitle = liveUnavailable
    ? "Live updates unavailable"
    : page !== 0
      ? "Live updates are only available on page 1"
      : autoRefresh
        ? "Pause live updates"
        : "Resume live updates";

  const refresh = useCallback(
    async (signal?: AbortSignal) => {
      setLoading(true);
      setError(null);
      try {
        const response = await listMessages(
          { query: filter, hosts: selectedHosts, limit: messageLimit, offset },
          signal
        );
        messagesRef.current = response.data;
        setMessages(response.data);
        setHasMore(response.meta.has_more);
        setKnownHosts((current) =>
          sortedUniqueHosts([
            ...current,
            ...selectedHosts,
            ...response.data.map((message) => message.hostname ?? "")
          ])
        );
        setLastUpdated(new Date());
      } catch (err) {
        if (err instanceof DOMException && err.name === "AbortError") {
          return;
        }
        setError(err instanceof Error ? err.message : "Request failed");
      } finally {
        setLoading(false);
      }
    },
    [filter, offset, selectedHosts]
  );

  useEffect(() => {
    const controller = new AbortController();
    void refresh(controller.signal);
    return () => controller.abort();
  }, [refresh]);

  useEffect(() => {
    if (liveDisabled && autoRefresh) {
      setAutoRefresh(false);
    }
  }, [autoRefresh, liveDisabled]);

  useEffect(() => {
    if (!autoRefresh || page !== 0 || liveUnavailable) {
      return undefined;
    }

    const events = openMessageStream();
    const handleMessage = (event: MessageEvent) => {
      let message: SyslogMessage;
      try {
        message = JSON.parse(event.data) as SyslogMessage;
      } catch {
        setError("Received an invalid live message event");
        return;
      }

      setKnownHosts((current) => sortedUniqueHosts([...current, message.hostname ?? ""]));
      setLastUpdated(new Date());

      if (!matchesVisibleFilters(message, filter, selectedHosts)) {
        return;
      }

      const merged = [
        message,
        ...messagesRef.current.filter((existing) => existing.id !== message.id)
      ].sort(compareMessagesLatestFirst);
      const nextMessages = merged.slice(0, messageLimit);
      messagesRef.current = nextMessages;
      setMessages(nextMessages);
      if (merged.length > messageLimit) {
        setHasMore(true);
      }
    };
    const handleError = () => {
      setAutoRefresh(false);
      setLiveUnavailable(true);
      setError("Live updates disabled because the SSE connection failed. Use Refresh for manual updates.");
      events.close();
    };

    events.addEventListener("message", handleMessage);
    events.addEventListener("error", handleError);
    return () => {
      events.removeEventListener("message", handleMessage);
      events.removeEventListener("error", handleError);
      events.close();
    };
  }, [autoRefresh, filter, liveUnavailable, page, selectedHosts]);

  const latestReceived = useMemo(() => {
    if (messages.length === 0) {
      return "";
    }
    return formatDate(messages[0].received_at);
  }, [messages]);

  const handleSendTestEvent = useCallback(async () => {
    setSendingTest(true);
    setError(null);
    setNotice(null);
    try {
      await sendTestEvent();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Request failed");
    } finally {
      setSendingTest(false);
    }
  }, []);

  const handleImportFile = useCallback(
    async (event: ChangeEvent<HTMLInputElement>) => {
      const file = event.currentTarget.files?.[0];
      if (!file) {
        return;
      }

      setImporting(true);
      setError(null);
      setNotice(null);
      try {
        const response = await importMessages(file);
        setNotice(`Imported ${response.imported} messages, skipped ${response.skipped} blank lines`);
        if (page === 0) {
          void refresh();
        } else {
          setPage(0);
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : "Request failed");
      } finally {
        setImporting(false);
        event.currentTarget.value = "";
      }
    },
    [page, refresh]
  );

  const clearHosts = useCallback(() => {
    setPage(0);
    setSelectedHosts([]);
  }, []);

  const toggleHost = useCallback((host: string) => {
    setPage(0);
    setSelectedHosts((current) => {
      if (current.includes(host)) {
        return current.filter((value) => value !== host);
      }
      return sortedUniqueHosts([...current, host]);
    });
  }, []);

  const handleExport = useCallback(() => {
    if (messages.length === 0) {
      return;
    }
    const body = `${messages.map((message) => JSON.stringify(message)).join("\n")}\n`;
    const blob = new Blob([body], { type: "application/x-ndjson;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `logthing-visible-${filenameTimestamp(new Date())}.ndjson`;
    link.click();
    window.setTimeout(() => URL.revokeObjectURL(url), 0);
  }, [messages]);

  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="brand">
          <span className="brand-mark" aria-hidden="true">
            <Wifi size={20} />
          </span>
          <div>
            <h1>Logthing</h1>
            <p>{latestReceived || "No messages received"}</p>
          </div>
        </div>
        <div className="status-strip" aria-live="polite">
          <span className="status-pill">
            <Database size={15} />
            {messages.length}
          </span>
          <span className="status-pill">
            <Shield size={15} />
            Basic auth
          </span>
          <span className="status-text">
            {lastUpdated ? lastUpdated.toLocaleTimeString() : ""}
          </span>
        </div>
      </header>

      <section className="toolbar" aria-label="Timeline controls">
        <div className="host-filter">
          <button
            type="button"
            className="host-filter-button"
            onClick={() => setHostMenuOpen((value) => !value)}
            aria-expanded={hostMenuOpen}
          >
            <Server size={17} />
            <span>{hostLabel(selectedHosts)}</span>
            <ChevronDown size={16} aria-hidden="true" />
          </button>

          {hostMenuOpen && (
            <div className="host-menu">
              <label className="host-option">
                <input
                  type="checkbox"
                  checked={selectedHosts.length === 0}
                  onChange={clearHosts}
                />
                <span>All hosts</span>
              </label>

              {hostOptions.map((host) => (
                <label className="host-option" key={host}>
                  <input
                    type="checkbox"
                    checked={selectedHosts.includes(host)}
                    onChange={() => toggleHost(host)}
                  />
                  <span>{host}</span>
                </label>
              ))}

              {hostOptions.length === 0 && <div className="host-empty">No hosts</div>}
            </div>
          )}
        </div>

        <label className="filter-box">
          <Search size={18} />
          <input
            value={filterInput}
            onChange={(event) => setFilterInput(event.target.value)}
            placeholder="Filter messages"
            spellCheck={false}
          />
        </label>

        <label className="switch">
          <input
            type="checkbox"
            checked={autoRefresh && !liveDisabled}
            disabled={liveDisabled}
            onChange={(event) => {
              if (!liveDisabled) {
                setAutoRefresh(event.target.checked);
              }
            }}
          />
          <span className="switch-track" aria-hidden="true">
            <span />
          </span>
          <span>Live</span>
        </label>

        <button
          type="button"
          className="command-button"
          onClick={() => void handleSendTestEvent()}
          disabled={sendingTest}
        >
          <Send size={17} className={sendingTest ? "spin" : ""} />
          <span>Send test event</span>
        </button>

        <button
          type="button"
          className="command-button secondary-command"
          onClick={handleExport}
          disabled={messages.length === 0}
        >
          <Download size={17} />
          <span>Export NDJSON</span>
        </button>

        <button
          type="button"
          className="command-button secondary-command"
          onClick={() => importInputRef.current?.click()}
          disabled={importing}
        >
          <Upload size={17} className={importing ? "spin" : ""} />
          <span>Import NDJSON</span>
        </button>
        <input
          ref={importInputRef}
          className="file-input"
          type="file"
          accept=".ndjson,application/x-ndjson,text/plain,application/json"
          onChange={(event) => void handleImportFile(event)}
        />

        <div className="pager" aria-label="Message pages">
          <button
            type="button"
            className="icon-button"
            onClick={() => setPage((value) => Math.max(0, value - 1))}
            disabled={page === 0}
            title="Previous page"
            aria-label="Previous page"
          >
            <ChevronLeft size={18} />
          </button>
          <span>Page {page + 1}</span>
          <button
            type="button"
            className="icon-button"
            onClick={() => setPage((value) => value + 1)}
            disabled={!hasMore}
            title="Next page"
            aria-label="Next page"
          >
            <ChevronRight size={18} />
          </button>
        </div>

        <button
          type="button"
          className="icon-button"
          onClick={() => void refresh()}
          title="Refresh"
          aria-label="Refresh"
        >
          <RefreshCcw size={18} className={loading ? "spin" : ""} />
        </button>

        <button
          type="button"
          className="icon-button"
          onClick={() => {
            if (!liveDisabled) {
              setAutoRefresh((value) => !value);
            }
          }}
          disabled={liveDisabled}
          title={liveTitle}
          aria-label={liveTitle}
        >
          {autoRefresh && !liveDisabled ? <Pause size={18} /> : <Play size={18} />}
        </button>
      </section>

      {error && <div className="error-banner">{error}</div>}
      {notice && <div className="notice-banner">{notice}</div>}

      <main className="table-shell">
        <table>
          <thead>
            <tr>
              <th>Received</th>
              <th>Timestamp</th>
              <th>Host</th>
              <th>App</th>
              <th>Proc</th>
              <th>Msg ID</th>
              <th>Facility</th>
              <th>Severity</th>
              <th>Priority</th>
              <th>Tag</th>
              <th>Source</th>
              <th>Transport</th>
              <th>Structured</th>
              <th>Message</th>
            </tr>
          </thead>
          <tbody>
            {messages.map((message) => {
              const structured = formatJSON(message.structured_data);
              return (
                <tr key={message.id}>
                  <td>{formatDate(message.received_at)}</td>
                  <td>{formatDate(message.timestamp)}</td>
                  <td>{message.hostname}</td>
                  <td>{message.app_name}</td>
                  <td>{message.proc_id}</td>
                  <td>{message.msg_id}</td>
                  <td className="numeric">{numberText(message.facility)}</td>
                  <td className="numeric">{numberText(message.severity)}</td>
                  <td className="numeric">{numberText(message.priority)}</td>
                  <td>{message.tag}</td>
                  <td>{message.source}</td>
                  <td>{message.transport}</td>
                  <td className="structured" title={structured}>
                    {structured}
                  </td>
                  <td className="message-cell" title={message.message}>
                    {message.message}
                  </td>
                </tr>
              );
            })}
            {messages.length === 0 && (
              <tr>
                <td className="empty" colSpan={14}>
                  No matching messages
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </main>
    </div>
  );
}
