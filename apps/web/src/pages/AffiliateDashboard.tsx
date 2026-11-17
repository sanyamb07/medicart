import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Copy, LogOut, Percent } from "lucide-react";
import { api, ApiError, type Envelope } from "../lib/api";
import { formatPaise } from "../lib/money";
import type { Affiliate, Referral, ReferralStatus } from "../types";

const STATUS_STYLE: Record<ReferralStatus, string> = {
  CONFIRMED: "bg-blue-50 text-blue-700",
  PENDING: "bg-amber-50 text-amber-700",
  PAID: "bg-green-50 text-green-700",
  VOID: "bg-slate-100 text-slate-400",
};

interface Summary {
  pendingPaise: number;
  confirmedPaise: number;
  paidPaise: number;
  referralCount: number;
}

export default function AffiliateDashboard() {
  const navigate = useNavigate();
  const [copied, setCopied] = useState(false);

  const summaryQ = useQuery({
    queryKey: ["affiliate-summary"],
    queryFn: () => api.get<Envelope<{ affiliate: Affiliate; summary: Summary }>>("/api/v1/affiliate/summary"),
    retry: false,
  });

  const referralsQ = useQuery({
    queryKey: ["affiliate-referrals"],
    queryFn: () => api.get<Envelope<Referral[]>>("/api/v1/affiliate/referrals"),
    enabled: !summaryQ.isError,
  });

  const logout = useMutation({
    mutationFn: () => api.post("/api/v1/affiliate/logout"),
    onSuccess: () => navigate("/affiliate"),
  });

  if (summaryQ.isError && summaryQ.error instanceof ApiError && summaryQ.error.status === 401) {
    navigate("/affiliate");
    return null;
  }

  const affiliate = summaryQ.data?.data.affiliate;
  const summary = summaryQ.data?.data.summary;
  const referrals = referralsQ.data?.data ?? [];
  const referralLink = affiliate ? `${window.location.origin}/?ref=${affiliate.code}` : "";

  return (
    <div className="min-h-screen bg-slate-100">
      <header className="bg-slate-950 text-white">
        <div className="max-w-5xl mx-auto px-6 py-4 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <span className="h-8 w-8 rounded-lg bg-brand-600 flex items-center justify-center">
              <Percent size={16} />
            </span>
            <span className="font-bold text-sm">MediCart Affiliates</span>
          </div>
          <button onClick={() => logout.mutate()} className="flex items-center gap-1.5 text-sm text-slate-300 hover:text-white">
            <LogOut size={14} /> Sign out
          </button>
        </div>
      </header>

      <main className="max-w-5xl mx-auto px-6 py-8 space-y-6">
        {affiliate && (
          <div className="bg-white border border-slate-200 rounded-2xl p-5">
            <h1 className="text-lg font-bold text-slate-900 mb-1">Welcome, {affiliate.fullName}</h1>
            <p className="text-sm text-slate-500 mb-4">
              Share your referral link. You earn commission only on non-prescription items in any order placed
              through it.
            </p>
            <div className="flex items-center gap-2 bg-slate-50 border border-slate-200 rounded-xl px-3 py-2.5">
              <code className="flex-1 text-sm text-slate-700 truncate">{referralLink}</code>
              <button
                onClick={async () => {
                  await navigator.clipboard.writeText(referralLink);
                  setCopied(true);
                  setTimeout(() => setCopied(false), 1500);
                }}
                className="flex items-center gap-1.5 text-xs font-semibold text-brand-700 shrink-0"
              >
                <Copy size={13} /> {copied ? "Copied!" : "Copy"}
              </button>
            </div>
          </div>
        )}

        {summary && (
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
            <StatTile label="Pending" value={formatPaise(summary.pendingPaise)} />
            <StatTile label="Confirmed" value={formatPaise(summary.confirmedPaise)} />
            <StatTile label="Paid out" value={formatPaise(summary.paidPaise)} highlight />
            <StatTile label="Referrals" value={String(summary.referralCount)} />
          </div>
        )}

        <section className="bg-white border border-slate-200 rounded-2xl overflow-hidden">
          <div className="px-4 py-3 border-b border-slate-100">
            <h2 className="text-sm font-bold text-slate-800">Your referrals</h2>
          </div>
          <table className="w-full text-sm">
            <thead className="bg-slate-50 text-slate-500 text-xs uppercase">
              <tr>
                <th className="text-left px-4 py-2.5">Order</th>
                <th className="text-left px-4 py-2.5">Eligible subtotal</th>
                <th className="text-left px-4 py-2.5">Commission</th>
                <th className="text-left px-4 py-2.5">Status</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {referrals.map((r) => (
                <tr key={r.id}>
                  <td className="px-4 py-3 font-mono text-xs text-slate-500">{r.orderReference}</td>
                  <td className="px-4 py-3 text-slate-600">{formatPaise(r.eligibleSubtotalPaise)}</td>
                  <td className="px-4 py-3 font-semibold text-slate-800">{formatPaise(r.commissionPaise)}</td>
                  <td className="px-4 py-3">
                    <span className={`text-xs font-medium px-2 py-0.5 rounded-full ${STATUS_STYLE[r.status]}`}>
                      {r.status}
                    </span>
                  </td>
                </tr>
              ))}
              {referrals.length === 0 && (
                <tr>
                  <td colSpan={4} className="px-4 py-10 text-center text-slate-400">
                    No referrals yet — share your link above to get started.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </section>
      </main>
    </div>
  );
}

function StatTile({ label, value, highlight }: { label: string; value: string; highlight?: boolean }) {
  return (
    <div className="bg-white border border-slate-200 rounded-2xl p-4">
      <div className="text-xs text-slate-500 mb-1">{label}</div>
      <div className={`text-lg font-bold ${highlight ? "text-green-700" : "text-slate-900"}`}>{value}</div>
    </div>
  );
}
