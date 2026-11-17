import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, FileText, FileUp, Heart, LogOut, Package, PackageSearch, RotateCcw, User } from "lucide-react";
import { api, ApiError, type Envelope } from "../lib/api";
import { formatPaise } from "../lib/money";
import { STATUS_LABELS, type CartSummary, type Order, type OrderListItem } from "../types";
import { useAuth, useLogout } from "../hooks/useAuth";
import { useWishlist } from "../lib/prefs";
import { useToast } from "../components/Toast";
import PrescriptionUploader from "../components/PrescriptionUploader";

const STATUS_TONE: Record<string, string> = {
  DELIVERED: "bg-save-50 text-save-700",
  SHIPPED: "bg-brand-50 text-brand-700",
  CANCELLED: "bg-ink-100 text-ink-600",
  REFUNDED: "bg-ink-100 text-ink-600",
  AWAITING_PRESCRIPTION_REVIEW: "bg-rx-50 text-rx-700",
  AWAITING_CUSTOMER_CONFIRMATION: "bg-rx-50 text-rx-700",
  AWAITING_PAYMENT: "bg-rx-50 text-rx-700",
};

type Tab = "orders" | "prescriptions";

export default function Account() {
  const navigate = useNavigate();
  const { user, isLoading: loadingUser } = useAuth();
  const logout = useLogout();
  const wishlist = useWishlist();
  const [tab, setTab] = useState<Tab>(() => (window.location.hash === "#prescriptions" ? "prescriptions" : "orders"));

  useEffect(() => {
    document.title = "Your account | MediCart";
  }, []);

  const { data, isLoading, isError } = useQuery({
    queryKey: ["my-orders"],
    queryFn: () => api.get<Envelope<OrderListItem[]>>("/api/v1/me/orders"),
    enabled: !!user,
  });

  if (loadingUser) {
    return (
      <div className="page max-w-4xl py-10">
        <div className="skeleton h-24 rounded-2xl" />
        <div className="skeleton mt-6 h-64 rounded-2xl" />
      </div>
    );
  }

  if (!user) {
    return (
      <div className="page max-w-md py-16 text-center">
        <span className="mx-auto flex h-16 w-16 items-center justify-center rounded-3xl bg-brand-50 text-brand-700">
          <User size={30} />
        </span>
        <h1 className="mt-5 text-2xl font-bold text-ink-900">Sign in to your account</h1>
        <p className="mt-2 text-ink-600">See your orders, buy again in one tap, and keep your prescriptions in one place.</p>
        <Link to="/login" state={{ from: "/account" }} className="btn btn-primary btn-lg mt-6">
          Sign in with mobile number
        </Link>
      </div>
    );
  }

  const orders = data?.data ?? [];

  return (
    <div className="page max-w-4xl py-8">
      <div className="flex flex-wrap items-center gap-4 rounded-2xl border border-ink-200/80 bg-white p-5 sm:p-6">
        <span className="flex h-14 w-14 items-center justify-center rounded-2xl bg-brand-600 font-display text-xl font-bold text-white">
          {(user.fullName ?? "U").charAt(0).toUpperCase()}
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-xl font-bold text-ink-900">{user.fullName ? `Hello, ${user.fullName.split(" ")[0]}` : "Your account"}</h1>
          {user.mobile && <p className="text-sm text-ink-500 tabular">+91 {user.mobile}</p>}
        </div>
        <button
          onClick={async () => {
            await logout();
            navigate("/");
          }}
          className="btn btn-sm btn-secondary"
        >
          <LogOut size={15} /> Sign out
        </button>
      </div>

      <div className="mt-4 grid grid-cols-3 gap-3">
        <QuickTile to="/wishlist" icon={Heart} label="Wishlist" value={`${wishlist.slugs.length} saved`} />
        <QuickTile to="/track" icon={Package} label="Track order" value="By reference" />
        <QuickTile to="/upload-prescription" icon={FileUp} label="Upload Rx" value="For pharmacist review" />
      </div>

      <div className="mt-8 flex gap-1 border-b border-ink-200" role="tablist">
        {(
          [
            ["orders", "Orders", Package],
            ["prescriptions", "Prescriptions", FileText],
          ] as const
        ).map(([id, label, Icon]) => (
          <button
            key={id}
            role="tab"
            aria-selected={tab === id}
            onClick={() => setTab(id)}
            className={`-mb-px flex items-center gap-2 border-b-2 px-4 py-3 text-sm font-semibold ${
              tab === id ? "border-brand-600 text-brand-800" : "border-transparent text-ink-500 hover:text-ink-800"
            }`}
          >
            <Icon size={16} /> {label}
          </button>
        ))}
      </div>

      <div className="mt-6" role="tabpanel">
        {tab === "orders" && (
          <>
            {isLoading && (
              <div className="space-y-3">
                {[0, 1, 2].map((i) => (
                  <div key={i} className="skeleton h-24 rounded-2xl" />
                ))}
              </div>
            )}
            {isError && <p className="text-sm text-red-700">We couldn't load your orders. Please refresh to try again.</p>}
            {data && orders.length === 0 && (
              <div className="rounded-2xl border border-ink-200/80 bg-white p-10 text-center">
                <PackageSearch className="mx-auto text-ink-300" size={40} />
                <p className="mt-3 font-semibold text-ink-800">No orders yet</p>
                <p className="mt-1 text-sm text-ink-500">Orders you place while signed in will show up here.</p>
                <Link to="/products" className="btn btn-primary mt-5">
                  Start shopping
                </Link>
              </div>
            )}
            <ul className="space-y-3">
              {orders.map((o) => (
                <OrderRow key={o.id} order={o} />
              ))}
            </ul>
          </>
        )}
        {tab === "prescriptions" && (
          <div>
            <p className="mb-5 text-sm text-ink-600">
              Upload a prescription so a pharmacist can review it ahead of time. This does not place an order.
            </p>
            <PrescriptionUploader />
          </div>
        )}
      </div>
    </div>
  );
}

function QuickTile({ to, icon: Icon, label, value }: { to: string; icon: typeof Heart; label: string; value: string }) {
  return (
    <Link to={to} className="rounded-2xl border border-ink-200/80 bg-white p-4 transition-shadow hover:shadow-cardHover">
      <Icon size={20} className="text-brand-600" />
      <p className="mt-2 text-sm font-semibold text-ink-900">{label}</p>
      <p className="truncate text-xs text-ink-500">{value}</p>
    </Link>
  );
}

function OrderRow({ order: o }: { order: OrderListItem }) {
  const qc = useQueryClient();
  const toast = useToast();
  const navigate = useNavigate();
  const [busy, setBusy] = useState(false);

  // "Buy again": re-adds each line of a past order through the normal cart
  // endpoint, so stock limits and current prices are applied by the server.
  async function reorder() {
    setBusy(true);
    try {
      const detail = await api.get<Envelope<Order>>(`/api/v1/me/orders/${o.id}`);
      let added = 0;
      const skipped: string[] = [];
      for (const item of detail.data.items) {
        try {
          const res = await api.post<Envelope<CartSummary>>("/api/v1/cart/items", { productId: item.productId, qty: item.qty });
          qc.setQueryData(["cart"], res);
          added++;
        } catch (e) {
          skipped.push(item.productName);
          if (!(e instanceof ApiError)) throw e;
        }
      }
      if (added > 0) {
        toast({
          tone: skipped.length ? "error" : "success",
          title: skipped.length ? `Added ${added} item(s). ${skipped.length} unavailable.` : `Added ${added} item(s) to cart`,
          action: { label: "View cart", to: "/cart" },
        });
        if (!skipped.length) navigate("/cart");
      } else {
        toast({ tone: "error", title: "Those items aren't available right now." });
      }
    } catch {
      toast({ tone: "error", title: "Couldn't reorder. Please try again." });
    } finally {
      setBusy(false);
    }
  }

  return (
    <li className="rounded-2xl border border-ink-200/80 bg-white p-4 sm:p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <span className={`inline-flex rounded-full px-2.5 py-1 text-xs font-semibold ${STATUS_TONE[o.status] ?? "bg-brand-50 text-brand-700"}`}>
            {STATUS_LABELS[o.status] ?? o.status}
          </span>
          <p className="mt-2 text-sm text-ink-600">
            {new Date(o.placedAt).toLocaleDateString("en-IN", { day: "numeric", month: "short", year: "numeric" })} · {o.itemCount} item
            {o.itemCount === 1 ? "" : "s"} · <span className="font-mono text-xs">{o.reference}</span>
          </p>
        </div>
        <p className="text-lg font-bold text-ink-900 tabular">{formatPaise(o.totalPaise)}</p>
      </div>
      <div className="mt-4 flex flex-wrap gap-2">
        <button onClick={reorder} disabled={busy} className="btn btn-sm btn-soft">
          <RotateCcw size={15} /> {busy ? "Adding…" : "Buy again"}
        </button>
        <Link to={`/track?reference=${o.reference}`} className="btn btn-sm btn-secondary">
          Track order <ChevronRight size={15} />
        </Link>
      </div>
    </li>
  );
}
