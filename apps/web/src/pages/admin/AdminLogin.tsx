import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation } from "@tanstack/react-query";
import { api, ApiError, type Envelope } from "../../lib/api";

interface StaffUser {
  id: string;
  email: string;
  fullName: string | null;
  roles: string[];
}

export default function AdminLogin() {
  const navigate = useNavigate();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  const login = useMutation({
    mutationFn: () => api.post<Envelope<StaffUser>>("/api/v1/staff/login", { email, password }),
    onSuccess: () => navigate("/admin/orders"),
  });

  return (
    <div className="max-w-sm mx-auto px-4 py-16">
      <h1 className="text-xl font-bold text-slate-900 mb-1">Staff sign in</h1>
      <p className="text-slate-500 text-sm mb-6">MediCart admin dashboard</p>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          login.mutate();
        }}
        className="bg-white border border-slate-200 rounded-2xl p-5 space-y-4"
      >
        <label className="block">
          <span className="block text-sm font-medium text-slate-700 mb-1">Email</span>
          <input
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="input"
            autoComplete="username"
          />
        </label>
        <label className="block">
          <span className="block text-sm font-medium text-slate-700 mb-1">Password</span>
          <input
            type="password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="input"
            autoComplete="current-password"
          />
        </label>
        {login.isError && (
          <p className="text-sm text-red-600">
            {login.error instanceof ApiError ? login.error.message : "Sign in failed."}
          </p>
        )}
        <button
          type="submit"
          disabled={login.isPending}
          className="w-full bg-brand-600 text-white font-semibold py-2.5 rounded-full hover:bg-brand-700 disabled:bg-slate-300"
        >
          {login.isPending ? "Signing in…" : "Sign in"}
        </button>
      </form>
    </div>
  );
}
