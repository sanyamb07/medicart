import { useState } from "react";
import { Link } from "react-router-dom";
import { useMutation } from "@tanstack/react-query";
import { CheckCircle2, Percent } from "lucide-react";
import { api, ApiError, type Envelope } from "../lib/api";
import type { Affiliate } from "../types";

export default function AffiliateApply() {
  const [form, setForm] = useState({
    fullName: "",
    email: "",
    password: "",
    phone: "",
    payoutDetails: "",
  });

  const apply = useMutation({
    mutationFn: () => api.post<Envelope<Affiliate>>("/api/v1/affiliate/apply", form),
  });

  if (apply.isSuccess) {
    return (
      <div className="min-h-screen bg-slate-950 flex items-center justify-center px-4">
        <div className="w-full max-w-sm bg-slate-900 border border-slate-800 rounded-2xl p-6 text-center">
          <CheckCircle2 className="mx-auto text-green-500 mb-3" size={36} />
          <h1 className="text-white font-bold mb-2">Application received</h1>
          <p className="text-sm text-slate-400">
            A platform admin will review your application. You'll be able to sign in once it's approved — your
            referral code is <span className="font-mono text-slate-200">{apply.data.data.code}</span>.
          </p>
          <Link to="/affiliate" className="inline-block mt-4 text-sm text-brand-400 hover:underline">
            Back to sign in
          </Link>
        </div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-slate-950 flex items-center justify-center px-4 py-10">
      <div className="w-full max-w-md">
        <div className="flex items-center gap-2 mb-6 text-white">
          <span className="h-9 w-9 rounded-xl bg-brand-600 flex items-center justify-center">
            <Percent size={18} />
          </span>
          <span className="text-lg font-bold">Become a MediCart affiliate</span>
        </div>
        <p className="text-sm text-slate-400 mb-5">
          Share your referral link, and earn a commission whenever someone buys a non-prescription product through
          it. Prescription items are never commissionable.
        </p>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            apply.mutate();
          }}
          className="bg-slate-900 border border-slate-800 rounded-2xl p-5 space-y-4"
        >
          <label className="block">
            <span className="block text-sm font-medium text-slate-300 mb-1">Full name</span>
            <input
              required
              value={form.fullName}
              onChange={(e) => setForm((f) => ({ ...f, fullName: e.target.value }))}
              className="w-full rounded-lg border border-slate-700 bg-slate-800 px-3 py-2 text-sm text-white focus:border-brand-500 focus:ring-2 focus:ring-brand-900 outline-none"
            />
          </label>
          <label className="block">
            <span className="block text-sm font-medium text-slate-300 mb-1">Email</span>
            <input
              type="email"
              required
              value={form.email}
              onChange={(e) => setForm((f) => ({ ...f, email: e.target.value }))}
              className="w-full rounded-lg border border-slate-700 bg-slate-800 px-3 py-2 text-sm text-white focus:border-brand-500 focus:ring-2 focus:ring-brand-900 outline-none"
              autoComplete="username"
            />
          </label>
          <label className="block">
            <span className="block text-sm font-medium text-slate-300 mb-1">Password</span>
            <input
              type="password"
              required
              minLength={10}
              value={form.password}
              onChange={(e) => setForm((f) => ({ ...f, password: e.target.value }))}
              className="w-full rounded-lg border border-slate-700 bg-slate-800 px-3 py-2 text-sm text-white focus:border-brand-500 focus:ring-2 focus:ring-brand-900 outline-none"
              autoComplete="new-password"
            />
            <span className="block text-xs text-slate-500 mt-1">At least 10 characters.</span>
          </label>
          <label className="block">
            <span className="block text-sm font-medium text-slate-300 mb-1">Phone (optional)</span>
            <input
              value={form.phone}
              onChange={(e) => setForm((f) => ({ ...f, phone: e.target.value }))}
              className="w-full rounded-lg border border-slate-700 bg-slate-800 px-3 py-2 text-sm text-white focus:border-brand-500 focus:ring-2 focus:ring-brand-900 outline-none"
            />
          </label>
          <label className="block">
            <span className="block text-sm font-medium text-slate-300 mb-1">Payout details (optional)</span>
            <textarea
              value={form.payoutDetails}
              onChange={(e) => setForm((f) => ({ ...f, payoutDetails: e.target.value }))}
              placeholder="UPI ID or bank details — used manually when a payout is sent"
              rows={2}
              className="w-full rounded-lg border border-slate-700 bg-slate-800 px-3 py-2 text-sm text-white focus:border-brand-500 focus:ring-2 focus:ring-brand-900 outline-none"
            />
          </label>
          {apply.isError && (
            <p className="text-sm text-red-400">
              {apply.error instanceof ApiError ? apply.error.message : "Could not submit your application."}
            </p>
          )}
          <button
            type="submit"
            disabled={apply.isPending}
            className="w-full bg-brand-600 text-white font-semibold py-2.5 rounded-lg hover:bg-brand-500 disabled:opacity-50"
          >
            {apply.isPending ? "Submitting…" : "Submit application"}
          </button>
        </form>
        <p className="text-center text-xs text-slate-500 mt-4">
          Already approved?{" "}
          <Link to="/affiliate" className="text-brand-400 hover:underline">
            Sign in
          </Link>
          .
        </p>
      </div>
    </div>
  );
}
