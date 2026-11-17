// Minimal, privacy-conscious event tracking.
//
// Events carry only an anonymous per-browser id (a random token in
// localStorage, never a customer id) and a small set of non-sensitive
// properties — product ids/slugs, prices, counts. Never a typed search
// query, a name, an address, or anything medical. Events batch and flush on
// a short interval so a burst of interactions (typing a search, bumping a
// quantity) doesn't mean a network request per keystroke.
const BASE_URL = import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";
const ANON_ID_KEY = "mc_anon_id";
const FLUSH_INTERVAL_MS = 2000;
const MAX_QUEUE = 20;

type Props = Record<string, string | number | boolean | null>;

function getAnonId(): string {
  try {
    let id = localStorage.getItem(ANON_ID_KEY);
    if (!id) {
      id = crypto.randomUUID();
      localStorage.setItem(ANON_ID_KEY, id);
    }
    return id;
  } catch {
    // Private browsing / blocked storage: fall back to a per-load id rather
    // than breaking tracking entirely.
    return "ephemeral";
  }
}

const queue: { event: string; properties: Props }[] = [];
let flushTimer: ReturnType<typeof setTimeout> | null = null;

function scheduleFlush() {
  if (flushTimer) return;
  flushTimer = setTimeout(flush, FLUSH_INTERVAL_MS);
}

function flush() {
  flushTimer = null;
  if (queue.length === 0) return;
  const events = queue.splice(0, MAX_QUEUE);
  const body = JSON.stringify({ anonId: getAnonId(), events });

  // sendBeacon survives a page navigation (e.g. the WhatsApp handoff opening
  // a new tab); fall back to fetch where it's unavailable.
  const url = `${BASE_URL}/api/v1/analytics/events`;
  if (navigator.sendBeacon) {
    const ok = navigator.sendBeacon(url, new Blob([body], { type: "application/json" }));
    if (ok) return;
  }
  fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body, keepalive: true }).catch(() => {
    // Analytics must never surface as a user-visible error.
  });
}

export function track(event: string, properties: Props = {}) {
  queue.push({ event, properties });
  if (queue.length >= MAX_QUEUE) {
    flush();
  } else {
    scheduleFlush();
  }
}

// Flush on tab hide, not just on an interval, so events from a short visit
// aren't lost to the timer never firing before the tab closes.
if (typeof document !== "undefined") {
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "hidden") flush();
  });
}
