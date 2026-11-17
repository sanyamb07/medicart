import { Fragment, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, type Envelope } from "../../lib/api";
import { formatPaise } from "../../lib/money";
import { LEAD_STATUS_LABELS, type CrmCustomer, type LeadStatus } from "../../types";

const STATUS_STYLE: Record<LeadStatus, string> = {
  NEW: "bg-slate-100 text-slate-600",
  CONTACTED: "bg-blue-50 text-blue-700",
  INTERESTED: "bg-amber-50 text-amber-700",
  AWAITING_RESPONSE: "bg-purple-50 text-purple-700",
  CONVERTED: "bg-green-50 text-green-700",
  LOST: "bg-red-50 text-red-700",
};

export default function AdminCustomers() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [filter, setFilter] = useState("");
  const [editing, setEditing] = useState<string | null>(null);

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["crm-customers", filter],
    queryFn: () => api.get<Envelope<CrmCustomer[]>>(`/api/v1/admin/customers${filter ? `?leadStatus=${filter}` : ""}`),
    retry: false,
  });

  const update = useMutation({
    mutationFn: (row: { customerId: string; leadStatus: LeadStatus; notes: string | null }) =>
      api.put(`/api/v1/admin/customers/${row.customerId}`, { leadStatus: row.leadStatus, notes: row.notes }),
    onSuccess: () => {
      setEditing(null);
      void qc.invalidateQueries({ queryKey: ["crm-customers"] });
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
          <h1 className="text-lg font-bold text-slate-900">Customers</h1>
          <p className="text-sm text-slate-500">Everyone who has ordered or chatted with your storefront.</p>
        </div>
        <div className="flex items-center gap-3">
          <Link to="/admin/catalog" className="text-sm text-slate-400 hover:text-slate-600">Catalog</Link>
          <Link to="/admin/enquiries" className="text-sm text-slate-400 hover:text-slate-600">Enquiries</Link>
        </div>
      </div>

      <select value={filter} onChange={(e) => setFilter(e.target.value)} className="input w-auto text-sm mb-4">
        <option value="">All lead statuses</option>
        {Object.entries(LEAD_STATUS_LABELS).map(([code, label]) => (
          <option key={code} value={code}>{label}</option>
        ))}
      </select>

      {isLoading && <p className="text-slate-400">Loading…</p>}

      {data && (
        <div className="bg-white border border-slate-200 rounded-2xl overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-slate-50 text-slate-500 text-xs uppercase">
              <tr>
                <th className="text-left px-4 py-2.5">Customer</th>
                <th className="text-right px-4 py-2.5">Orders</th>
                <th className="text-right px-4 py-2.5">Lifetime value</th>
                <th className="text-left px-4 py-2.5">Lead status</th>
                <th className="px-4 py-2.5"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {data.data.map((row) => (
                <Fragment key={row.id}>
                  <tr className="hover:bg-slate-50">
                    <td className="px-4 py-3">
                      <span className="block font-medium text-slate-800">{row.fullName ?? "Unnamed customer"}</span>
                      <span className="block text-xs text-slate-400">{row.mobile ?? "—"}</span>
                    </td>
                    <td className="px-4 py-3 text-right">{row.orderCount}</td>
                    <td className="px-4 py-3 text-right">{formatPaise(row.lifetimeValuePaise)}</td>
                    <td className="px-4 py-3">
                      <span className={`text-xs font-medium px-2 py-0.5 rounded-full ${STATUS_STYLE[row.leadStatus]}`}>
                        {LEAD_STATUS_LABELS[row.leadStatus]}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-right">
                      <button
                        onClick={() => setEditing(editing === row.customerId ? null : row.customerId)}
                        className="text-xs font-medium text-brand-600 hover:underline"
                      >
                        {editing === row.customerId ? "Close" : "Update"}
                      </button>
                    </td>
                  </tr>
                  {editing === row.customerId && (
                    <tr className="bg-brand-50/40">
                      <td colSpan={5} className="px-4 py-3">
                        <EditForm
                          row={row}
                          saving={update.isPending}
                          onSave={(leadStatus, notes) => update.mutate({ customerId: row.customerId, leadStatus, notes })}
                        />
                      </td>
                    </tr>
                  )}
                </Fragment>
              ))}
              {data.data.length === 0 && (
                <tr>
                  <td colSpan={5} className="px-4 py-10 text-center text-slate-400">
                    No customers yet.
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

function EditForm({
  row,
  onSave,
  saving,
}: {
  row: CrmCustomer;
  onSave: (leadStatus: LeadStatus, notes: string | null) => void;
  saving: boolean;
}) {
  const [leadStatus, setLeadStatus] = useState<LeadStatus>(row.leadStatus);
  const [notes, setNotes] = useState(row.notes ?? "");

  return (
    <div className="flex flex-wrap items-end gap-3">
      <label className="block">
        <span className="block text-xs font-medium text-slate-500 mb-1">Lead status</span>
        <select value={leadStatus} onChange={(e) => setLeadStatus(e.target.value as LeadStatus)} className="input text-sm w-auto">
          {Object.entries(LEAD_STATUS_LABELS).map(([code, label]) => (
            <option key={code} value={code}>{label}</option>
          ))}
        </select>
      </label>
      <label className="block flex-1 min-w-[16rem]">
        <span className="block text-xs font-medium text-slate-500 mb-1">Notes</span>
        <input value={notes} onChange={(e) => setNotes(e.target.value)} className="input text-sm w-full" placeholder="Follow-up notes…" />
      </label>
      <button
        disabled={saving}
        onClick={() => onSave(leadStatus, notes || null)}
        className="bg-brand-600 text-white text-xs font-semibold rounded-lg px-4 py-2 disabled:opacity-50"
      >
        {saving ? "Saving…" : "Save"}
      </button>
    </div>
  );
}
