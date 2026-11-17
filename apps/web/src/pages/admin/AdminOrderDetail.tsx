import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, CreditCard, Mail, Package, PackageCheck, Truck, X } from "lucide-react";
import { api, ApiError, type Envelope } from "../../lib/api";
import { formatPaise } from "../../lib/money";
import { STATUS_LABELS, type Order } from "../../types";

// Admin order detail: this is where the "operator confirms the order" and
// "initiate payment in sandbox / verify webhook" steps of the brief happen
// from the staff side. The "Simulate payment" buttons build and sign a mock
// webhook event server-side and run it through the exact same
// signature-verification + idempotency code a real gateway webhook would hit
// — see internal/payments.Service.SimulateFor.
export default function AdminOrderDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();

  const { data, isError, error } = useQuery({
    queryKey: ["admin-order", id],
    queryFn: () => api.get<Envelope<Order>>(`/api/v1/admin/orders/${id}`),
    retry: false,
  });

  const confirm = useMutation({
    mutationFn: () => api.post<Envelope<Order>>(`/api/v1/admin/orders/${id}/confirm-whatsapp`),
    onSuccess: (res) => qc.setQueryData(["admin-order", id], res),
  });

  const createPayment = useMutation({
    mutationFn: () => api.post<Envelope<{ providerOrderId: string }>>(`/api/v1/orders/${order?.reference}/pay`, {
      contactMobile: order?.contactMobile,
    }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["admin-order", id] }),
  });

  const simulate = useMutation({
    mutationFn: (succeed: boolean) =>
      api.post(`/api/v1/admin/payments/${order?.payment ? providerOrderId : ""}/simulate`, { succeed }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["admin-order", id] }),
  });

  const setStatus = useMutation({
    mutationFn: (status: string) =>
      api.post<Envelope<Order>>(`/api/v1/admin/orders/${id}/status`, { status, reason: "updated by admin" }),
    onSuccess: (res) => qc.setQueryData(["admin-order", id], res),
  });

  if (isError && error instanceof ApiError && error.status === 401) {
    navigate("/admin");
    return null;
  }
  if (!data) return <div className="max-w-3xl mx-auto px-4 py-10 text-slate-400">Loading…</div>;

  const order = data.data;
  // providerOrderId is embedded in the payment URL ".../pay/mock/<id>"; the
  // list/detail payload doesn't expose it as its own field in this iteration.
  const providerOrderId = order.payment?.paymentUrl?.split("/").pop() ?? "";

  return (
    <div className="max-w-3xl mx-auto px-4 py-8">
      <Link to="/admin/orders" className="text-sm text-slate-400 hover:text-slate-600">
        ← All orders
      </Link>

      <div className="flex items-center justify-between mt-3 mb-5">
        <div>
          <h1 className="font-mono text-sm text-slate-500">{order.reference}</h1>
          <div className="text-xl font-bold text-slate-900">{STATUS_LABELS[order.status] ?? order.status}</div>
        </div>
        <div className="text-right">
          <div className="text-xs text-slate-400">Total</div>
          <div className="font-bold text-slate-900">{formatPaise(order.totalPaise)}</div>
        </div>
      </div>

      <div className="bg-white border border-slate-200 rounded-2xl p-5 mb-5">
        <h2 className="font-semibold text-slate-800 mb-3">Actions</h2>
        <div className="flex flex-wrap gap-2">
          {order.status === "AWAITING_CUSTOMER_CONFIRMATION" && (
            <ActionButton onClick={() => confirm.mutate()} pending={confirm.isPending}>
              <CheckCircle2 size={15} /> Confirm WhatsApp order
            </ActionButton>
          )}
          {order.status === "AWAITING_PAYMENT" && (
            <ActionButton onClick={() => createPayment.mutate()} pending={createPayment.isPending}>
              <CreditCard size={15} /> Create sandbox payment
            </ActionButton>
          )}
          {order.status === "PAYMENT_PENDING" && (
            <>
              <ActionButton onClick={() => simulate.mutate(true)} pending={simulate.isPending} tone="success">
                <CheckCircle2 size={15} /> Simulate payment success
              </ActionButton>
              <ActionButton onClick={() => simulate.mutate(false)} pending={simulate.isPending} tone="danger">
                <X size={15} /> Simulate payment failure
              </ActionButton>
            </>
          )}
          {order.status === "PAID" && (
            <ActionButton onClick={() => setStatus.mutate("PROCESSING")} pending={setStatus.isPending}>
              <Package size={15} /> Mark processing
            </ActionButton>
          )}
          {order.status === "PROCESSING" && (
            <ActionButton onClick={() => setStatus.mutate("READY_FOR_DISPATCH")} pending={setStatus.isPending}>
              <PackageCheck size={15} /> Ready for dispatch
            </ActionButton>
          )}
          {order.status === "READY_FOR_DISPATCH" && (
            <ActionButton onClick={() => setStatus.mutate("SHIPPED")} pending={setStatus.isPending}>
              <Truck size={15} /> Mark shipped
            </ActionButton>
          )}
          {order.status === "SHIPPED" && (
            <ActionButton onClick={() => setStatus.mutate("DELIVERED")} pending={setStatus.isPending}>
              <Mail size={15} /> Mark delivered
            </ActionButton>
          )}
        </div>
        {(confirm.isError || createPayment.isError || simulate.isError || setStatus.isError) && (
          <p className="text-sm text-red-600 mt-3">Action failed. Please refresh and try again.</p>
        )}
      </div>

      <div className="bg-white border border-slate-200 rounded-2xl p-5 mb-5 text-sm">
        <h2 className="font-semibold text-slate-800 mb-3">Customer &amp; delivery</h2>
        <p>{order.recipientName} · {order.contactMobile}</p>
        <p className="text-slate-500">
          {order.addressLine1}, {order.city}, {order.state} {order.pinCode}
        </p>
      </div>

      <div className="bg-white border border-slate-200 rounded-2xl p-5 mb-5 text-sm">
        <h2 className="font-semibold text-slate-800 mb-3">Items</h2>
        {order.items.map((it) => (
          <div key={it.id} className="flex justify-between text-slate-600 mb-1">
            <span>{it.productName} × {it.qty} {it.requiresPrescription && <span className="text-amber-600">℞</span>}</span>
            <span>{formatPaise(it.lineTotalPaise)}</span>
          </div>
        ))}
      </div>

      {order.payment && (
        <div className="bg-white border border-slate-200 rounded-2xl p-5 mb-5 text-sm">
          <h2 className="font-semibold text-slate-800 mb-2">Payment</h2>
          <p>Status: <strong>{order.payment.status}</strong> {order.payment.isTestMode && <span className="text-amber-600">(sandbox)</span>}</p>
          <p className="text-slate-500">Amount: {formatPaise(order.payment.amountPaise)}</p>
        </div>
      )}

      {order.history && (
        <div className="bg-white border border-slate-200 rounded-2xl p-5 text-sm">
          <h2 className="font-semibold text-slate-800 mb-2">Status history</h2>
          <ul className="space-y-1 text-slate-500 text-xs">
            {order.history.map((h, i) => (
              <li key={i}>
                {new Date(h.createdAt).toLocaleString()} — {h.fromStatus ?? "—"} → {h.toStatus} ({h.actorType})
                {h.reason ? `: ${h.reason}` : ""}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function ActionButton({
  onClick,
  pending,
  tone = "default",
  children,
}: {
  onClick: () => void;
  pending: boolean;
  tone?: "default" | "success" | "danger";
  children: React.ReactNode;
}) {
  const colors =
    tone === "success"
      ? "bg-teal-600 hover:bg-teal-700"
      : tone === "danger"
      ? "bg-red-600 hover:bg-red-700"
      : "bg-brand-600 hover:bg-brand-700";
  return (
    <button
      onClick={onClick}
      disabled={pending}
      className={`${colors} flex items-center gap-1.5 text-white text-sm font-semibold px-4 py-2 rounded-full disabled:opacity-50`}
    >
      {children}
    </button>
  );
}
