import { useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, Store } from "lucide-react";
import { api, type Envelope } from "../lib/api";
import type { Business } from "../types";
import { useStorefront } from "../hooks/useStorefront";

export default function Businesses() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { slug, setSlug } = useStorefront();

  const { data, isLoading } = useQuery({
    queryKey: ["businesses"],
    queryFn: () => api.get<Envelope<Business[]>>("/api/v1/businesses"),
  });

  function choose(next: string | null) {
    setSlug(next);
    // Every storefront-scoped query (catalog, cart, categories) depends on
    // which business is selected — clear them so the new storefront's own
    // data loads instead of a stale cache from the previous one.
    qc.clear();
    navigate("/");
  }

  return (
    <div className="max-w-3xl mx-auto px-4 py-10">
      <h1 className="text-xl font-bold text-slate-900 mb-1">Choose your pharmacy</h1>
      <p className="text-sm text-slate-500 mb-6">
        MediCart hosts independent pharmacy businesses. Pick one to shop its catalog and prices.
      </p>

      {isLoading && <p className="text-slate-400">Loading…</p>}

      <div className="space-y-3">
        {(data?.data ?? []).map((b) => {
          const isCurrent = slug === b.slug || (slug === null && b.slug === "medicart-demo");
          return (
            <button
              key={b.id}
              onClick={() => choose(b.slug)}
              className={`w-full text-left bg-white border rounded-2xl p-4 flex items-center gap-4 transition-shadow hover:shadow-cardHover ${
                isCurrent ? "border-brand-300 ring-1 ring-brand-200" : "border-slate-200"
              }`}
            >
              <span className="h-11 w-11 rounded-xl bg-brand-50 text-brand-600 flex items-center justify-center shrink-0">
                <Store size={20} />
              </span>
              <span className="flex-1 min-w-0">
                <span className="block font-semibold text-slate-900">{b.name}</span>
                <span className="block text-sm text-slate-500 truncate">
                  {b.tagline ?? `medicart.local/${b.slug}`}
                </span>
              </span>
              {isCurrent && (
                <span className="flex items-center gap-1 text-xs font-medium text-brand-600 shrink-0">
                  <CheckCircle2 size={15} /> Current
                </span>
              )}
            </button>
          );
        })}
      </div>
    </div>
  );
}
