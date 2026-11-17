// Captures and remembers a `?ref=CODE` affiliate referral link, the same way
// a dropshipping storefront attributes a sale to whoever shared the link.
// First-touch attribution: once a code is stored, a later visit through a
// different link does not overwrite it, matching how most affiliate programs
// resolve "which affiliate gets credit" when more than one link was clicked.
const STORAGE_KEY = "mc_affiliate_ref";
const EXPIRY_DAYS = 30;

interface StoredRef {
  code: string;
  storedAt: number;
}

// captureReferralFromURL reads ?ref=CODE from the current URL (if present),
// stores it, and strips it from the address bar so it doesn't linger in a
// shared/bookmarked link. Call once, near the app's root.
export function captureReferralFromURL(): void {
  try {
    const url = new URL(window.location.href);
    const ref = url.searchParams.get("ref");
    if (!ref) return;

    if (!localStorage.getItem(STORAGE_KEY)) {
      const entry: StoredRef = { code: ref.toUpperCase().slice(0, 12), storedAt: Date.now() };
      localStorage.setItem(STORAGE_KEY, JSON.stringify(entry));
    }

    url.searchParams.delete("ref");
    window.history.replaceState({}, "", url.pathname + url.search + url.hash);
  } catch {
    // Private browsing or storage disabled — checkout simply won't carry an
    // affiliate code for this visitor, which is a safe degradation.
  }
}

// getStoredReferralCode returns the remembered code, or "" once it has
// expired or none was ever captured — checkout always sends a string, and an
// empty one is a normal, silently-ignored value server-side.
export function getStoredReferralCode(): string {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return "";
    const entry = JSON.parse(raw) as StoredRef;
    const ageDays = (Date.now() - entry.storedAt) / (1000 * 60 * 60 * 24);
    if (ageDays > EXPIRY_DAYS) {
      localStorage.removeItem(STORAGE_KEY);
      return "";
    }
    return entry.code ?? "";
  } catch {
    return "";
  }
}
