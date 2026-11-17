// Per-browser shopping conveniences: wishlist, recently viewed, search
// history, saved-for-later and the delivery PIN.
//
// UI-ONLY / NOT BACKED BY THE API: the Go API has no wishlist, history or
// saved-for-later endpoints yet, so these live in localStorage and are scoped
// to this browser. Only product *slugs* are stored — never prices — so every
// screen that renders these lists re-fetches live price and stock from
// /api/v1/products/:slug instead of showing a stale snapshot. When server
// endpoints exist, swap the bodies of read/write below; the hooks' shape can
// stay the same.
import { useSyncExternalStore } from "react";

type Key = "wishlist" | "recent" | "searches" | "saved" | "pin";

const STORAGE_KEYS: Record<Key, string> = {
  wishlist: "mc_wishlist",
  recent: "mc_recently_viewed",
  searches: "mc_search_history",
  saved: "mc_saved_for_later",
  pin: "mc_pin",
};

const LIMITS: Partial<Record<Key, number>> = { recent: 12, searches: 8, wishlist: 100, saved: 50 };

const listeners = new Set<() => void>();
const cache = new Map<Key, unknown>();

function read<T>(key: Key, fallback: T): T {
  if (cache.has(key)) return cache.get(key) as T;
  let value = fallback;
  try {
    const raw = localStorage.getItem(STORAGE_KEYS[key]);
    if (raw != null) value = (key === "pin" ? raw : JSON.parse(raw)) as T;
  } catch {
    // Blocked storage or a corrupted entry — behave as empty.
  }
  cache.set(key, value);
  return value;
}

function write(key: Key, value: unknown) {
  cache.set(key, value);
  try {
    if (key === "pin") localStorage.setItem(STORAGE_KEYS[key], String(value));
    else localStorage.setItem(STORAGE_KEYS[key], JSON.stringify(value));
  } catch {
    // In-memory value still works for this tab.
  }
  listeners.forEach((l) => l());
}

function subscribe(l: () => void) {
  listeners.add(l);
  const onStorage = (e: StorageEvent) => {
    if (e.key && Object.values(STORAGE_KEYS).includes(e.key)) {
      cache.clear();
      l();
    }
  };
  window.addEventListener("storage", onStorage);
  return () => {
    listeners.delete(l);
    window.removeEventListener("storage", onStorage);
  };
}

const EMPTY: string[] = [];

function useList(key: Key): string[] {
  return useSyncExternalStore(
    subscribe,
    () => read<string[]>(key, EMPTY),
    () => EMPTY,
  );
}

function pushFront(key: Key, value: string) {
  const list = read<string[]>(key, EMPTY).filter((v) => v.toLowerCase() !== value.toLowerCase());
  write(key, [value, ...list].slice(0, LIMITS[key] ?? 50));
}

function removeFrom(key: Key, value: string) {
  write(
    key,
    read<string[]>(key, EMPTY).filter((v) => v !== value),
  );
}

// ---------------------------------------------------------------- wishlist

export function useWishlist() {
  const slugs = useList("wishlist");
  return {
    slugs,
    has: (slug: string) => slugs.includes(slug),
    /** Returns true when the item is now in the wishlist. */
    toggle: (slug: string) => {
      if (read<string[]>("wishlist", EMPTY).includes(slug)) {
        removeFrom("wishlist", slug);
        return false;
      }
      pushFront("wishlist", slug);
      return true;
    },
    remove: (slug: string) => removeFrom("wishlist", slug),
  };
}

// ---------------------------------------------------------------- recently viewed

export function useRecentlyViewed() {
  return useList("recent");
}
export function recordView(slug: string) {
  pushFront("recent", slug);
}
export function clearRecentlyViewed() {
  write("recent", []);
}

// ---------------------------------------------------------------- search history

export function useSearchHistory() {
  const items = useList("searches");
  return {
    items,
    add: (q: string) => q.trim().length >= 2 && pushFront("searches", q.trim()),
    remove: (q: string) => removeFrom("searches", q),
    clear: () => write("searches", []),
  };
}

// ---------------------------------------------------------------- saved for later

export function useSavedForLater() {
  const slugs = useList("saved");
  return {
    slugs,
    add: (slug: string) => pushFront("saved", slug),
    remove: (slug: string) => removeFrom("saved", slug),
  };
}

// ---------------------------------------------------------------- delivery PIN

export function useDeliveryPin(): [string, (pin: string) => void] {
  const pin = useSyncExternalStore(
    subscribe,
    () => read<string>("pin", ""),
    () => "",
  );
  return [pin, (next: string) => write("pin", next)];
}
