import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, type Envelope } from "../../lib/api";
import { formatPaise } from "../../lib/money";
import type { Affiliate, AffiliateSettings, AdminReferral, AffiliateStatus, ReferralStatus } from "../../types";
import PlatformShell from "./PlatformShell";

const AFFILIATE_STATUS_STYLE: Record<AffiliateStatus, string> = {
  ACTIVE: "bg-green-50 text-green-700",
  PENDING: "bg-amber-50 text-amber-700",
  SUSPENDED: "bg-red-50 text-red-700",
  REJECTED: "bg-slate-100 text-slate-500",
};

const REFERRAL_STATUS_STYLE: Record<ReferralStatus, string> = {
  CONFIRMED: "bg-blue-50 text-blue-700",
  PENDING: "bg-amber-50 text-amber-700",
  PAID: "bg-green-50 text-green-700",
  VOID: "bg-slate-100 text-slate-400",
};

export default function PlatformAffiliates() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [rateInput, setRateInput] = useState<string | null>(null);

  const affiliatesQ = useQuery({
    queryKey: ["platform-affiliates"],
    queryFn: () => api.get<Envelope<Affiliate[]>>("/api/v1/platform/affiliates"),
    retry: false,
  });

  const referralsQ = useQuery({
    queryKey: ["platform-affiliate-referrals"],
    queryFn: () => api.get<Envelope<AdminReferral[]>>("/api/v1/platform/affiliates/referrals"),
    enabled: !affiliatesQ.isError,
  });

  const settingsQ = useQuery({
    queryKey: ["platform-affiliate-settings"],
    queryFn: () => api.get<Envelope<AffiliateSettings>>("/api/v1/platform/affiliates/settings"),
    enabled: !affiliatesQ.isError,
  });

  const decide = useMutation({
    mutationFn: ({ id, action }: { id: string; action: "approve" | "suspend" }) =>
      api.post(`/api/v1/platform/affiliates/${id}/${action}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["platform-affiliates"] }),
  });

  const reject = useMutation({
    mutationFn: ({ id, reason }: { id: string; reason: string }) =>
      api.post(`/api/v1/platform/affiliates/${id}/reject`, { reason }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["platform-affiliates"] }),
  });

  const markPaid = useMutation({
    mutationFn: (referralIds: string[]) =>
      api.post("/api/v1/platform/affiliates/referrals/mark-paid", { referralIds }),
    onSuccess: () => {
      setSelected(new Set());
      void qc.invalidateQueries({ queryKey: ["platform-affiliate-referrals"] });
    },
  });

  const setRate = useMutation({
    mutationFn: (commissionRateBps: number) =>
      api.put<Envelope<AffiliateSettings>>("/api/v1/platform/affiliates/settings", { commissionRateBps }),
    onSuccess: () => {
      setRateInput(null);
      void qc.invalidateQueries({ queryKey: ["platform-affiliate-settings"] });
    },
  });

  if (affiliatesQ.isError && affiliatesQ.error instanceof ApiError && affiliatesQ.error.status === 401) {
    navigate("/platform");
    return null;
  }

  const affiliates = affiliatesQ.data?.data ?? [];
  const referrals = referralsQ.data?.data ?? [];
  const currentRateBps = settingsQ.data?.data.commissionRateBps;

  function toggle(id: string) {
    setSelected((s) => {
      const next = new Set(s);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  return (
    <PlatformShell>
      <div className="max-w-6xl mx-auto px-6 py-8 space-y-8">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-lg font-bold text-slate-900">Affiliates</h1>
            <p className="text-sm text-slate-500">
              External promoters earning commission on non-prescription order referrals.
            </p>
          </div>
          {currentRateBps !== undefined && (
            <div className="flex items-center gap-2 bg-white border border-slate-200 rounded-xl px-4 py-2">
              <span className="text-xs text-slate-500">Flat commission rate</span>
              {rateInput === null ? (
                <button
                  onClick={() => setRateInput((currentRateBps / 100).toString())}
                  className="text-sm font-semibold text-brand-700 hover:underline"
                >
                  {(currentRateBps / 100).toFixed(2)}%
                </button>
              ) : (
                <form
                  onSubmit={(e) => {
                    e.preventDefault();
                    const pct = parseFloat(rateInput);
                    if (!isNaN(pct)) setRate.mutate(Math.round(pct * 100));
                  }}
                  className="flex items-center gap-1.5"
                >
                  <input
                    autoFocus
                    type="number"
                    step="0.1"
                    min="0"
                    max="50"
                    value={rateInput}
                    onChange={(e) => setRateInput(e.target.value)}
                    className="w-16 text-sm rounded-lg border border-slate-300 px-2 py-1"
                  />
                  <span className="text-xs text-slate-500">%</span>
                  <button type="submit" className="text-xs font-semibold text-brand-700">
                    Save
                  </button>
                </form>
              )}
            </div>
          )}
        </div>

        <section className="bg-white border border-slate-200 rounded-2xl overflow-hidden">
          <div className="px-4 py-3 border-b border-slate-100">
            <h2 className="text-sm font-bold text-slate-800">Applications</h2>
          </div>
          <table className="w-full text-sm">
            <thead className="bg-slate-50 text-slate-500 text-xs uppercase">
              <tr>
                <th className="text-left px-4 py-2.5">Name</th>
                <th className="text-left px-4 py-2.5">Email</th>
                <th className="text-left px-4 py-2.5">Code</th>
                <th className="text-left px-4 py-2.5">Status</th>
                <th className="px-4 py-2.5"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {affiliates.map((a) => (
                <tr key={a.id}>
                  <td className="px-4 py-3 font-medium text-slate-800">{a.fullName}</td>
                  <td className="px-4 py-3 text-slate-500">{a.email}</td>
                  <td className="px-4 py-3 font-mono text-xs text-slate-500">{a.code}</td>
                  <td className="px-4 py-3">
                    <span className={`text-xs font-medium px-2 py-0.5 rounded-full ${AFFILIATE_STATUS_STYLE[a.status]}`}>
                      {a.status}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-right space-x-3 text-xs font-medium">
                    {a.status !== "ACTIVE" && (
                      <button
                        onClick={() => decide.mutate({ id: a.id, action: "approve" })}
                        className="text-green-700 hover:underline"
                      >
                        Approve
                      </button>
                    )}
                    {a.status === "ACTIVE" && (
                      <button
                        onClick={() => decide.mutate({ id: a.id, action: "suspend" })}
                        className="text-red-600 hover:underline"
                      >
                        Suspend
                      </button>
                    )}
                    {a.status === "PENDING" && (
                      <button
                        onClick={() => {
                          const reason = window.prompt("Reason for rejecting this application?");
                          if (reason) reject.mutate({ id: a.id, reason });
                        }}
                        className="text-slate-500 hover:underline"
                      >
                        Reject
                      </button>
                    )}
                  </td>
                </tr>
              ))}
              {affiliates.length === 0 && (
                <tr>
                  <td colSpan={5} className="px-4 py-10 text-center text-slate-400">
                    No affiliate applications yet.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </section>

        <section className="bg-white border border-slate-200 rounded-2xl overflow-hidden">
          <div className="px-4 py-3 border-b border-slate-100 flex items-center justify-between">
            <h2 className="text-sm font-bold text-slate-800">Commission ledger</h2>
            <button
              disabled={selected.size === 0 || markPaid.isPending}
              onClick={() => markPaid.mutate([...selected])}
              className="text-xs font-semibold text-white bg-brand-600 px-3 py-1.5 rounded-lg disabled:opacity-40"
            >
              Mark {selected.size || ""} paid
            </button>
          </div>
          <table className="w-full text-sm">
            <thead className="bg-slate-50 text-slate-500 text-xs uppercase">
              <tr>
                <th className="px-4 py-2.5"></th>
                <th className="text-left px-4 py-2.5">Affiliate</th>
                <th className="text-left px-4 py-2.5">Order</th>
                <th className="text-left px-4 py-2.5">Eligible subtotal</th>
                <th className="text-left px-4 py-2.5">Commission</th>
                <th className="text-left px-4 py-2.5">Status</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {referrals.map((r) => (
                <tr key={r.id}>
                  <td className="px-4 py-3">
                    {r.status === "CONFIRMED" && (
                      <input
                        type="checkbox"
                        checked={selected.has(r.id)}
                        onChange={() => toggle(r.id)}
                        className="rounded border-slate-300"
                      />
                    )}
                  </td>
                  <td className="px-4 py-3">
                    <div className="font-medium text-slate-800">{r.affiliateName}</div>
                    <div className="font-mono text-xs text-slate-400">{r.affiliateCode}</div>
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-slate-500">{r.orderReference}</td>
                  <td className="px-4 py-3 text-slate-600">{formatPaise(r.eligibleSubtotalPaise)}</td>
                  <td className="px-4 py-3 font-semibold text-slate-800">{formatPaise(r.commissionPaise)}</td>
                  <td className="px-4 py-3">
                    <span className={`text-xs font-medium px-2 py-0.5 rounded-full ${REFERRAL_STATUS_STYLE[r.status]}`}>
                      {r.status}
                    </span>
                  </td>
                </tr>
              ))}
              {referrals.length === 0 && (
                <tr>
                  <td colSpan={6} className="px-4 py-10 text-center text-slate-400">
                    No referrals recorded yet.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </section>
      </div>
    </PlatformShell>
  );
}
