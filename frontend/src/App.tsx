import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
} from "react";
import {
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Bell,
  CircleAlert,
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
  Wifi,
} from "lucide-react";
import {
  importMessages,
  listMessages,
  openMessageStream,
  sendTestEvent,
  fetchBuildID,
  getIncidentHealth,
  listIncidents,
  sendTestNotification,
} from "./api";
import type { Incident, IncidentHealth, SyslogMessage } from "./types";

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
  return Array.from(
    new Set(hosts.map((host) => host.trim()).filter(Boolean)),
  ).sort((a, b) => a.localeCompare(b));
}

function receivedAtMillis(message: SyslogMessage): number {
  const parsed = Date.parse(message.received_at);
  return Number.isNaN(parsed) ? 0 : parsed;
}

function compareMessagesLatestFirst(
  left: SyslogMessage,
  right: SyslogMessage,
): number {
  return receivedAtMillis(right) - receivedAtMillis(left);
}

export default function App() {
  const importInputRef = useRef<HTMLInputElement | null>(null);
  const messagesRef = useRef<SyslogMessage[]>([]);
  const [messages, setMessages] = useState<SyslogMessage[]>([]);
  const [filterInput, setFilterInput] = useState("");
  const [filter, setFilter] = useState("");
  const [validatedFilter, setValidatedFilter] = useState<string | null>(null);
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
  const [buildId, setBuildId] = useState<string>("");
  const [incidents, setIncidents] = useState<Incident[]>([]);
  const [incidentHealth, setIncidentHealth] = useState<IncidentHealth | null>(null);
  const [sendingNotification, setSendingNotification] = useState(false);

  useEffect(() => {
    fetchBuildID()
      .then(setBuildId)
      .catch((err) => console.error("Failed to fetch build ID:", err));
  }, []);

  const refreshIncidents = useCallback(async (signal?: AbortSignal) => {
    try {
      const [pending, active, resolved, health] = await Promise.all([
        listIncidents("pending_failure", signal),
        listIncidents("active", signal),
        listIncidents("resolved", signal),
        getIncidentHealth(signal),
      ]);
      setIncidents([...pending.data, ...active.data, ...resolved.data]);
      setIncidentHealth(health);
    } catch (err) {
      if (!(err instanceof DOMException && err.name === "AbortError")) {
        setError(err instanceof Error ? err.message : "Incident refresh failed");
      }
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void refreshIncidents(controller.signal);
    const interval = window.setInterval(() => void refreshIncidents(), 15000);
    return () => {
      controller.abort();
      window.clearInterval(interval);
    };
  }, [refreshIncidents]);

  const [pageSizeSetting, setPageSizeSetting] = useState<number | "auto">(
    () => {
      const saved = localStorage.getItem("logthing_page_size");
      if (saved === "auto") return "auto";
      if (saved) {
        const val = parseInt(saved, 10);
        if (!isNaN(val)) return val;
      }
      return 100;
    },
  );
  const [calculatedLimit, setCalculatedLimit] = useState<number>(100);

  useEffect(() => {
    localStorage.setItem("logthing_page_size", String(pageSizeSetting));
  }, [pageSizeSetting]);

  const errorString = error ? String(error) : "";
  const noticeString = notice ? String(notice) : "";

  useEffect(() => {
    if (pageSizeSetting !== "auto") {
      setCalculatedLimit(pageSizeSetting);
      return;
    }

    const updateSize = () => {
      const topbar = document.querySelector(".topbar");
      const toolbar = document.querySelector(".toolbar");
      const errorBanner = document.querySelector(".error-banner");
      const noticeBanner = document.querySelector(".notice-banner");

      const topbarHeight = topbar ? topbar.getBoundingClientRect().height : 72;
      const toolbarHeight = toolbar
        ? toolbar.getBoundingClientRect().height
        : 67;
      const errorHeight = errorBanner
        ? errorBanner.getBoundingClientRect().height
        : 0;
      const noticeHeight = noticeBanner
        ? noticeBanner.getBoundingClientRect().height
        : 0;

      const overhead =
        topbarHeight + toolbarHeight + errorHeight + noticeHeight + 80;
      const availableHeight = window.innerHeight - overhead;
      const rowHeight = 39.2; // 2.45rem * 16px = 39.2px
      const count = Math.max(10, Math.floor(availableHeight / rowHeight));

      setCalculatedLimit(count);
    };

    updateSize();
    window.addEventListener("resize", updateSize);
    const timeoutId = setTimeout(updateSize, 100);
    return () => {
      window.removeEventListener("resize", updateSize);
      clearTimeout(timeoutId);
    };
  }, [pageSizeSetting, errorString, noticeString]);

  const offset = page * calculatedLimit;

  const hostOptions = useMemo(
    () => sortedUniqueHosts([...knownHosts, ...selectedHosts]),
    [knownHosts, selectedHosts],
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
          {
            query: filter,
            hosts: selectedHosts,
            limit: calculatedLimit,
            offset,
          },
          signal,
        );
        setValidatedFilter(filter);
        messagesRef.current = response.data;
        setMessages(response.data);
        setHasMore(response.meta.has_more);
        setKnownHosts((current) =>
          sortedUniqueHosts([
            ...current,
            ...selectedHosts,
            ...response.data.map((message) => message.hostname ?? ""),
          ]),
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
    [filter, offset, selectedHosts, calculatedLimit],
  );

  useEffect(() => {
    const handle = window.setTimeout(() => {
      setFilter(filterInput);
      setPage(0);
    }, 250);
    return () => window.clearTimeout(handle);
  }, [filterInput]);

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
    if (!autoRefresh || page !== 0 || liveUnavailable || validatedFilter !== filter) {
      return undefined;
    }

    const events = openMessageStream(filter);
    const handleMessage = (event: MessageEvent) => {
      let message: SyslogMessage;
      try {
        message = JSON.parse(event.data) as SyslogMessage;
      } catch {
        setError("Received an invalid live message event");
        return;
      }

      setKnownHosts((current) =>
        sortedUniqueHosts([...current, message.hostname ?? ""]),
      );
      setLastUpdated(new Date());

      if (selectedHosts.length > 0 && !selectedHosts.includes(message.hostname ?? "")) {
        return;
      }

      const merged = [
        message,
        ...messagesRef.current.filter((existing) => existing.id !== message.id),
      ].sort(compareMessagesLatestFirst);
      const nextMessages = merged.slice(0, calculatedLimit);
      messagesRef.current = nextMessages;
      setMessages(nextMessages);
      if (merged.length > calculatedLimit) {
        setHasMore(true);
      }
    };
    const handleError = () => {
      setAutoRefresh(false);
      setLiveUnavailable(true);
      setError(
        "Live updates disabled because the SSE connection failed. Use Refresh for manual updates.",
      );
      events.close();
    };

    events.addEventListener("message", handleMessage);
    events.addEventListener("error", handleError);
    return () => {
      events.removeEventListener("message", handleMessage);
      events.removeEventListener("error", handleError);
      events.close();
    };
  }, [
    autoRefresh,
    validatedFilter,
    filter,
    liveUnavailable,
    page,
    selectedHosts,
    calculatedLimit,
  ]);

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

  const handleTestNotification = useCallback(async () => {
    setSendingNotification(true);
    setError(null);
    setNotice(null);
    try {
      await sendTestNotification();
      setNotice("Test notification sent");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Test notification failed");
    } finally {
      setSendingNotification(false);
    }
  }, []);

  const pendingIncidents = incidents.filter((incident) => incident.state === "pending_failure");
  const activeIncidents = incidents.filter((incident) => incident.state === "active" || incident.state === "pending_recovery");
  const resolvedIncidents = incidents.filter((incident) => incident.state === "resolved");

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
        setNotice(
          `Imported ${response.imported} messages, skipped ${response.skipped} blank lines`,
        );
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
    [page, refresh],
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
    const blob = new Blob([body], {
      type: "application/x-ndjson;charset=utf-8",
    });
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
            {buildId && <p className="text-xs opacity-50">Build: {buildId}</p>}
          </div>
        </div>
        <div className="status-strip" aria-live="polite">
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

              {hostOptions.length === 0 && (
                <div className="host-empty">No hosts</div>
              )}
            </div>
          )}
        </div>

        <label className="filter-box">
          <Search size={18} />
          <input
            value={filterInput}
            onChange={(event) => setFilterInput(event.target.value)}
            placeholder="Text or /pppoe|lcp/"
            aria-label="Filter messages"
            title="Case-insensitive text search, or /regex/ such as /pppoe|lcp/"
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
          <span className="pager-separator" aria-hidden="true">
            |
          </span>
          <select
            className="page-size-select"
            value={pageSizeSetting}
            onChange={(event) => {
              const val = event.target.value;
              setPageSizeSetting(val === "auto" ? "auto" : Number(val));
              setPage(0);
            }}
            title="Select page size"
            aria-label="Select page size"
          >
            <option value={25}>25 rows</option>
            <option value={50}>50 rows</option>
            <option value={100}>100 rows</option>
            <option value={250}>250 rows</option>
            <option value={500}>500 rows</option>
            <option value="auto">Auto (Fit)</option>
          </select>
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
          {autoRefresh && !liveDisabled ? (
            <Pause size={18} />
          ) : (
            <Play size={18} />
          )}
        </button>
      </section>

      {error && <div className="error-banner">{error}</div>}
      {notice && <div className="notice-banner">{notice}</div>}

      <section className="incidents-panel" aria-label="Primary WAN incidents">
        <div className="incidents-heading">
          <div>
            <h2><CircleAlert size={19} /> Primary WAN incidents</h2>
            <p>Logical WAN status reported by GL.iNet routers. Backup availability is unknown.</p>
          </div>
          <div className="incident-actions">
            <span className={`analysis-health ${incidentHealth?.running && !incidentHealth.stale && !incidentHealth.last_error ? "healthy" : "unhealthy"}`}>
              {incidentHealth?.last_error ? "Analysis error" : incidentHealth?.stale ? "Analysis stale" : incidentHealth?.running ? `Analysis healthy · ${incidentHealth.pending_jobs} queued` : "Analysis stopped"}
            </span>
            <button className="command-button secondary-command" type="button" disabled={sendingNotification} onClick={() => void handleTestNotification()}>
              <Bell size={16} className={sendingNotification ? "spin" : ""} /> Test notification
            </button>
          </div>
        </div>
        {activeIncidents.length > 0 ? (
          <div className="active-alert"><strong>{activeIncidents.length} primary WAN {activeIncidents.length === 1 ? "incident" : "incidents"} active</strong></div>
        ) : (
          <div className="active-clear">No active primary WAN incidents</div>
        )}
        {pendingIncidents.length > 0 && (
          <div className="pending-alert">{pendingIncidents.length} primary WAN {pendingIncidents.length === 1 ? "failure is" : "failures are"} being confirmed</div>
        )}
        <div className="incident-list">
          {[...activeIncidents, ...pendingIncidents, ...resolvedIncidents.slice(0, 10)].map((incident) => (
            <article className={`incident-card ${incident.state}`} key={incident.id}>
              <div>
                <strong>{incident.hostname} · {incident.interface}</strong>
                <span className="incident-state">{incident.state.replace("_", " ")}</span>
              </div>
              <div className="incident-times">
                <span>Started {formatDate(incident.started_at)}</span>
                {incident.activated_at && <span>Activated {formatDate(incident.activated_at)}</span>}
                {incident.resolved_at && <span>Recovered {formatDate(incident.resolved_at)}</span>}
                <span>Duration {durationBetween(incident.started_at, incident.resolved_at)}</span>
                <span>{deliveryLabel(incident)}</span>
              </div>
              <div className="evidence-links">
                {incident.evidence_ids.map((id) => <button type="button" key={id} onClick={() => { setFilterInput(id); setPage(0); }}>{id.slice(0, 10)}…</button>)}
              </div>
            </article>
          ))}
        </div>
      </section>

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

function durationBetween(start: string, end?: string): string {
  const milliseconds = (end ? Date.parse(end) : Date.now()) - Date.parse(start);
  if (!Number.isFinite(milliseconds) || milliseconds < 0) return "";
  const minutes = Math.floor(milliseconds / 60000);
  const hours = Math.floor(minutes / 60);
  return hours > 0 ? `${hours}h ${minutes % 60}m` : `${minutes}m`;
}

function deliveryLabel(incident: Incident): string {
  if (incident.deliveries.some((delivery) => delivery.last_error && !delivery.sent_at)) return "Delivery failed";
  if (incident.deliveries.some((delivery) => !delivery.sent_at)) return "Delivery pending";
  if (incident.deliveries.length > 0) return "Delivered";
  return "No notification";
}
