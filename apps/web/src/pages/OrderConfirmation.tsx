import { useEffect } from "react";
import { Link, useLocation, useParams } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, MessageCircle, PackageSearch } from "lucide-react";
import { formatPaise } from "../lib/money";
import type { Order } from "../types";
import { track } from "../lib/analytics";
import { titleCase } from "../lib/categories";

interface LocationState {
  order: Order;
  whatsappLink: string;
  mobile: string;
}

// Shown immediately after checkout. The WhatsApp link is opened in a new tab
// — opening it is NOT proof the message was sent, so the order stays
// AWAITING_CUSTOMER_CONFIRMATION until staff record an explicit confirmation.
// This page just makes that next step obvious and gives the customer a way
// back to the site (the order reference + mobile + /track page).
export default function OrderConfirmation() {
  const { reference } = useParams();
  const location = useLocation();
  const state = location.state as LocationState | undefined;
  const qc = useQueryClient();

  useEffect(() => {
    qc.invalidateQueries({ queryKey: ["cart"] });
  }, [qc]);

  if (!state?.order) {
    return (
      <div className="page max-w-xl py-16 text-center">
        <span className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-ink-100 text-ink-400">
          <PackageSearch size={26} />
        </span>
        <h1 className="mt-4 text-2xl font-bold text-ink-900">Order {reference}</h1>
        <p className="mt-2 text-ink-600">
          The WhatsApp confirmation step is shown right after checkout. You can follow this order's progress from the tracking page.
        </p>
        <Link to={`/track?reference=${reference ?? ""}`} className="btn btn-primary mt-6">
          Track this order
        </Link>
      </div>
    );
  }

  const { order, whatsappLink } = state;

  return (
    <div className="page max-w-2xl py-10 sm:py-14">
      <div className="text-center animate-slide-up">
        <span className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-save-50 text-save-600 ring-8 ring-save-50/50">
          <CheckCircle2 size={34} />
        </span>
        <h1 className="mt-5 text-3xl font-bold text-ink-900">Order placed</h1>
        <p className="mt-2 text-ink-600">
          Reference <span className="font-mono font-semibold text-ink-900">{order.reference}</span>
        </p>
      </div>

      <div className="mt-8 rounded-2xl border-2 border-[#25D366]/40 bg-white p-5 sm:p-6">
        <p className="text-xs font-semibold uppercase tracking-wide text-[#128C4B]">Next step</p>
        <h2 className="mt-1 text-xl font-bold text-ink-900">Confirm your order on WhatsApp</h2>
        <p className="mt-2 text-ink-600">
          We've prepared a WhatsApp message with your order details. Send it to us to confirm. Nothing is confirmed, and nothing is
          charged, until you do.
        </p>
        <a
          href={whatsappLink}
          target="_blank"
          rel="noreferrer"
          onClick={() => {
            track("whatsapp_handoff_opened", { orderReference: order.reference });
            fetch(
              `${import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080"}/api/v1/orders/${order.reference}/whatsapp-handoff`,
              {
                method: "POST",
                credentials: "include",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ contactMobile: state.mobile }),
              },
            );
          }}
          className="btn btn-lg mt-5 w-full bg-[#1fa855] text-white hover:bg-[#178a45]"
        >
          <MessageCircle size={19} /> Confirm on WhatsApp
        </a>
        <ol className="mt-5 grid gap-2 text-sm text-ink-600 sm:grid-cols-3">
          {[
            "Send the WhatsApp message",
            order.requiresPrescription ? "Pharmacist reviews your prescription" : "We confirm your order",
            "Pay securely and we dispatch",
          ].map((t, i) => (
            <li key={t} className="flex items-start gap-2 rounded-xl bg-ink-50 p-3">
              <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-ink-900 text-xs font-bold text-white">{i + 1}</span>
              {t}
            </li>
          ))}
        </ol>
      </div>

      <div className="mt-5 rounded-2xl border border-ink-200/80 bg-white p-5 sm:p-6">
        <h2 className="font-sans text-lg font-semibold text-ink-900">Order summary</h2>
        <ul className="mt-3 divide-y divide-ink-100 text-sm">
          {order.items.map((it) => (
            <li key={it.id} className="flex justify-between gap-4 py-2 text-ink-700">
              <span>
                {titleCase(it.productName)} <span className="text-ink-400">× {it.qty}</span>
              </span>
              <span className="font-medium text-ink-900 tabular">{formatPaise(it.lineTotalPaise)}</span>
            </li>
          ))}
        </ul>
        <dl className="mt-2 space-y-1.5 border-t border-ink-100 pt-3 text-sm">
          {order.discountPaise > 0 && (
            <div className="flex justify-between text-save-700">
              <dt>Discount</dt>
              <dd className="tabular">−{formatPaise(order.discountPaise)}</dd>
            </div>
          )}
          <div className="flex justify-between text-ink-600">
            <dt>Delivery</dt>
            <dd className="tabular">{order.deliveryFeePaise === 0 ? "Free" : formatPaise(order.deliveryFeePaise)}</dd>
          </div>
          <div className="flex justify-between pt-1 text-base font-bold text-ink-900">
            <dt>Total</dt>
            <dd className="tabular">{formatPaise(order.totalPaise)}</dd>
          </div>
        </dl>
        <p className="mt-3 text-sm text-ink-500">
          Delivering to {order.recipientName}, {order.city} {order.pinCode}
        </p>
      </div>

      <div className="mt-6 flex flex-col gap-3 sm:flex-row">
        <Link to={`/track?reference=${order.reference}`} className="btn btn-secondary btn-lg flex-1">
          Track order
        </Link>
        <Link to="/products" className="btn btn-primary btn-lg flex-1">
          Continue shopping
        </Link>
      </div>
    </div>
  );
}
