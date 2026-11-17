import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api, ApiError, type Envelope } from "../../lib/api";
import { formatPaise } from "../../lib/money";
import { STATUS_LABELS, type OrderListItem } from "../../types";

export default function AdminOrders() {
  const navigate = useNavigate();
  const [status, setStatus] = useState("");

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["admin-orders", status],
    queryFn: () =>
      api.get<Envelope<OrderListItem[]>>(`/api/v1/admin/orders${status ? `?status=${status}` : ""}`),
    retry: false,
  });

  if (isError && error instanceof ApiError && error.status === 401) {
    navigate("/admin");
    return null;
  }

  return (
    <div className="max-w-5xl mx-auto px-4 py-8">
      <div className="flex items-center justify-between mb-5">
        <h1 className="text-lg font-bold text-slate-900">Orders</h1>
        <div className="flex items-center gap-3">
          <Link to="/admin/catalog" className="text-sm font-medium text-brand-600 hover:underline">
            Catalog
          </Link>
          <Link to="/admin/prescriptions" className="text-sm font-medium text-brand-600 hover:underline">
            Prescription review
          </Link>
          <Link to="/admin/customers" className="text-sm font-medium text-brand-600 hover:underline">
            Customers
          </Link>
          <Link to="/admin/enquiries" className="text-sm font-medium text-brand-600 hover:underline">
            Enquiries
          </Link>
          <select value={status} onChange={(e) => setStatus(e.target.value)} className="input w-auto text-sm">
            <option value="">All statuses</option>
            {Object.keys(STATUS_LABELS).map((s) => (
              <option key={s} value={s}>
                {STATUS_LABELS[s]}
              </option>
            ))}
          </select>
        </div>
      </div>

      {isLoading && <p className="text-slate-400">Loading…</p>}

      {data && (
        <div className="bg-white border border-slate-200 rounded-2xl overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-slate-50 text-slate-500 text-xs uppercase">
              <tr>
                <th className="text-left px-4 py-2.5">Reference</th>
                <th className="text-left px-4 py-2.5">Customer</th>
                <th className="text-left px-4 py-2.5">Status</th>
                <th className="text-right px-4 py-2.5">Total</th>
                <th className="text-left px-4 py-2.5">Placed</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {data.data.map((o) => (
                <tr
                  key={o.id}
                  className="hover:bg-slate-50 cursor-pointer"
                  onClick={() => navigate(`/admin/orders/${o.id}`)}
                >
                  <td className="px-4 py-3 font-mono text-xs">{o.reference}</td>
                  <td className="px-4 py-3">{o.recipientName}</td>
                  <td className="px-4 py-3">
                    <span className="bg-slate-100 text-slate-700 text-xs font-medium px-2 py-0.5 rounded-full">
                      {STATUS_LABELS[o.status] ?? o.status}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-right font-medium">{formatPaise(o.totalPaise)}</td>
                  <td className="px-4 py-3 text-slate-400">{new Date(o.placedAt).toLocaleString()}</td>
                </tr>
              ))}
              {data.data.length === 0 && (
                <tr>
                  <td colSpan={5} className="px-4 py-10 text-center text-slate-400">
                    No orders found.
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
