import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, ExternalLink, FileText, XCircle } from "lucide-react";
import { api, ApiError, type Envelope } from "../../lib/api";
import type { Prescription } from "../../types";

export default function AdminPrescriptions() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [busyId, setBusyId] = useState<string | null>(null);
  const [notes, setNotes] = useState<Record<string, string>>({});

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["admin-prescriptions-pending"],
    queryFn: () => api.get<Envelope<Prescription[]>>("/api/v1/admin/prescriptions/pending"),
    retry: false,
  });

  if (isError && error instanceof ApiError && error.status === 401) {
    navigate("/admin");
    return null;
  }

  async function viewFile(id: string) {
    const res = await api.get<Envelope<{ url: string }>>(`/api/v1/admin/prescriptions/${id}/url`);
    window.open(res.data.url, "_blank", "noopener,noreferrer");
  }

  async function decide(id: string, decision: "APPROVED" | "REJECTED") {
    setBusyId(id);
    try {
      await api.post(`/api/v1/admin/prescriptions/${id}/review`, { decision, notes: notes[id] || null });
      await queryClient.invalidateQueries({ queryKey: ["admin-prescriptions-pending"] });
    } finally {
      setBusyId(null);
    }
  }

  return (
    <div className="max-w-3xl mx-auto px-4 py-8">
      <h1 className="text-lg font-bold text-slate-900 mb-1">Prescriptions awaiting review</h1>
      <p className="text-sm text-slate-500 mb-5">
        Open the file, confirm it's a genuine, legible prescription for the right customer, then approve or reject.
        This queue is independent of order checkout — prescription-only medicines still cannot be sold while
        sales are disabled by configuration.
      </p>

      {isLoading && <p className="text-slate-400">Loading…</p>}

      {data && (data.data ?? []).length === 0 && (
        <div className="bg-white border border-slate-200 rounded-2xl p-10 text-center text-slate-400">
          Nothing pending review.
        </div>
      )}

      <div className="space-y-3">
        {(data?.data ?? []).map((rx) => (
          <div key={rx.id} className="bg-white border border-slate-200 rounded-2xl p-4">
            <div className="flex items-center justify-between gap-4 mb-3">
              <div className="flex items-center gap-2">
                <FileText size={18} className="text-slate-400" />
                <div>
                  <div className="text-sm font-medium text-slate-800">{rx.originalName ?? "Prescription"}</div>
                  <div className="text-xs text-slate-400">
                    Uploaded {new Date(rx.uploadedAt).toLocaleString()} · {(rx.sizeBytes / 1024).toFixed(0)} KB
                  </div>
                </div>
              </div>
              <button
                onClick={() => void viewFile(rx.id)}
                className="inline-flex items-center gap-1 text-xs font-medium text-brand-600 border border-brand-200 rounded-full px-3 py-1.5 hover:bg-brand-50"
              >
                <ExternalLink size={12} /> Open file
              </button>
            </div>
            <input
              type="text"
              placeholder="Notes shown to the customer (optional)"
              className="input text-sm w-full mb-2"
              value={notes[rx.id] ?? ""}
              onChange={(e) => setNotes((n) => ({ ...n, [rx.id]: e.target.value }))}
            />
            <div className="flex gap-2">
              <button
                disabled={busyId === rx.id}
                onClick={() => void decide(rx.id, "APPROVED")}
                className="flex-1 inline-flex items-center justify-center gap-1.5 bg-green-600 text-white text-sm font-semibold rounded-lg py-2 disabled:opacity-50"
              >
                <CheckCircle2 size={14} /> Approve
              </button>
              <button
                disabled={busyId === rx.id}
                onClick={() => void decide(rx.id, "REJECTED")}
                className="flex-1 inline-flex items-center justify-center gap-1.5 bg-red-50 text-red-700 text-sm font-semibold rounded-lg py-2 disabled:opacity-50"
              >
                <XCircle size={14} /> Reject
              </button>
            </div>
          </div>
        ))}
      </div>

      <Link to="/admin/orders" className="inline-block mt-6 text-sm text-slate-400 hover:text-slate-600">
        ← Back to orders
      </Link>
    </div>
  );
}
