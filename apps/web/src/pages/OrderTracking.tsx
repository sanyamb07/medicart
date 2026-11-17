import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useMutation } from "@tanstack/react-query";
import { AlertTriangle, Check, CheckCircle2, CreditCard, MapPin, PackageCheck, Search, XCircle } from "lucide-react";
import { api, ApiError, type Envelope } from "../lib/api";
import { formatPaise } from "../lib/money";
import { STATUS_LABELS, type Order } from "../types";
import { useAuth } from "../hooks/useAuth";
import { titleCase } from "../lib/categories";

// Lifecycle order of statuses (mirrors services/api/internal/orders/state.go).
const ORDER = [
  "CREATED",
  "AWAITING_CUSTOMER_CONFIRMATION",
  "AWAITING_PRESCRIPTION_REVIEW",
  "AWAITING_PAYMENT",
  "PAYMENT_PENDING",
  "PAID",
  "PROCESSING",
  "READY_FOR_DISPATCH",
  "SHIPPED",
  "DELIVERED",
];
const rank = (s: string) => ORDER.indexOf(s);

// Customer-facing milestones; each is "reached" once the order gets to
// `reachedAt`. The prescription milestone only appears for Rx orders.
const MILESTONES = [
  { label: "Order placed", hint: "We've received your order", reachedAt: "CREATED" },
  { label: "Confirmed on WhatsApp", hint: "Confirm your order in the WhatsApp chat we opened", reachedAt: "AWAITING_PRESCRIPTION_REVIEW" },
  { label: "Prescription reviewed", hint: "A pharmacist is checking your prescription", reachedAt: "AWAITING_PAYMENT", rxOnly: true },
  { label: "Payment received", hint: "Complete payment to continue", reachedAt: "PAID" },
  { label: "Packed", hint: "Your order is being prepared", reachedAt: "READY_FOR_DISPATCH" },
  { label: "Shipped", hint: "On its way", reachedAt: "SHIPPED" },
  { label: "Delivered", hint: "", reachedAt: "DELIVERED" },
];

// Statuses where it's worth quietly re-checking for a status the customer
// didn't cause themselves — an operator confirming on WhatsApp, or a webhook
// landing. Once PAID or later, or CANCELLED, there's nothing left to wait for.
const POLLABLE = new Set(["AWAITING_CUSTOMER_CONFIRMATION", "AWAITING_PAYMENT", "PAYMENT_PENDING"]);

// Mirrors orders.CanCustomerCancel: after payment, cancelling becomes a
// refund decision for staff.
const CUSTOMER_CANCELLABLE = new Set(["CREATED", "AWAITING_CUSTOMER_CONFIRMATION", "AWAITING_PRESCRIPTION_REVIEW", "AWAITING_PAYMENT", "PAYMENT_PENDING"]);

const CANCEL_REASONS = ["Ordered by mistake", "Found a better price", "Delivery takes too long", "Doctor changed my prescription", "Other"];

export default function OrderTracking() {
  const [params] = useSearchParams();
  const { user } = useAuth();
  const [reference, setReference] = useState(params.get("reference") ?? "");
  const [mobile, setMobile] = useState("");
  const [order, setOrder] = useState<Order | null>(null);
  const [cancelOpen, setCancelOpen] = useState(false);
  const [reason, setReason] = useState(CANCEL_REASONS[0]);
  const justPaid = params.get("justPaid");

  useEffect(() => {
    document.title = "Track your order | MediCart";
  }, []);
  useEffect(() => {
    if (user?.mobile) setMobile((m) => m || user.mobile!.replace(/\D/g, "").slice(-10));
  }, [user]);

  const track = useMutation({
    mutationFn: () => api.post<Envelope<Order>>(`/api/v1/orders/${reference.trim()}/track`, { contactMobile: mobile }),
    onSuccess: (res) => setOrder(res.data),
    onError: () => setOrder(null),
  });

  const payNow = useMutation({
    mutationFn: () => api.post<Envelope<{ paymentUrl: string }>>(`/api/v1/orders/${reference.trim()}/pay`, { contactMobile: mobile }),
    onSuccess: (res) => {
      window.location.href = res.data.paymentUrl;
    },
  });

  const cancel = useMutation({
    mutationFn: () => api.post<Envelope<Order>>(`/api/v1/orders/${reference.trim()}/cancel`, { contactMobile: mobile, reason }),
    onSuccess: () => {
      setCancelOpen(false);
      track.mutate();
    },
  });

  // Lightweight polling: while the order is in a state something else (staff,
  // a webhook) might move it out of, re-check every 5s so the customer sees
  // "Payment received" without having to click Track again.
  useEffect(() => {
    if (!order || !POLLABLE.has(order.status) || !reference || !mobile) return;
    const id = setInterval(() => track.mutate(), 5000);
    return () => clearInterval(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [order?.status, reference, mobile]);

  const isCancelled = order?.status === "CANCELLED";
  const isRefund = order?.status === "REFUND_PENDING" || order?.status === "REFUNDED";
  const canPayNow = order && (order.status === "AWAITING_PAYMENT" || order.status === "PAYMENT_PENDING");
  const canCancel = order && CUSTOMER_CANCELLABLE.has(order.status);

  return (
    <div className="page max-w-3xl py-8 sm:py-12">
      <h1 className="text-2xl font-bold text-ink-900 sm:text-3xl">Track your order</h1>
      <p className="mt-1 text-ink-600">Enter your order reference and the mobile number used at checkout.</p>

      {justPaid === "1" && (
        <div className="mt-5 flex items-center gap-2 rounded-xl bg-save-50 px-4 py-3 text-sm font-medium text-save-700 ring-1 ring-inset ring-save-100">
          <CheckCircle2 size={18} className="shrink-0" /> Payment received. Thank you!
        </div>
      )}
      {justPaid === "0" && (
        <div className="mt-5 flex items-center gap-2 rounded-xl bg-red-50 px-4 py-3 text-sm text-red-700 ring-1 ring-inset ring-red-200">
          <XCircle size={18} className="shrink-0" /> That payment attempt didn't go through. You can try again below.
        </div>
      )}

      <form
        onSubmit={(e) => {
          e.preventDefault();
          track.mutate();
        }}
        className="mt-6 grid gap-3 rounded-2xl border border-ink-200/80 bg-white p-4 sm:grid-cols-[1fr_11rem_auto] sm:p-5"
      >
        <label className="block">
          <span className="label">Order reference</span>
          <input
            required
            placeholder="e.g. MC-26A-XXXXXXXX"
            value={reference}
            onChange={(e) => setReference(e.target.value.toUpperCase())}
            className="input font-mono"
          />
        </label>
        <label className="block">
          <span className="label">Mobile number</span>
          <input
            required
            placeholder="10 digits"
            inputMode="numeric"
            autoComplete="tel-national"
            value={mobile}
            onChange={(e) => setMobile(e.target.value.replace(/\D/g, "").slice(0, 10))}
            className="input tabular"
          />
        </label>
        <button type="submit" disabled={track.isPending} className="btn btn-primary self-end">
          <Search size={16} /> {track.isPending && !order ? "Searching…" : "Track"}
        </button>
      </form>

      {track.isError && (
        <div role="alert" className="mt-4 rounded-xl bg-red-50 px-4 py-3 text-sm text-red-700 ring-1 ring-inset ring-red-200">
          {track.error instanceof ApiError ? track.error.message : "Could not find that order."}
        </div>
      )}

      {order && (
        <div className="mt-6 overflow-hidden rounded-2xl border border-ink-200/80 bg-white animate-slide-up">
          <div className="flex flex-wrap items-start justify-between gap-3 border-b border-ink-100 p-5 sm:p-6">
            <div>
              <p className="font-mono text-sm text-ink-500">{order.reference}</p>
              <p className="mt-1 text-xl font-bold text-ink-900">{STATUS_LABELS[order.status] ?? order.status}</p>
            </div>
            <div className="text-right">
              <p className="text-xs text-ink-500">Total</p>
              <p className="text-xl font-bold text-ink-900 tabular">{formatPaise(order.totalPaise)}</p>
            </div>
          </div>

          <div className="space-y-5 p-5 sm:p-6">
            {isCancelled ? (
              <div className="rounded-xl bg-ink-100 p-4 text-sm text-ink-700">
                This order was cancelled{order.cancelledReason ? `: ${order.cancelledReason}` : "."}
              </div>
            ) : isRefund ? (
              <div className="rounded-xl bg-ink-100 p-4 text-sm text-ink-700">
                {order.status === "REFUNDED" ? "This order has been refunded to your original payment method." : "A refund for this order is in progress."}
              </div>
            ) : (
              <Timeline order={order} />
            )}

            {canPayNow && (
              <div className="rounded-xl bg-brand-50 p-4 ring-1 ring-inset ring-brand-100">
                <p className="flex items-center gap-2 font-semibold text-brand-800">
                  <CreditCard size={17} /> Your order is confirmed. Complete payment to proceed.
                </p>
                <p className="mt-1 text-sm text-brand-700">Pay by UPI, card or net banking on our secure checkout (sandbox in this demo).</p>
                <button onClick={() => payNow.mutate()} disabled={payNow.isPending} className="btn btn-primary mt-3">
                  {payNow.isPending ? "Opening payment…" : "Pay now"}
                </button>
              </div>
            )}

            {(order.status === "PAID" || order.status === "PROCESSING") && (
              <div className="flex items-center gap-2 rounded-xl bg-save-50 p-3 text-sm font-medium text-save-700">
                <PackageCheck size={16} /> Payment received. We're getting your order ready.
              </div>
            )}

            {order.payment && (
              <p className="text-sm text-ink-600">
                Payment: <strong className="text-ink-900">{order.payment.status}</strong>
                {order.payment.isTestMode && <span className="text-rx-700"> (sandbox)</span>}
              </p>
            )}

            <div>
              <h2 className="font-sans text-sm font-semibold text-ink-900">Items</h2>
              <ul className="mt-2 divide-y divide-ink-100 text-sm">
                {order.items.map((it) => (
                  <li key={it.id} className="flex justify-between gap-4 py-2 text-ink-700">
                    <span>
                      {titleCase(it.productName)} <span className="text-ink-400">× {it.qty}</span>
                    </span>
                    <span className="font-medium text-ink-900 tabular">{formatPaise(it.lineTotalPaise)}</span>
                  </li>
                ))}
              </ul>
            </div>

            <p className="flex items-center gap-2 text-sm text-ink-600">
              <MapPin size={15} className="text-ink-400" /> Delivering to {order.city}, {order.state} {order.pinCode}
            </p>

            {order.history && order.history.length > 0 && (
              <details className="text-sm text-ink-600">
                <summary className="cursor-pointer font-medium text-ink-800">Full status history</summary>
                <ul className="mt-2 space-y-1">
                  {order.history.map((h, i) => (
                    <li key={i}>
                      <span className="tabular">{new Date(h.createdAt).toLocaleString("en-IN")}</span> · {STATUS_LABELS[h.toStatus] ?? h.toStatus}
                      {h.reason ? `: ${h.reason}` : ""}
                    </li>
                  ))}
                </ul>
              </details>
            )}

            {canCancel && !cancelOpen && (
              <button onClick={() => setCancelOpen(true)} className="text-sm font-semibold text-red-700 hover:underline">
                Cancel this order
              </button>
            )}
            {canCancel && cancelOpen && (
              <div className="rounded-xl bg-red-50 p-4 ring-1 ring-inset ring-red-200">
                <p className="flex items-center gap-2 font-semibold text-red-800">
                  <AlertTriangle size={16} /> Cancel this order?
                </p>
                <p className="mt-1 text-sm text-red-700">Nothing has been charged yet. This can't be undone.</p>
                <label className="mt-3 block">
                  <span className="label">Reason</span>
                  <select value={reason} onChange={(e) => setReason(e.target.value)} className="input">
                    {CANCEL_REASONS.map((r) => (
                      <option key={r}>{r}</option>
                    ))}
                  </select>
                </label>
                {cancel.isError && (
                  <p className="mt-2 text-sm text-red-700">{cancel.error instanceof ApiError ? cancel.error.message : "Couldn't cancel. Please try again."}</p>
                )}
                <div className="mt-3 flex gap-2">
                  <button onClick={() => cancel.mutate()} disabled={cancel.isPending} className="btn btn-sm bg-red-600 text-white hover:bg-red-700">
                    {cancel.isPending ? "Cancelling…" : "Yes, cancel order"}
                  </button>
                  <button onClick={() => setCancelOpen(false)} className="btn btn-sm btn-secondary">
                    Keep order
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function Timeline({ order }: { order: Order }) {
  const r = rank(order.status);
  const steps = MILESTONES.filter((m) => !m.rxOnly || order.requiresPrescription);
  const current = steps.findIndex((m) => r < rank(m.reachedAt));
  return (
    <ol className="space-y-0" aria-label="Order progress">
      {steps.map((m, i) => {
        const done = r >= rank(m.reachedAt);
        const active = i === current;
        return (
          <li key={m.label} className="relative flex gap-3 pb-5 last:pb-0" aria-current={active ? "step" : undefined}>
            {i < steps.length - 1 && (
              <span aria-hidden className={`absolute left-[11px] top-6 h-[calc(100%-1.5rem)] w-0.5 ${done ? "bg-brand-600" : "bg-ink-200"}`} />
            )}
            <span
              className={`relative z-10 flex h-6 w-6 shrink-0 items-center justify-center rounded-full ${
                done ? "bg-brand-600 text-white" : active ? "bg-white ring-2 ring-brand-600" : "bg-white ring-2 ring-ink-200"
              }`}
            >
              {done ? <Check size={14} strokeWidth={3} /> : active ? <span className="h-2 w-2 rounded-full bg-brand-600" /> : null}
            </span>
            <div className="-mt-0.5">
              <p className={`text-sm font-semibold ${done || active ? "text-ink-900" : "text-ink-400"}`}>{m.label}</p>
              {active && m.hint && <p className="text-sm text-ink-600">{m.hint}</p>}
            </div>
          </li>
        );
      })}
    </ol>
  );
}
