import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Boxes, Search } from "lucide-react";
import { api, ApiError, type Envelope } from "../../lib/api";
import { formatPaise } from "../../lib/money";
import type { BusinessCatalogRow } from "../../types";

export default function AdminCatalog() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [q, setQ] = useState("");
  const [editing, setEditing] = useState<string | null>(null);

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["business-catalog", q],
    queryFn: () => api.get<Envelope<BusinessCatalogRow[]>>(`/api/v1/admin/catalog${q ? `?q=${encodeURIComponent(q)}` : ""}`),
    retry: false,
  });

  const save = useMutation({
    mutationFn: (row: BusinessCatalogRow) =>
      api.put(`/api/v1/admin/catalog/${row.productId}`, {
        isListed: row.isListed, pricePaise: row.pricePaise, isVisible: row.isVisible,
        isFeatured: row.isFeatured, minOrderQty: row.minOrderQty,
      }),
    onSuccess: () => {
      setEditing(null);
      void qc.invalidateQueries({ queryKey: ["business-catalog"] });
    },
  });

  if (isError && error instanceof ApiError && error.status === 401) {
    navigate("/admin");
    return null;
  }

  return (
    <div className="max-w-5xl mx-auto px-4 py-8">
      <div className="flex items-center justify-between mb-5">
        <div>
          <h1 className="text-lg font-bold text-slate-900">Catalog</h1>
          <p className="text-sm text-slate-500">Pick which master products you sell, and set your own price.</p>
        </div>
        <div className="flex items-center gap-3">
          <Link to="/admin/orders" className="text-sm text-slate-400 hover:text-slate-600">Orders</Link>
          <Link to="/admin/customers" className="text-sm text-slate-400 hover:text-slate-600">Customers</Link>
          <Link to="/admin/enquiries" className="text-sm text-slate-400 hover:text-slate-600">Enquiries</Link>
        </div>
      </div>

      <div className="relative mb-4">
        <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" />
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search master catalog by name or SKU…"
          className="input pl-9 text-sm w-full max-w-sm"
        />
      </div>

      {isLoading && <p className="text-slate-400">Loading…</p>}

      {data && (
        <div className="bg-white border border-slate-200 rounded-2xl overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-slate-50 text-slate-500 text-xs uppercase">
              <tr>
                <th className="text-left px-4 py-2.5">Product</th>
                <th className="text-right px-4 py-2.5">MRP</th>
                <th className="text-right px-4 py-2.5">Your price</th>
                <th className="text-left px-4 py-2.5">Listed</th>
                <th className="px-4 py-2.5"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {data.data.map((row) =>
                editing === row.productId ? (
                  <EditRow key={row.productId} row={row} onCancel={() => setEditing(null)} onSave={(r) => save.mutate(r)} saving={save.isPending} />
                ) : (
                  <tr key={row.productId} className="hover:bg-slate-50">
                    <td className="px-4 py-3 font-medium text-slate-800">
                      {row.name}
                      {row.requiresPrescription && (
                        <span className="ml-2 bg-amber-50 text-amber-700 text-[10px] font-semibold px-1.5 py-0.5 rounded">Rx</span>
                      )}
                      <span className="block text-xs text-slate-400 font-mono font-normal">{row.sku}</span>
                    </td>
                    <td className="px-4 py-3 text-right text-slate-500">{formatPaise(row.mrpPaise)}</td>
                    <td className="px-4 py-3 text-right">
                      {row.isListed ? formatPaise(row.pricePaise ?? row.masterPricePaise) : "—"}
                    </td>
                    <td className="px-4 py-3">
                      <span
                        className={`text-xs font-medium px-2 py-0.5 rounded-full ${
                          row.isListed ? "bg-green-50 text-green-700" : "bg-slate-100 text-slate-500"
                        }`}
                      >
                        {row.isListed ? "Listed" : "Not listed"}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-right space-x-3 text-xs font-medium">
                      <button onClick={() => setEditing(row.productId)} className="text-brand-600 hover:underline">
                        {row.isListed ? "Edit" : "List for sale"}
                      </button>
                      {row.isListed && (
                        <Link
                          to={`/admin/catalog/${row.productId}/batches`}
                          className="inline-flex items-center gap-1 text-slate-500 hover:text-slate-800"
                        >
                          <Boxes size={13} /> Stock
                        </Link>
                      )}
                    </td>
                  </tr>
                ),
              )}
              {data.data.length === 0 && (
                <tr>
                  <td colSpan={5} className="px-4 py-10 text-center text-slate-400">
                    No products match.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}

      <Link to="/" className="inline-block mt-6 text-sm text-slate-400 hover:text-slate-600">
        ← Back to storefront
      </Link>
    </div>
  );
}

function EditRow({
  row,
  onSave,
  onCancel,
  saving,
}: {
  row: BusinessCatalogRow;
  onSave: (row: BusinessCatalogRow) => void;
  onCancel: () => void;
  saving: boolean;
}) {
  const [isListed, setIsListed] = useState(row.isListed);
  const [priceRupees, setPriceRupees] = useState(((row.pricePaise ?? row.masterPricePaise) / 100).toString());
  const [isVisible, setIsVisible] = useState(row.isVisible);
  const [isFeatured, setIsFeatured] = useState(row.isFeatured);
  const [minOrderQty, setMinOrderQty] = useState(row.minOrderQty.toString());

  return (
    <tr className="bg-brand-50/40">
      <td colSpan={5} className="px-4 py-4">
        <div className="flex flex-wrap items-end gap-4 text-sm">
          <div>
            <p className="font-medium text-slate-800">{row.name}</p>
            <label className="flex items-center gap-2 mt-1.5 text-xs">
              <input type="checkbox" checked={isListed} onChange={(e) => setIsListed(e.target.checked)} />
              List for sale
            </label>
          </div>
          <label className="block">
            <span className="block text-xs font-medium text-slate-500 mb-1">Your price (₹)</span>
            <input
              type="number" step="0.01" className="input text-sm w-28"
              value={priceRupees} onChange={(e) => setPriceRupees(e.target.value)}
            />
          </label>
          <label className="block">
            <span className="block text-xs font-medium text-slate-500 mb-1">Min order qty</span>
            <input
              type="number" min={1} className="input text-sm w-20"
              value={minOrderQty} onChange={(e) => setMinOrderQty(e.target.value)}
            />
          </label>
          <label className="flex items-center gap-2 text-xs">
            <input type="checkbox" checked={isVisible} onChange={(e) => setIsVisible(e.target.checked)} />
            Visible to customers
          </label>
          <label className="flex items-center gap-2 text-xs">
            <input type="checkbox" checked={isFeatured} onChange={(e) => setIsFeatured(e.target.checked)} />
            Featured
          </label>
          <div className="flex gap-2 ml-auto">
            <button onClick={onCancel} className="text-xs font-medium text-slate-500 px-3 py-1.5">
              Cancel
            </button>
            <button
              disabled={saving}
              onClick={() =>
                onSave({
                  ...row,
                  isListed,
                  pricePaise: Math.round(parseFloat(priceRupees || "0") * 100),
                  isVisible,
                  isFeatured,
                  minOrderQty: parseInt(minOrderQty || "1", 10),
                })
              }
              className="bg-brand-600 text-white text-xs font-semibold rounded-lg px-3 py-1.5 disabled:opacity-50"
            >
              {saving ? "Saving…" : "Save"}
            </button>
          </div>
        </div>
      </td>
    </tr>
  );
}
