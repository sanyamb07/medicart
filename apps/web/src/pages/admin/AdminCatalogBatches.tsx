import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Ban, CheckCircle2, PackagePlus } from "lucide-react";
import { api, ApiError, type Envelope } from "../../lib/api";
import type { Batch } from "../../types";

export default function AdminCatalogBatches() {
  const { productId } = useParams();
  const qc = useQueryClient();
  const [receiving, setReceiving] = useState(false);
  const [batchNumber, setBatchNumber] = useState("");
  const [expiryDate, setExpiryDate] = useState("");
  const [qty, setQty] = useState("");
  const [receiveError, setReceiveError] = useState<string | null>(null);

  const { data } = useQuery({
    queryKey: ["admin-batches", productId],
    queryFn: () => api.get<Envelope<Batch[]>>(`/api/v1/admin/products/${productId}/batches`),
  });

  const receive = useMutation({
    mutationFn: () =>
      api.post(`/api/v1/admin/products/${productId}/batches`, {
        batchNumber, expiryDate, qty: parseInt(qty || "0", 10),
      }),
    onSuccess: () => {
      setReceiving(false);
      setBatchNumber("");
      setExpiryDate("");
      setQty("");
      setReceiveError(null);
      void qc.invalidateQueries({ queryKey: ["admin-batches", productId] });
    },
    onError: (e) => setReceiveError(e instanceof ApiError ? e.message : "Could not record this batch."),
  });

  const toggleBlock = useMutation({
    mutationFn: ({ batchId, block }: { batchId: string; block: boolean }) =>
      api.post(`/api/v1/admin/batches/${batchId}/${block ? "block" : "unblock"}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["admin-batches", productId] }),
  });

  return (
    <div className="max-w-3xl mx-auto px-4 py-8">
      <h1 className="text-lg font-bold text-slate-900 mb-5">Stock batches</h1>

      <div className="flex items-center justify-between mb-3">
        <p className="text-sm text-slate-500">Your own stock of this product — never shared with another business.</p>
        <button
          onClick={() => setReceiving((r) => !r)}
          className="inline-flex items-center gap-1.5 text-sm font-medium text-brand-600 shrink-0"
        >
          <PackagePlus size={15} /> Receive stock
        </button>
      </div>

      {receiving && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            receive.mutate();
          }}
          className="bg-white border border-slate-200 rounded-xl p-4 mb-4 grid grid-cols-3 gap-3 items-end"
        >
          <Field label="Batch / lot number">
            <input className="input text-sm" value={batchNumber} onChange={(e) => setBatchNumber(e.target.value)} required />
          </Field>
          <Field label="Expiry date">
            <input className="input text-sm" type="date" value={expiryDate} onChange={(e) => setExpiryDate(e.target.value)} required />
          </Field>
          <Field label="Quantity received">
            <input className="input text-sm" type="number" min={1} value={qty} onChange={(e) => setQty(e.target.value)} required />
          </Field>
          {receiveError && <p className="col-span-3 text-sm text-red-600">{receiveError}</p>}
          <button type="submit" disabled={receive.isPending} className="col-span-3 bg-brand-600 text-white text-sm font-semibold rounded-lg py-2 disabled:opacity-50">
            {receive.isPending ? "Recording…" : "Record batch"}
          </button>
        </form>
      )}

      <div className="bg-white border border-slate-200 rounded-2xl overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-slate-50 text-slate-500 text-xs uppercase">
            <tr>
              <th className="text-left px-4 py-2.5">Batch</th>
              <th className="text-left px-4 py-2.5">Expiry</th>
              <th className="text-right px-4 py-2.5">On hand</th>
              <th className="text-right px-4 py-2.5">Available</th>
              <th className="text-left px-4 py-2.5">Status</th>
              <th className="px-4 py-2.5"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100">
            {data?.data.map((b) => (
              <tr key={b.id}>
                <td className="px-4 py-3 font-mono text-xs">{b.batchNumber}</td>
                <td className="px-4 py-3">
                  {new Date(b.expiryDate).toLocaleDateString()}
                  {b.isExpired && (
                    <span className="ml-2 inline-flex items-center gap-1 text-red-600 text-[11px] font-semibold">
                      <AlertTriangle size={11} /> Expired
                    </span>
                  )}
                </td>
                <td className="px-4 py-3 text-right">{b.qtyOnHand}</td>
                <td className="px-4 py-3 text-right">{b.qtyAvailable}</td>
                <td className="px-4 py-3">
                  {b.isBlocked ? (
                    <span className="bg-red-50 text-red-700 text-xs font-medium px-2 py-0.5 rounded-full">
                      Blocked{b.blockedReason ? `: ${b.blockedReason}` : ""}
                    </span>
                  ) : (
                    <span className="bg-green-50 text-green-700 text-xs font-medium px-2 py-0.5 rounded-full">Sellable</span>
                  )}
                </td>
                <td className="px-4 py-3 text-right">
                  <button
                    onClick={() => toggleBlock.mutate({ batchId: b.id, block: !b.isBlocked })}
                    className="inline-flex items-center gap-1 text-xs font-medium text-slate-500 hover:text-slate-800"
                  >
                    {b.isBlocked ? <><CheckCircle2 size={13} /> Unblock</> : <><Ban size={13} /> Block</>}
                  </button>
                </td>
              </tr>
            ))}
            {data && data.data.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-slate-400">
                  No stock received yet.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      <Link to="/admin/catalog" className="inline-block mt-6 text-sm text-slate-400 hover:text-slate-600">
        ← Back to catalog
      </Link>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="block text-xs font-medium text-slate-500 mb-1">{label}</span>
      {children}
    </label>
  );
}
