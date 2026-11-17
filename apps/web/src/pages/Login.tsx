import { useState } from "react";
import { useNavigate, useLocation } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ShieldCheck, Smartphone } from "lucide-react";
import { api, ApiError, type Envelope } from "../lib/api";
import type { CustomerUser } from "../hooks/useAuth";

interface OtpStartResp {
  expiresInSeconds: number;
  devCode?: string;
  deliveryNote: string;
}

export default function Login() {
  const navigate = useNavigate();
  const location = useLocation();
  const qc = useQueryClient();
  const returnTo = (location.state as { from?: string } | null)?.from ?? "/";

  const [mobile, setMobile] = useState("");
  const [code, setCode] = useState("");
  const [stage, setStage] = useState<"mobile" | "code">("mobile");
  const [devCode, setDevCode] = useState<string | null>(null);

  const startOtp = useMutation({
    mutationFn: () => api.post<Envelope<OtpStartResp>>("/api/v1/auth/otp/start", { mobile }),
    onSuccess: (res) => {
      setStage("code");
      setDevCode(res.data.devCode ?? null);
    },
  });

  const verifyOtp = useMutation({
    mutationFn: () => api.post<Envelope<CustomerUser>>("/api/v1/auth/otp/verify", { mobile, code }),
    onSuccess: (res) => {
      qc.setQueryData(["me"], { data: res.data });
      navigate(returnTo, { replace: true });
    },
  });

  return (
    <div className="max-w-sm mx-auto px-4 py-16">
      <div className="text-center mb-6">
        <div className="inline-flex h-11 w-11 items-center justify-center rounded-2xl bg-brand-50 text-brand-600 mb-3">
          <Smartphone size={22} />
        </div>
        <h1 className="text-xl font-bold text-slate-900">Sign in to MediCart</h1>
        <p className="text-slate-500 text-sm mt-1">We'll send a one-time code to your mobile number.</p>
      </div>

      <div className="bg-white border border-slate-200 rounded-2xl p-5">
        {stage === "mobile" ? (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              startOtp.mutate();
            }}
            className="space-y-4"
          >
            <label className="block">
              <span className="block text-sm font-medium text-slate-700 mb-1">Mobile number</span>
              <div className="flex items-center gap-2 input !flex">
                <span className="text-slate-400 text-sm">+91</span>
                <input
                  required
                  autoFocus
                  inputMode="numeric"
                  value={mobile}
                  onChange={(e) => setMobile(e.target.value.replace(/\D/g, "").slice(0, 10))}
                  placeholder="98765 43210"
                  className="flex-1 border-0 p-0 bg-transparent focus:ring-0 text-sm"
                />
              </div>
            </label>
            {startOtp.isError && (
              <p className="text-sm text-red-600">
                {startOtp.error instanceof ApiError ? startOtp.error.message : "Could not send a code."}
              </p>
            )}
            <button
              type="submit"
              disabled={mobile.length !== 10 || startOtp.isPending}
              className="w-full bg-brand-600 text-white font-semibold py-2.5 rounded-full hover:bg-brand-700 disabled:bg-slate-300"
            >
              {startOtp.isPending ? "Sending…" : "Send code"}
            </button>
          </form>
        ) : (
          <form
            onSubmit={(e) => {
              e.preventDefault();
              verifyOtp.mutate();
            }}
            className="space-y-4"
          >
            <p className="text-sm text-slate-600">
              Enter the 6-digit code sent to <strong>+91 {mobile}</strong>.
            </p>
            {devCode && (
              <div className="flex items-center gap-2 bg-amber-50 border border-amber-200 text-amber-800 text-xs rounded-lg px-3 py-2">
                <ShieldCheck size={14} className="shrink-0" />
                No SMS provider configured — your code is <strong className="font-mono">{devCode}</strong> (development only).
              </div>
            )}
            <label className="block">
              <span className="block text-sm font-medium text-slate-700 mb-1">Verification code</span>
              <input
                required
                autoFocus
                inputMode="numeric"
                value={code}
                onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
                className="input tracking-[0.3em] text-center font-mono text-lg"
                placeholder="------"
              />
            </label>
            {verifyOtp.isError && (
              <p className="text-sm text-red-600">
                {verifyOtp.error instanceof ApiError ? verifyOtp.error.message : "That code didn't work."}
              </p>
            )}
            <button
              type="submit"
              disabled={code.length !== 6 || verifyOtp.isPending}
              className="w-full bg-brand-600 text-white font-semibold py-2.5 rounded-full hover:bg-brand-700 disabled:bg-slate-300"
            >
              {verifyOtp.isPending ? "Verifying…" : "Verify & sign in"}
            </button>
            <button
              type="button"
              onClick={() => setStage("mobile")}
              className="w-full text-sm text-slate-500 hover:text-slate-700"
            >
              Use a different number
            </button>
          </form>
        )}
      </div>

      <p className="text-center text-xs text-slate-400 mt-5">
        You can also{" "}
        <button onClick={() => navigate(returnTo)} className="text-brand-600 hover:underline">
          continue as a guest
        </button>
        .
      </p>
    </div>
  );
}
