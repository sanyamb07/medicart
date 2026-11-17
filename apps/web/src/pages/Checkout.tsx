import { useEffect, useRef, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useMutation, useQuery } from "@tanstack/react-query";
import { AlertTriangle, CheckCircle2, Clock, CreditCard, FileText, FileUp, Lock, MapPin, MessageCircle, Truck, User } from "lucide-react";
import { api, ApiError, type Envelope } from "../lib/api";
import { formatPaise } from "../lib/money";
import type { Order, Prescription } from "../types";
import { PRESCRIPTION_STATUS_LABELS } from "../types";
import { useAuth } from "../hooks/useAuth";
import { cartTotals, useCart, useSetCartQty } from "../hooks/useCart";
import { useServiceability } from "../hooks/useDelivery";
import { useDeliveryPin, useSavedForLater } from "../lib/prefs";
import { titleCase } from "../lib/categories";
import { track } from "../lib/analytics";
import { getStoredReferralCode } from "../lib/affiliateRef";
import { ProductImage } from "../components/product/bits";

// One page, three clearly numbered sections — no wizard to click through.
// The request body and endpoint are unchanged from the original checkout:
// POST /api/v1/checkout with the address fields (+ silent affiliate code).
export default function Checkout() {
  const navigate = useNavigate();
  const { user } = useAuth();
  const [savedPin, setSavedPin] = useDeliveryPin();
  const [form, setForm] = useState({
    recipientName: "",
    contactMobile: "",
    addressLine1: "",
    addressLine2: "",
    landmark: "",
    city: "",
    state: "",
    pinCode: savedPin,
    deliveryNotes: "",
    // Silently carried, not a visible field — see lib/affiliateRef.
    affiliateCode: getStoredReferralCode(),
  });
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const errorRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    document.title = "Checkout | MediCart";
    track("checkout_started");
  }, []);

  // A signed-in customer shouldn't retype what we already know.
  useEffect(() => {
    if (user) {
      setForm((f) => ({
        ...f,
        contactMobile: f.contactMobile || user.mobile || "",
        recipientName: f.recipientName || user.fullName || "",
      }));
    }
  }, [user]);

  const { cart, isLoading } = useCart();
  const { data: area, isLoading: checkingPin } = useServiceability(form.pinCode);

  // Fill city/state from the PIN lookup when the shopper hasn't typed them.
  useEffect(() => {
    if (area?.isServiceable && area.city) {
      setForm((f) => ({ ...f, city: f.city || area.city, state: f.state || area.state }));
    }
  }, [area]);

  const { data: rxRes } = useQuery({
    queryKey: ["my-prescriptions"],
    queryFn: () => api.get<Envelope<Prescription[]>>("/api/v1/me/prescriptions"),
    enabled: !!user && !!cart?.requiresPrescription,
  });

  const checkout = useMutation({
    mutationFn: () => api.post<Envelope<{ order: Order; whatsappLink: string }>>("/api/v1/checkout", form),
    onSuccess: (res) => {
      track("order_completed", {
        orderReference: res.data.order.reference,
        totalPaise: res.data.order.totalPaise,
        itemCount: res.data.order.items.length,
      });
      if (/^\d{6}$/.test(form.pinCode)) setSavedPin(form.pinCode);
      navigate(`/orders/${res.data.order.reference}/confirmation`, {
        state: { order: res.data.order, whatsappLink: res.data.whatsappLink, mobile: form.contactMobile },
      });
    },
    onError: (err) => {
      if (err instanceof ApiError && err.fields) setFieldErrors(err.fields);
      // Bring the explanation to where the shopper is looking.
      requestAnimationFrame(() => errorRef.current?.scrollIntoView({ behavior: "smooth", block: "start" }));
    },
  });

  function update<K extends keyof typeof form>(key: K, value: string) {
    setForm((f) => ({ ...f, [key]: value }));
    if (fieldErrors[key]) setFieldErrors((e) => ({ ...e, [key]: "" }));
  }

  if (isLoading) {
    return (
      <div className="page py-8">
        <div className="grid gap-6 lg:grid-cols-[1fr_24rem]">
          <div className="skeleton h-[32rem] rounded-2xl" />
          <div className="skeleton h-80 rounded-2xl" />
        </div>
      </div>
    );
  }

  if (!cart || cart.lines.length === 0) {
    return (
      <div className="page max-w-md py-20 text-center">
        <h1 className="text-2xl font-bold text-ink-900">Your cart is empty</h1>
        <p className="mt-2 text-ink-600">Add something to your cart before checking out.</p>
        <Link to="/products" className="btn btn-primary mt-6">
          Browse products
        </Link>
      </div>
    );
  }

  const { mrp, discount } = cartTotals(cart);
  const pinComplete = form.pinCode.length === 6;
  const notServiceable = pinComplete && !checkingPin && (area === null || area?.isServiceable === false);
  const deliveryFee = area?.isServiceable ? area.deliveryFeePaise : 0;
  const total = cart.subtotalPaise + deliveryFee;
  const prescriptions = rxRes?.data ?? [];
  const usableRx = prescriptions.filter((p) => p.status === "APPROVED" || p.status === "PENDING_REVIEW");
  const placeDisabled = checkout.isPending || notServiceable || cart.hasIssues;
  const placeLabel = checkout.isPending ? "Placing order…" : `Place order · ${formatPaise(total)}`;

  return (
    <div className="pb-28 md:pb-0">
      <div className="page py-6 sm:py-8">
        <ol className="mb-6 flex items-center gap-2 text-sm" aria-label="Checkout progress">
          <li className="flex items-center gap-1.5 text-ink-500">
            <CheckCircle2 size={16} className="text-brand-600" /> Cart
          </li>
          <li aria-hidden className="h-px w-6 bg-ink-300" />
          <li className="flex items-center gap-1.5 font-semibold text-ink-900" aria-current="step">
            <span className="flex h-5 w-5 items-center justify-center rounded-full bg-brand-600 text-xs text-white">2</span> Details
          </li>
          <li aria-hidden className="h-px w-6 bg-ink-300" />
          <li className="flex items-center gap-1.5 text-ink-400">
            <span className="flex h-5 w-5 items-center justify-center rounded-full bg-ink-200 text-xs text-ink-600">3</span> Confirm &amp; pay
          </li>
        </ol>

        <form
          id="checkout-form"
          className="grid gap-6 lg:grid-cols-[1fr_24rem] lg:gap-8"
          onSubmit={(e) => {
            e.preventDefault();
            if (!placeDisabled) checkout.mutate();
          }}
          noValidate={false}
        >
          <div className="space-y-5">
            <div ref={errorRef} className="scroll-mt-28 empty:hidden">
            {checkout.isError && isRxBlocked(checkout.error) ? (
              <RxBlockedNotice onResolved={() => checkout.reset()} />
            ) : checkout.isError ? (
              <div role="alert" className="flex items-start gap-2 rounded-xl bg-red-50 px-4 py-3 text-sm text-red-700 ring-1 ring-inset ring-red-200">
                <AlertTriangle size={17} className="mt-0.5 shrink-0" />
                {checkout.error instanceof ApiError ? checkout.error.message : "We could not place your order. Please try again."}
              </div>
            ) : null}
            </div>
            {/* 1. Contact & address */}
            <Section n={1} icon={MapPin} title="Delivery address">
              {!user && (
                <p className="mb-4 flex flex-wrap items-center gap-1.5 rounded-xl bg-ink-50 px-3.5 py-2.5 text-sm text-ink-600">
                  <User size={15} />
                  <Link to="/login" state={{ from: "/checkout" }} className="font-semibold text-brand-700 hover:underline">
                    Sign in
                  </Link>
                  to save this order to your account.
                </p>
              )}
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="Full name" error={fieldErrors.recipientName}>
                  <input required autoComplete="name" value={form.recipientName} onChange={(e) => update("recipientName", e.target.value)} className="input" />
                </Field>
                <Field label="Mobile number" error={fieldErrors.contactMobile} hint="For delivery updates on WhatsApp">
                  <div className="relative">
                    <span className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-ink-500">+91</span>
                    <input
                      required
                      inputMode="numeric"
                      autoComplete="tel-national"
                      pattern="\d{10}"
                      title="10-digit mobile number"
                      placeholder="10-digit number"
                      value={form.contactMobile}
                      onChange={(e) => update("contactMobile", e.target.value.replace(/\D/g, "").slice(0, 10))}
                      className="input pl-12 tabular"
                    />
                  </div>
                </Field>
                <Field label="PIN code" error={fieldErrors.pinCode}>
                  <input
                    required
                    inputMode="numeric"
                    autoComplete="postal-code"
                    pattern="\d{6}"
                    title="6-digit PIN code"
                    value={form.pinCode}
                    onChange={(e) => update("pinCode", e.target.value.replace(/\D/g, "").slice(0, 6))}
                    className="input tabular"
                  />
                  <span className="mt-1.5 block min-h-[1.25rem] text-sm" aria-live="polite">
                    {pinComplete && checkingPin && <span className="text-ink-500">Checking delivery…</span>}
                    {area?.isServiceable && (
                      <span className="flex items-center gap-1.5 text-save-700">
                        <CheckCircle2 size={14} /> Delivers to {area.city} in {area.etaMinDays}–{area.etaMaxDays} days
                      </span>
                    )}
                    {notServiceable && (
                      <span className="flex items-center gap-1.5 text-red-700">
                        <AlertTriangle size={14} /> We don't deliver to this PIN code yet
                      </span>
                    )}
                  </span>
                </Field>
                <div className="grid grid-cols-2 gap-4">
                  <Field label="City" error={fieldErrors.city}>
                    <input required autoComplete="address-level2" value={form.city} onChange={(e) => update("city", e.target.value)} className="input" />
                  </Field>
                  <Field label="State" error={fieldErrors.state}>
                    <input required autoComplete="address-level1" value={form.state} onChange={(e) => update("state", e.target.value)} className="input" />
                  </Field>
                </div>
                <div className="sm:col-span-2">
                  <Field label="House / flat, building, street" error={fieldErrors.addressLine1}>
                    <input required autoComplete="address-line1" value={form.addressLine1} onChange={(e) => update("addressLine1", e.target.value)} className="input" />
                  </Field>
                </div>
                <Field label="Area / locality (optional)">
                  <input autoComplete="address-line2" value={form.addressLine2} onChange={(e) => update("addressLine2", e.target.value)} className="input" />
                </Field>
                <Field label="Landmark (optional)">
                  <input value={form.landmark} onChange={(e) => update("landmark", e.target.value)} className="input" />
                </Field>
                <div className="sm:col-span-2">
                  <Field label="Delivery instructions (optional)">
                    <textarea value={form.deliveryNotes} onChange={(e) => update("deliveryNotes", e.target.value)} className="input" rows={2} placeholder="e.g. Call before arriving" />
                  </Field>
                </div>
              </div>
            </Section>

            {/* 2. Delivery */}
            <Section n={2} icon={Truck} title="Delivery">
              <div className="flex items-start gap-3 rounded-xl border-2 border-brand-600 bg-brand-50/50 p-4">
                <span className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full border-[5px] border-brand-600 bg-white" aria-hidden />
                <div className="flex-1">
                  <p className="font-semibold text-ink-900">Standard delivery</p>
                  <p className="text-sm text-ink-600">
                    {area?.isServiceable
                      ? `Arrives in ${area.etaMinDays}–${area.etaMaxDays} days after your order is confirmed`
                      : "Enter your PIN code to see the delivery window"}
                  </p>
                </div>
                <p className="font-semibold text-ink-900 tabular">
                  {area?.isServiceable ? (area.deliveryFeePaise === 0 ? "Free" : formatPaise(area.deliveryFeePaise)) : "—"}
                </p>
              </div>
            </Section>

            {/* Prescription (only when needed) */}
            {cart.requiresPrescription && !isRxBlocked(checkout.error) && (
              <section className="rounded-2xl bg-rx-50 p-5 ring-1 ring-inset ring-rx-200 sm:p-6">
                <div className="flex gap-3">
                  <FileText size={22} className="shrink-0 text-rx-600" />
                  <div className="flex-1 text-sm text-rx-800">
                    <h2 className="font-sans text-base font-semibold text-ink-900">Prescription verification</h2>
                    <p className="mt-1 leading-relaxed">
                      Your order includes prescription medicines. After you place it, a pharmacist checks your prescription before confirming. You
                      won't be asked to pay until that's done.
                    </p>
                    {user ? (
                      usableRx.length > 0 ? (
                        <ul className="mt-3 space-y-1.5">
                          {usableRx.slice(0, 3).map((p) => (
                            <li key={p.id} className="flex items-center gap-2 rounded-lg bg-white px-3 py-2 text-ink-700 ring-1 ring-rx-200">
                              {p.status === "APPROVED" ? <CheckCircle2 size={15} className="text-save-600" /> : <Clock size={15} className="text-rx-600" />}
                              <span className="flex-1 truncate">{p.originalName ?? "Prescription"}</span>
                              <span className="text-xs font-medium">{PRESCRIPTION_STATUS_LABELS[p.status]}</span>
                            </li>
                          ))}
                        </ul>
                      ) : (
                        <Link to="/upload-prescription" className="btn btn-sm mt-3 bg-white text-rx-800 ring-1 ring-rx-200 hover:bg-rx-100">
                          <FileUp size={15} /> Upload prescription
                        </Link>
                      )
                    ) : (
                      <p className="mt-2">
                        <Link to="/login" state={{ from: "/upload-prescription" }} className="font-semibold underline underline-offset-2">
                          Sign in to upload it now
                        </Link>
                        , or share it with our pharmacist on WhatsApp after placing the order.
                      </p>
                    )}
                  </div>
                </div>
              </section>
            )}

            {/* 3. Payment */}
            <Section n={3} icon={CreditCard} title="Payment">
              <ol className="space-y-3 text-sm text-ink-700">
                <li className="flex gap-3">
                  <MessageCircle size={18} className="shrink-0 text-brand-600" />
                  <span>
                    <strong className="text-ink-900">Confirm on WhatsApp.</strong> After you place the order we'll open WhatsApp with your order details
                    so you can confirm it with us.
                  </span>
                </li>
                <li className="flex gap-3">
                  <Lock size={18} className="shrink-0 text-brand-600" />
                  <span>
                    <strong className="text-ink-900">Pay securely.</strong> You'll get a secure link to pay by UPI, card or net banking
                    {area?.codAvailable ? ". Cash on delivery is also available for your PIN code." : "."}
                  </span>
                </li>
              </ol>
              <p className="mt-4 text-xs text-ink-500">Nothing is charged when you place the order. This demo runs payments in test mode.</p>
            </Section>


          </div>

          {/* Summary */}
          <aside aria-label="Order summary" className="lg:sticky lg:top-24 lg:self-start">
            <div className="rounded-2xl border border-ink-200/80 bg-white p-5 sm:p-6">
              <div className="flex items-center justify-between">
                <h2 className="font-sans text-lg font-semibold text-ink-900">Order summary</h2>
                <Link to="/cart" className="text-sm font-semibold text-brand-700 hover:underline">
                  Edit
                </Link>
              </div>
              <ul className="mt-4 max-h-64 space-y-3 overflow-y-auto pr-1">
                {cart.lines.map((l) => (
                  <li key={l.productId} className="flex items-center gap-3">
                    <span className="relative h-12 w-12 shrink-0 overflow-hidden rounded-lg bg-ink-50">
                      <ProductImage src={l.imageUrl} alt="" pad="p-1" />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm font-medium text-ink-900">{titleCase(l.productName)}</span>
                      <span className="block text-xs text-ink-500">Qty {l.qty}{l.requiresPrescription ? " · Rx" : ""}</span>
                    </span>
                    <span className="text-sm font-semibold text-ink-900 tabular">{formatPaise(l.lineTotalPaise)}</span>
                  </li>
                ))}
              </ul>
              <dl className="mt-5 space-y-2.5 border-t border-ink-100 pt-4 text-[15px]">
                <SummaryRow label="Item total (MRP)" value={formatPaise(mrp)} />
                {discount > 0 && <SummaryRow label="Discount on MRP" value={`−${formatPaise(discount)}`} valueClass="text-save-700" />}
                <SummaryRow
                  label="Delivery fee"
                  value={area?.isServiceable ? (deliveryFee === 0 ? "Free" : formatPaise(deliveryFee)) : "Enter PIN"}
                  valueClass={!area?.isServiceable ? "text-ink-500 text-sm" : deliveryFee === 0 ? "text-save-700" : ""}
                />
                <div className="flex items-baseline justify-between border-t border-dashed border-ink-200 pt-3">
                  <dt className="font-semibold text-ink-900">To pay</dt>
                  <dd className="text-xl font-bold text-ink-900 tabular">{formatPaise(total)}</dd>
                </div>
              </dl>
              {discount > 0 && <p className="mt-3 rounded-xl bg-save-50 px-3 py-2 text-sm font-semibold text-save-700">You save {formatPaise(discount)}</p>}
              <button type="submit" disabled={placeDisabled} className="btn btn-primary btn-lg mt-5 hidden w-full md:inline-flex">
                {placeLabel}
              </button>
              <p className="mt-3 flex items-center justify-center gap-1.5 text-xs text-ink-500">
                <Lock size={13} /> Your details are only used to deliver this order
              </p>
            </div>
          </aside>
        </form>
      </div>

      <div className="fixed inset-x-0 bottom-0 z-40 border-t border-ink-200 bg-white/95 px-4 py-3 shadow-bar backdrop-blur pb-safe md:hidden">
        <button type="submit" form="checkout-form" disabled={placeDisabled} className="btn btn-primary btn-lg w-full">
          {placeLabel}
        </button>
      </div>
    </div>
  );
}

// The storefront can disable online sale of prescription medicines
// (FEATURE_RX_SALES_ENABLED). The API then rejects the order with
// `prescription_required` / `rx_sales_disabled`; turn that into a way forward.
function isRxBlocked(err: unknown) {
  return err instanceof ApiError && (err.code === "prescription_required" || err.code === "rx_sales_disabled");
}

function RxBlockedNotice({ onResolved }: { onResolved: () => void }) {
  const { cart } = useCart();
  const setQty = useSetCartQty();
  const saved = useSavedForLater();
  const rxLines = cart?.lines.filter((l) => l.requiresPrescription) ?? [];
  const onlyRx = rxLines.length === cart?.lines.length;

  async function moveRxItems() {
    for (const l of rxLines) {
      saved.add(l.productSlug);
      await setQty.mutateAsync({ productId: l.productId, qty: 0 });
    }
    onResolved();
  }

  return (
    <div role="alert" className="rounded-2xl bg-rx-50 p-5 ring-1 ring-inset ring-rx-200">
      <div className="flex gap-3">
        <AlertTriangle size={20} className="mt-0.5 shrink-0 text-rx-600" />
        <div className="flex-1 text-sm text-rx-800">
          <p className="font-semibold text-ink-900">Prescription medicines can't be ordered online on this storefront yet</p>
          <p className="mt-1">
            {rxLines.map((l) => titleCase(l.productName)).join(", ")} {rxLines.length === 1 ? "needs" : "need"} a prescription.
            {onlyRx ? " Save them for later and contact the pharmacy to order them." : " You can save them for later and order the rest of your cart now."}
          </p>
          <div className="mt-3 flex flex-wrap gap-2">
            <button type="button" onClick={moveRxItems} disabled={setQty.isPending} className="btn btn-sm bg-white text-rx-800 ring-1 ring-rx-200 hover:bg-rx-100">
              {setQty.isPending ? "Moving…" : `Save ${rxLines.length === 1 ? "it" : "them"} for later`}
            </button>
            <Link to="/cart" className="btn btn-sm btn-ghost">
              Back to cart
            </Link>
          </div>
        </div>
      </div>
    </div>
  );
}

function Section({ n, icon: Icon, title, children }: { n: number; icon: typeof MapPin; title: string; children: React.ReactNode }) {
  return (
    <section className="rounded-2xl border border-ink-200/80 bg-white p-5 sm:p-6" aria-labelledby={`step-${n}`}>
      <h2 id={`step-${n}`} className="mb-4 flex items-center gap-3 font-sans text-lg font-semibold text-ink-900">
        <span className="flex h-8 w-8 items-center justify-center rounded-full bg-ink-900 text-sm font-bold text-white">{n}</span>
        {title}
        <Icon size={18} className="ml-auto text-ink-400" aria-hidden />
      </h2>
      {children}
    </section>
  );
}

function Field({ label, error, hint, children }: { label: string; error?: string; hint?: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="label">{label}</span>
      {children}
      {error ? (
        <span className="mt-1.5 block text-sm text-red-700" role="alert">
          {error}
        </span>
      ) : hint ? (
        <span className="mt-1.5 block text-xs text-ink-500">{hint}</span>
      ) : null}
    </label>
  );
}

function SummaryRow({ label, value, valueClass = "" }: { label: string; value: string; valueClass?: string }) {
  return (
    <div className="flex items-baseline justify-between gap-4">
      <dt className="text-ink-600">{label}</dt>
      <dd className={`font-medium text-ink-900 tabular ${valueClass}`}>{value}</dd>
    </div>
  );
}
