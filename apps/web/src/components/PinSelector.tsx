import { useEffect, useRef, useState } from "react";
import { CheckCircle2, ChevronDown, Loader2, MapPin, XCircle } from "lucide-react";
import { useDeliveryEstimate, useServiceability } from "../hooks/useDelivery";
import { formatPaise } from "../lib/money";

// Delivery-location selector. Uses the real serviceability endpoint so the
// ETA, fee and COD shown here are the same numbers checkout will use.
export default function PinSelector({ compact = false }: { compact?: boolean }) {
  const { pin, setPin, area, eta } = useDeliveryEstimate();
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState(pin);
  const root = useRef<HTMLDivElement>(null);
  const draftValid = /^\d{6}$/.test(draft);
  const preview = useServiceability(draftValid ? draft : "");

  useEffect(() => {
    if (!open) return;
    setDraft(pin);
    function onDown(e: MouseEvent) {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false);
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const label = pin && area?.city ? `${area.city} ${pin}` : pin || "Select location";

  return (
    <div ref={root} className="relative">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        aria-haspopup="dialog"
        className={
          compact
            ? "flex w-full items-center gap-1.5 text-left text-sm text-ink-700"
            : "flex items-center gap-2 rounded-xl px-2.5 py-1.5 text-left hover:bg-ink-100"
        }
      >
        <MapPin size={compact ? 15 : 18} className="shrink-0 text-brand-700" aria-hidden />
        {compact ? (
          <span className="min-w-0 truncate">
            {pin ? (
              <>
                Deliver to <strong className="font-semibold text-ink-900">{label}</strong>
                {eta && <span className="text-ink-500"> · {eta}</span>}
              </>
            ) : (
              <strong className="font-semibold text-ink-900">Add your PIN code to see delivery time</strong>
            )}
          </span>
        ) : (
          <span className="leading-tight">
            <span className="block text-xs text-ink-500">{eta ? `Delivery in ${eta}` : "Deliver to"}</span>
            <span className="block max-w-[9.5rem] truncate text-sm font-semibold text-ink-900">{label}</span>
          </span>
        )}
        <ChevronDown size={15} className="shrink-0 text-ink-400" aria-hidden />
      </button>

      {open && (
        <div
          role="dialog"
          aria-label="Choose delivery location"
          className="absolute left-0 z-50 mt-2 w-[min(22rem,calc(100vw-2rem))] rounded-2xl border border-ink-200 bg-white p-4 shadow-pop animate-pop-in"
        >
          <h2 className="font-display text-base font-bold text-ink-900">Where should we deliver?</h2>
          <p className="mt-0.5 text-sm text-ink-500">We'll show delivery time and charges for your area.</p>
          <form
            className="mt-3 flex gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              if (!draftValid) return;
              setPin(draft);
              setOpen(false);
            }}
          >
            <input
              autoFocus
              value={draft}
              onChange={(e) => setDraft(e.target.value.replace(/\D/g, "").slice(0, 6))}
              inputMode="numeric"
              placeholder="6-digit PIN code"
              aria-label="PIN code"
              className="input tabular"
            />
            <button type="submit" disabled={!draftValid} className="btn btn-primary shrink-0">
              Apply
            </button>
          </form>
          <div className="mt-3 min-h-[2.5rem] text-sm" aria-live="polite">
            {draftValid && preview.isLoading && (
              <p className="flex items-center gap-2 text-ink-500">
                <Loader2 size={15} className="animate-spin" /> Checking {draft}…
              </p>
            )}
            {draftValid && preview.data && preview.data.isServiceable && (
              <p className="flex items-start gap-2 text-ink-700">
                <CheckCircle2 size={16} className="mt-0.5 shrink-0 text-save-600" />
                <span>
                  Delivers to <strong>{preview.data.city}</strong> in {preview.data.etaMinDays}–{preview.data.etaMaxDays} days ·{" "}
                  {preview.data.deliveryFeePaise === 0 ? "free delivery" : `${formatPaise(preview.data.deliveryFeePaise)} delivery`}
                  {preview.data.codAvailable && " · Cash on delivery available"}
                </span>
              </p>
            )}
            {draftValid && !preview.isLoading && (preview.data === null || preview.data?.isServiceable === false) && (
              <p className="flex items-start gap-2 text-ink-700">
                <XCircle size={16} className="mt-0.5 shrink-0 text-red-600" />
                We don't deliver to {draft} yet. You can still browse and save items.
              </p>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
