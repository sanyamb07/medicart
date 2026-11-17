import { Info } from "lucide-react";

// Persistent, honest reminder that the catalogue is demonstration data and
// payments/WhatsApp run in sandbox mode — never let a demo look like a real
// pharmacy with real stock. Kept to one slim line so it informs without
// pushing the store below the fold.
export default function DemoBanner() {
  return (
    <div className="bg-ink-900 text-ink-200 text-xs">
      <div className="page flex items-center justify-center gap-2 py-1.5 text-center">
        <Info size={13} className="shrink-0 text-rx-200" aria-hidden />
        <p>
          <strong className="font-semibold text-white">Demo store.</strong> Sample products and prices; payments and
          WhatsApp run in test mode, so no real orders are fulfilled.
        </p>
      </div>
    </div>
  );
}
