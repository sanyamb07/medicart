import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery } from "@tanstack/react-query";
import { CreditCard, Landmark, QrCode, ShieldCheck } from "lucide-react";
import { api, ApiError, type Envelope } from "../lib/api";
import { formatPaise } from "../lib/money";
import { track } from "../lib/analytics";

interface MockSummary {
  orderReference: string;
  amountPaise: number;
  status: string;
  isTestMode: boolean;
}

type Method = "upi" | "card" | "netbanking";

// This page stands in for a payment gateway's own hosted checkout (what a
// real Razorpay/Stripe-style integration would redirect the customer to). No
// card number, UPI PIN, or bank credential is ever collected here or sent to
// MediCart — the fields below are cosmetic, matching what a gateway's own
// page would show, and "Pay" simply tells the (sandboxed) gateway the
// customer completed payment, which arrives back to MediCart as a signed
// webhook — the same path a real gateway's webhook would take.
export default function MockCheckout() {
  const { providerOrderId } = useParams();
  const navigate = useNavigate();
  const [method, setMethod] = useState<Method>("upi");
  const [upiId, setUpiId] = useState("");
  const [cardNumber, setCardNumber] = useState("");

  const { data, isLoading, isError } = useQuery({
    queryKey: ["mock-checkout", providerOrderId],
    queryFn: () => api.get<Envelope<MockSummary>>(`/api/v1/payments/mock/${providerOrderId}`),
  });

  useEffect(() => {
    if (data) {
      track("payment_initiated", { orderReference: data.data.orderReference, amountPaise: data.data.amountPaise });
    }
  }, [data]);

  const complete = useMutation({
    mutationFn: (succeed: boolean) =>
      api.post(`/api/v1/payments/mock/${providerOrderId}/complete`, { method, succeed }),
    onSuccess: (_res, succeed) => {
      track(succeed ? "payment_succeeded" : "payment_failed", { method });
      navigate(`/track?reference=${data?.data.orderReference}&justPaid=${succeed ? "1" : "0"}`);
    },
  });

  if (isLoading) {
    return <div className="max-w-md mx-auto px-4 py-16 text-center text-slate-400">Loading payment details…</div>;
  }
  if (isError || !data) {
    return (
      <div className="max-w-md mx-auto px-4 py-16 text-center">
        <p className="font-semibold text-slate-700">This payment link is invalid or has expired.</p>
      </div>
    );
  }

  const { orderReference, amountPaise, isTestMode } = data.data;

  return (
    <div className="max-w-md mx-auto px-4 py-10">
      <div className="text-center mb-6">
        <div className="inline-flex h-11 w-11 items-center justify-center rounded-2xl bg-teal-50 text-teal-600 mb-3">
          <ShieldCheck size={22} />
        </div>
        <h1 className="text-lg font-bold text-slate-900">Complete your payment</h1>
        <p className="text-slate-500 text-sm mt-1">Order {orderReference}</p>
      </div>

      {isTestMode && (
        <div className="bg-amber-50 border border-amber-200 text-amber-800 text-xs rounded-lg px-3 py-2 mb-4 text-center">
          Sandbox payment session — no real money moves. For demonstration only.
        </div>
      )}

      <div className="bg-white border border-slate-200 rounded-2xl p-5">
        <div className="flex items-baseline justify-between mb-5 pb-4 border-b border-slate-100">
          <span className="text-sm text-slate-500">Amount payable</span>
          <span className="text-2xl font-bold text-slate-900">{formatPaise(amountPaise)}</span>
        </div>

        <div className="grid grid-cols-3 gap-2 mb-5">
          <MethodTab icon={<QrCode size={16} />} label="UPI" active={method === "upi"} onClick={() => setMethod("upi")} />
          <MethodTab icon={<CreditCard size={16} />} label="Card" active={method === "card"} onClick={() => setMethod("card")} />
          <MethodTab icon={<Landmark size={16} />} label="Net banking" active={method === "netbanking"} onClick={() => setMethod("netbanking")} />
        </div>

        {method === "upi" && (
          <label className="block mb-5">
            <span className="block text-sm font-medium text-slate-700 mb-1">UPI ID</span>
            <input
              value={upiId}
              onChange={(e) => setUpiId(e.target.value)}
              placeholder="yourname@upi"
              className="input"
            />
          </label>
        )}
        {method === "card" && (
          <label className="block mb-5">
            <span className="block text-sm font-medium text-slate-700 mb-1">Card number</span>
            <input
              value={cardNumber}
              onChange={(e) => setCardNumber(e.target.value.replace(/\D/g, "").slice(0, 16))}
              placeholder="4242 4242 4242 4242"
              inputMode="numeric"
              className="input"
            />
          </label>
        )}
        {method === "netbanking" && (
          <label className="block mb-5">
            <span className="block text-sm font-medium text-slate-700 mb-1">Select your bank</span>
            <select className="input">
              <option>HDFC Bank (sandbox)</option>
              <option>ICICI Bank (sandbox)</option>
              <option>State Bank of India (sandbox)</option>
            </select>
          </label>
        )}

        <button
          onClick={() => complete.mutate(true)}
          disabled={complete.isPending}
          className="w-full bg-teal-600 text-white font-semibold py-3 rounded-full hover:bg-teal-700 disabled:bg-slate-300"
        >
          {complete.isPending ? "Processing…" : `Pay ${formatPaise(amountPaise)}`}
        </button>
        <button
          onClick={() => complete.mutate(false)}
          disabled={complete.isPending}
          className="w-full mt-2 text-sm text-slate-400 hover:text-red-600"
        >
          Simulate a failed payment instead
        </button>

        {complete.isError && (
          <p className="text-sm text-red-600 mt-3 text-center">
            {complete.error instanceof ApiError ? complete.error.message : "Payment could not be completed."}
          </p>
        )}
      </div>

      <p className="text-center text-xs text-slate-400 mt-4">
        Secured sandbox checkout · no card or bank details are collected or stored
      </p>
    </div>
  );
}

function MethodTab({ icon, label, active, onClick }: { icon: React.ReactNode; label: string; active: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex flex-col items-center gap-1 rounded-xl border py-2.5 text-xs font-medium transition-colors ${
        active ? "border-brand-500 bg-brand-50 text-brand-700" : "border-slate-200 text-slate-500 hover:bg-slate-50"
      }`}
    >
      {icon}
      {label}
    </button>
  );
}
