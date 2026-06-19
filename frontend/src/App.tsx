import { useCallback, useEffect, useMemo, useState } from "react";
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
  Wifi
} from "lucide-react";
import { listMessages, sendTestEvent } from "./api";
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

export default function App() {
  const [messages, setMessages] = useState<SyslogMessage[]>([]);
  const [filterInput, setFilterInput] = useState("");
  const [filter, setFilter] = useState("");
  const [selectedHosts, setSelectedHosts] = useState<string[]>([]);
  const [knownHosts, setKnownHosts] = useState<string[]>([]);
  const [hostMenuOpen, setHostMenuOpen] = useState(false);
  const [page, setPage] = useState(0);
  const [hasMore, setHasMore] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [loading, setLoading] = useState(false);
  const [sendingTest, setSendingTest] = useState(false);
  const [error, setError] = useState<string | null>(null);
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

  const refresh = useCallback(
    async (signal?: AbortSignal) => {
      setLoading(true);
      setError(null);
      try {
        const response = await listMessages(
          { query: filter, hosts: selectedHosts, limit: messageLimit, offset },
          signal
        );
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
    if (!autoRefresh) {
      return undefined;
    }
    const handle = window.setInterval(() => {
      void refresh();
    }, 2000);
    return () => window.clearInterval(handle);
  }, [autoRefresh, refresh]);

  const latestReceived = useMemo(() => {
    if (messages.length === 0) {
      return "";
    }
    return formatDate(messages[0].received_at);
  }, [messages]);

  const handleSendTestEvent = useCallback(async () => {
    setSendingTest(true);
    setError(null);
    try {
      await sendTestEvent();
      window.setTimeout(() => {
        void refresh();
      }, 300);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Request failed");
    } finally {
      setSendingTest(false);
    }
  }, [refresh]);

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
            checked={autoRefresh}
            onChange={(event) => setAutoRefresh(event.target.checked)}
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
          onClick={() => setAutoRefresh((value) => !value)}
          title={autoRefresh ? "Pause live updates" : "Resume live updates"}
          aria-label={autoRefresh ? "Pause live updates" : "Resume live updates"}
        >
          {autoRefresh ? <Pause size={18} /> : <Play size={18} />}
        </button>
      </section>

      {error && <div className="error-banner">{error}</div>}

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
