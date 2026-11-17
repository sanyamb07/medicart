import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useMutation } from "@tanstack/react-query";
import { Percent } from "lucide-react";
import { api, ApiError, type Envelope } from "../lib/api";
import type { Affiliate } from "../types";

export default function AffiliateLogin() {
  const navigate = useNavigate();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  const login = useMutation({
    mutationFn: () => api.post<Envelope<Affiliate>>("/api/v1/affiliate/login", { email, password }),
    onSuccess: () => navigate("/affiliate/dashboard"),
  });

  return (
    <div className="min-h-screen bg-slate-950 flex items-center justify-center px-4">
      <div className="w-full max-w-sm">
        <div className="flex items-center gap-2 mb-6 text-white">
          <span className="h-9 w-9 rounded-xl bg-brand-600 flex items-center justify-center">
            <Percent size={18} />
          </span>
          <span className="text-lg font-bold">MediCart Affiliates</span>
        </div>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            login.mutate();
          }}
          className="bg-slate-900 border border-slate-800 rounded-2xl p-5 space-y-4"
        >
          <label className="block">
            <span className="block text-sm font-medium text-slate-300 mb-1">Email</span>
            <input
              type="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className="w-full rounded-lg border border-slate-700 bg-slate-800 px-3 py-2 text-sm text-white focus:border-brand-500 focus:ring-2 focus:ring-brand-900 outline-none"
              autoComplete="username"
            />
          </label>
          <label className="block">
            <span className="block text-sm font-medium text-slate-300 mb-1">Password</span>
            <input
              type="password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="w-full rounded-lg border border-slate-700 bg-slate-800 px-3 py-2 text-sm text-white focus:border-brand-500 focus:ring-2 focus:ring-brand-900 outline-none"
              autoComplete="current-password"
            />
          </label>
          {login.isError && (
            <p className="text-sm text-red-400">
              {login.error instanceof ApiError ? login.error.message : "Sign in failed."}
            </p>
          )}
          <button
            type="submit"
            disabled={login.isPending}
            className="w-full bg-brand-600 text-white font-semibold py-2.5 rounded-lg hover:bg-brand-500 disabled:opacity-50"
          >
            {login.isPending ? "Signing in…" : "Sign in"}
          </button>
        </form>
        <p className="text-center text-xs text-slate-500 mt-4">
          Not an affiliate yet?{" "}
          <Link to="/affiliate/apply" className="text-brand-400 hover:underline">
            Apply here
          </Link>
          .
        </p>
      </div>
    </div>
  );
}
