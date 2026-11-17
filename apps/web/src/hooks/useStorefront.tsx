import { createContext, useContext, useEffect, useState } from "react";
import { setBusinessSlug } from "../lib/api";

const STORAGE_KEY = "mc_storefront_slug";

interface StorefrontContextValue {
  /** null = the original default storefront (no header sent). */
  slug: string | null;
  setSlug: (slug: string | null) => void;
}

const StorefrontContext = createContext<StorefrontContextValue | null>(null);

export function StorefrontProvider({ children }: { children: React.ReactNode }) {
  const [slug, setSlugState] = useState<string | null>(() => {
    try {
      return localStorage.getItem(STORAGE_KEY);
    } catch {
      return null;
    }
  });

  useEffect(() => {
    setBusinessSlug(slug);
  }, [slug]);

  function setSlug(next: string | null) {
    setSlugState(next);
    try {
      if (next) localStorage.setItem(STORAGE_KEY, next);
      else localStorage.removeItem(STORAGE_KEY);
    } catch {
      // Private browsing or storage disabled — the in-memory value still works for this tab.
    }
  }

  return <StorefrontContext.Provider value={{ slug, setSlug }}>{children}</StorefrontContext.Provider>;
}

export function useStorefront() {
  const ctx = useContext(StorefrontContext);
  if (!ctx) throw new Error("useStorefront must be used within StorefrontProvider");
  return ctx;
}
