// Lightweight, accessible feedback toasts (cart and wishlist updates, errors).
// One live region announces messages to screen readers; toasts auto-dismiss
// and never block interaction.
import { createContext, useCallback, useContext, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { AlertCircle, CheckCircle2, Heart, X } from "lucide-react";

type Tone = "success" | "error" | "wishlist";
interface Toast {
  id: number;
  tone: Tone;
  title: string;
  action?: { label: string; to: string };
}

const ToastContext = createContext<((t: Omit<Toast, "id">) => void) | null>(null);

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const nextId = useRef(1);

  const dismiss = useCallback((id: number) => setToasts((ts) => ts.filter((t) => t.id !== id)), []);

  const push = useCallback(
    (t: Omit<Toast, "id">) => {
      const id = nextId.current++;
      setToasts((ts) => [...ts.slice(-2), { ...t, id }]);
      setTimeout(() => dismiss(id), 3200);
    },
    [dismiss],
  );

  return (
    <ToastContext.Provider value={push}>
      {children}
      <div
        aria-live="polite"
        role="status"
        className="pointer-events-none fixed inset-x-0 z-[70] flex flex-col items-center gap-2 px-4 bottom-[5.5rem] md:bottom-auto md:top-24 md:items-end md:pr-6"
      >
        {toasts.map((t) => {
          const Icon = t.tone === "error" ? AlertCircle : t.tone === "wishlist" ? Heart : CheckCircle2;
          return (
            <div
              key={t.id}
              className="pointer-events-auto flex w-full max-w-sm items-center gap-3 rounded-2xl bg-ink-900 text-white px-4 py-3 shadow-pop animate-slide-up"
            >
              <Icon
                size={18}
                className={
                  t.tone === "error" ? "text-red-300 shrink-0" : t.tone === "wishlist" ? "text-pink-300 shrink-0" : "text-brand-300 shrink-0"
                }
                fill={t.tone === "wishlist" ? "currentColor" : "none"}
              />
              <p className="flex-1 text-sm font-medium">{t.title}</p>
              {t.action && (
                <Link
                  to={t.action.to}
                  onClick={() => dismiss(t.id)}
                  className="text-sm font-semibold text-brand-300 hover:text-brand-200 shrink-0"
                >
                  {t.action.label}
                </Link>
              )}
              <button onClick={() => dismiss(t.id)} aria-label="Dismiss" className="text-ink-400 hover:text-white shrink-0">
                <X size={16} />
              </button>
            </div>
          );
        })}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast() {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error("useToast must be used within ToastProvider");
  return ctx;
}
