import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Plus, Search } from "lucide-react";
import { api, ApiError, type Envelope } from "../../lib/api";
import { formatPaise } from "../../lib/money";
import type { Product } from "../../types";
import PlatformShell from "./PlatformShell";

export default function PlatformProducts() {
  const navigate = useNavigate();
  const [q, setQ] = useState("");

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["platform-products", q],
    queryFn: () => api.get<Envelope<Product[]>>(`/api/v1/platform/products?pageSize=50${q ? `&q=${encodeURIComponent(q)}` : ""}`),
    retry: false,
  });

  if (isError && error instanceof ApiError && error.status === 401) {
    navigate("/platform");
    return null;
  }

  return (
    <PlatformShell>
      <div className="max-w-5xl mx-auto px-6 py-8">
        <div className="flex items-center justify-between mb-5">
          <div>
            <h1 className="text-lg font-bold text-slate-900">Master catalog</h1>
            <p className="text-sm text-slate-500">
              The shared product data every business selects from. Businesses set their own price and visibility.
            </p>
          </div>
          <button
            onClick={() => navigate("/platform/products/new")}
            className="inline-flex items-center gap-1.5 bg-brand-600 text-white text-sm font-semibold rounded-lg px-4 py-2"
          >
            <Plus size={15} /> New product
          </button>
        </div>

        <div className="relative mb-4">
          <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" />
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Search by name or SKU…"
            className="input pl-9 text-sm w-full max-w-sm"
          />
        </div>

        {isLoading && <p className="text-slate-400">Loading…</p>}

        {data && (
          <div className="bg-white border border-slate-200 rounded-2xl overflow-hidden">
            <table className="w-full text-sm">
              <thead className="bg-slate-50 text-slate-500 text-xs uppercase">
                <tr>
                  <th className="text-left px-4 py-2.5">Name</th>
                  <th className="text-left px-4 py-2.5">SKU</th>
                  <th className="text-right px-4 py-2.5">MRP</th>
                  <th className="text-left px-4 py-2.5">Status</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100">
                {data.data.map((p) => (
                  <tr
                    key={p.id}
                    className="hover:bg-slate-50 cursor-pointer"
                    onClick={() => navigate(`/platform/products/${p.id}`)}
                  >
                    <td className="px-4 py-3 font-medium text-slate-800">
                      {p.name}
                      {p.requiresPrescription && (
                        <span className="ml-2 bg-amber-50 text-amber-700 text-[10px] font-semibold px-1.5 py-0.5 rounded">Rx</span>
                      )}
                    </td>
                    <td className="px-4 py-3 font-mono text-xs text-slate-500">{p.sku}</td>
                    <td className="px-4 py-3 text-right">{formatPaise(p.mrpPaise)}</td>
                    <td className="px-4 py-3">
                      <span
                        className={`text-xs font-medium px-2 py-0.5 rounded-full ${
                          p.isActive ? "bg-green-50 text-green-700" : "bg-slate-100 text-slate-500"
                        }`}
                      >
                        {p.isActive ? "Active" : "Inactive"}
                      </span>
                    </td>
                  </tr>
                ))}
                {data.data.length === 0 && (
                  <tr>
                    <td colSpan={4} className="px-4 py-10 text-center text-slate-400">
                      No products found.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </PlatformShell>
  );
}
