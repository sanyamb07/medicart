import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { api, ApiError, type Envelope } from "../../lib/api";
import type { Business } from "../../types";
import PlatformShell from "./PlatformShell";

const STATUS_STYLE: Record<Business["status"], string> = {
  ACTIVE: "bg-green-50 text-green-700",
  PENDING: "bg-amber-50 text-amber-700",
  SUSPENDED: "bg-red-50 text-red-700",
  REJECTED: "bg-slate-100 text-slate-500",
};

export default function PlatformBusinesses() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [showNew, setShowNew] = useState(false);
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [formError, setFormError] = useState<string | null>(null);

  const { data, isError, error } = useQuery({
    queryKey: ["platform-businesses"],
    queryFn: () => api.get<Envelope<Business[]>>("/api/v1/platform/businesses"),
    retry: false,
  });

  const create = useMutation({
    mutationFn: () => api.post<Envelope<Business>>("/api/v1/platform/businesses", { name, slug }),
    onSuccess: () => {
      setShowNew(false);
      setName("");
      setSlug("");
      setFormError(null);
      void qc.invalidateQueries({ queryKey: ["platform-businesses"] });
    },
    onError: (e) => setFormError(e instanceof ApiError ? e.message : "Could not create business."),
  });

  const setStatus = useMutation({
    mutationFn: ({ id, status }: { id: string; status: string }) =>
      api.post(`/api/v1/platform/businesses/${id}/status`, { status }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["platform-businesses"] }),
  });

  if (isError && error instanceof ApiError && error.status === 401) {
    navigate("/platform");
    return null;
  }

  return (
    <PlatformShell>
      <div className="max-w-5xl mx-auto px-6 py-8">
        <div className="flex items-center justify-between mb-6">
          <div>
            <h1 className="text-lg font-bold text-slate-900">Businesses</h1>
            <p className="text-sm text-slate-500">Every pharmacy registered on the platform.</p>
          </div>
          <button
            onClick={() => setShowNew((s) => !s)}
            className="inline-flex items-center gap-1.5 bg-brand-600 text-white text-sm font-semibold px-4 py-2 rounded-lg hover:bg-brand-700"
          >
            <Plus size={15} /> New business
          </button>
        </div>

        {showNew && (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              create.mutate();
            }}
            className="bg-white border border-slate-200 rounded-2xl p-5 mb-6 grid grid-cols-2 gap-3 items-end"
          >
            <label className="block">
              <span className="block text-xs font-medium text-slate-500 mb-1">Business name</span>
              <input className="input text-sm" value={name} onChange={(e) => setName(e.target.value)} required />
            </label>
            <label className="block">
              <span className="block text-xs font-medium text-slate-500 mb-1">URL slug (optional — derived from name)</span>
              <input className="input text-sm font-mono" value={slug} onChange={(e) => setSlug(e.target.value)} />
            </label>
            {formError && <p className="col-span-2 text-sm text-red-600">{formError}</p>}
            <button
              type="submit"
              disabled={create.isPending}
              className="col-span-2 bg-brand-600 text-white text-sm font-semibold rounded-lg py-2 disabled:opacity-50"
            >
              {create.isPending ? "Creating…" : "Create (starts PENDING)"}
            </button>
          </form>
        )}

        <div className="bg-white border border-slate-200 rounded-2xl overflow-hidden">
          <table className="w-full text-sm">
            <thead className="bg-slate-50 text-slate-500 text-xs uppercase">
              <tr>
                <th className="text-left px-4 py-2.5">Business</th>
                <th className="text-left px-4 py-2.5">Storefront</th>
                <th className="text-left px-4 py-2.5">Status</th>
                <th className="text-left px-4 py-2.5">License</th>
                <th className="px-4 py-2.5"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {data?.data.map((b) => (
                <tr key={b.id}>
                  <td className="px-4 py-3 font-medium text-slate-800">{b.name}</td>
                  <td className="px-4 py-3 font-mono text-xs text-slate-500">/{b.slug}</td>
                  <td className="px-4 py-3">
                    <span className={`text-xs font-medium px-2 py-0.5 rounded-full ${STATUS_STYLE[b.status]}`}>
                      {b.status}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-xs text-slate-500">{b.licenseStatus}</td>
                  <td className="px-4 py-3 text-right space-x-3 text-xs font-medium">
                    {b.status !== "ACTIVE" && (
                      <button
                        onClick={() => setStatus.mutate({ id: b.id, status: "ACTIVE" })}
                        className="text-green-700 hover:underline"
                      >
                        Approve
                      </button>
                    )}
                    {b.status !== "SUSPENDED" && (
                      <button
                        onClick={() => setStatus.mutate({ id: b.id, status: "SUSPENDED" })}
                        className="text-red-600 hover:underline"
                      >
                        Suspend
                      </button>
                    )}
                    {b.status === "PENDING" && (
                      <button
                        onClick={() => setStatus.mutate({ id: b.id, status: "REJECTED" })}
                        className="text-slate-500 hover:underline"
                      >
                        Reject
                      </button>
                    )}
                  </td>
                </tr>
              ))}
              {data && data.data.length === 0 && (
                <tr>
                  <td colSpan={5} className="px-4 py-10 text-center text-slate-400">
                    No businesses yet.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </PlatformShell>
  );
}
