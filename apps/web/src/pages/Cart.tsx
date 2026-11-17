import { useEffect } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, ArrowRight, Bookmark, FileText, FileUp, Lock, ShoppingCart, Trash2, Truck } from "lucide-react";
import { api, type Envelope } from "../lib/api";
import { formatPaise } from "../lib/money";
import type { CartLine, CartSummary, Product } from "../types";
import { track } from "../lib/analytics";
import { titleCase } from "../lib/categories";
import { useRecentlyViewed, useSavedForLater, useWishlist } from "../lib/prefs";
import { cartTotals, useAddToCart, useCart, useSetCartQty } from "../hooks/useCart";
import { useDeliveryEstimate } from "../hooks/useDelivery";
import { rankEssentials } from "../lib/catalog";
import ProductRail, { useProductsBySlug } from "../components/ProductRail";
import { ProductImage, QtyStepper } from "../components/product/bits";
import PinSelector from "../components/PinSelector";
import { useToast } from "../components/Toast";

export default function Cart() {
  const navigate = useNavigate();
  const { cart, isLoading, isError } = useCart();
  const { area, eta } = useDeliveryEstimate();

  useEffect(() => {
    document.title = "Your cart | MediCart";
  }, []);
  useEffect(() => {
    if (cart) track("cart_viewed", { itemCount: cart.itemCount, subtotalPaise: cart.subtotalPaise });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cart?.cartId]);

  if (isLoading) return <CartSkeleton />;
  if (isError) {
    return (
      <div className="page max-w-xl py-20 text-center">
        <p className="font-semibold text-ink-800">We couldn't load your cart.</p>
        <button onClick={() => window.location.reload()} className="btn btn-secondary mt-4">
          Try again
        </button>
      </div>
    );
  }

  if (!cart || cart.lines.length === 0) return <EmptyCart />;

  const { mrp, discount } = cartTotals(cart);
  const deliveryFee = area?.isServiceable ? area.deliveryFeePaise : null;
  const total = cart.subtotalPaise + (deliveryFee ?? 0);

  return (
    <div className="pb-28 md:pb-0">
      <div className="page py-6 sm:py-8">
        <h1 className="text-2xl font-bold text-ink-900 sm:text-3xl">
          Your cart <span className="text-ink-400 font-semibold text-xl">({cart.itemCount} item{cart.itemCount === 1 ? "" : "s"})</span>
        </h1>

        <div className="mt-6 grid gap-6 lg:grid-cols-[1fr_24rem] lg:gap-8">
          <div className="space-y-4">
            {cart.requiresPrescription && (
              <div className="flex gap-3 rounded-2xl bg-rx-50 p-4 ring-1 ring-inset ring-rx-200">
                <FileText size={20} className="mt-0.5 shrink-0 text-rx-600" />
                <div className="text-sm text-rx-800">
                  <p className="font-semibold">Your cart has prescription items</p>
                  <p className="mt-0.5">
                    A pharmacist must verify your prescription before the order is confirmed, and payment is requested only after that.
                  </p>
                  <Link to="/upload-prescription" className="mt-1.5 inline-flex items-center gap-1 font-semibold underline underline-offset-2">
                    <FileUp size={14} /> Upload prescription now
                  </Link>
                </div>
              </div>
            )}

            <ul className="divide-y divide-ink-200 rounded-2xl border border-ink-200/80 bg-white">
              {cart.lines.map((line) => (
                <CartRow key={line.productId} line={line} />
              ))}
            </ul>

            <SavedForLater />
          </div>

          <aside aria-label="Order summary" className="lg:sticky lg:top-36 lg:self-start space-y-4">
            <div className="rounded-2xl border border-ink-200/80 bg-white p-5 sm:p-6">
              <h2 className="font-sans text-lg font-semibold text-ink-900">Price details</h2>
              <dl className="mt-4 space-y-3 text-[15px]">
                <Row label={`Item total (MRP)`} value={formatPaise(mrp)} />
                {discount > 0 && <Row label="Discount on MRP" value={`−${formatPaise(discount)}`} valueClass="text-save-700" />}
                <Row
                  label="Delivery fee"
                  value={deliveryFee == null ? "At checkout" : deliveryFee === 0 ? "Free" : formatPaise(deliveryFee)}
                  valueClass={deliveryFee === 0 ? "text-save-700" : deliveryFee == null ? "text-ink-500 text-sm" : ""}
                />
                <div className="flex items-baseline justify-between border-t border-dashed border-ink-200 pt-3">
                  <dt className="font-semibold text-ink-900">{deliveryFee == null ? "Total (before delivery)" : "Total"}</dt>
                  <dd className="text-xl font-bold text-ink-900 tabular">{formatPaise(total)}</dd>
                </div>
              </dl>
              {discount > 0 && (
                <p className="mt-4 rounded-xl bg-save-50 px-3 py-2 text-sm font-semibold text-save-700">
                  You save {formatPaise(discount)} on this order
                </p>
              )}

              <div className="mt-4 space-y-1.5 text-sm">
                <PinSelector compact />
                {eta && (
                  <p className="flex items-center gap-2 text-ink-600">
                    <Truck size={15} className="text-brand-600" /> Estimated delivery in {eta} after confirmation
                  </p>
                )}
              </div>

              {cart.hasIssues && (
                <p className="mt-4 flex items-start gap-2 rounded-xl bg-red-50 px-3 py-2 text-sm text-red-700">
                  <AlertTriangle size={16} className="mt-0.5 shrink-0" /> Some items need attention before you can check out.
                </p>
              )}

              <button onClick={() => navigate("/checkout")} disabled={cart.hasIssues} className="btn btn-primary btn-lg mt-5 hidden w-full md:inline-flex">
                Proceed to checkout <ArrowRight size={18} />
              </button>
              <p className="mt-3 hidden items-center justify-center gap-1.5 text-xs text-ink-500 md:flex">
                <Lock size={13} /> Secure checkout · Prices include taxes
              </p>
            </div>
            <Link to="/products" className="block text-center text-sm font-semibold text-brand-700 hover:underline">
              Continue shopping
            </Link>
          </aside>
        </div>
      </div>

      <div className="page mt-12">
        <CartRecommendations cart={cart} />
      </div>

      {/* Mobile sticky checkout */}
      <div className="fixed inset-x-0 bottom-0 z-40 border-t border-ink-200 bg-white/95 px-4 py-3 shadow-bar backdrop-blur pb-safe md:hidden">
        <div className="flex items-center gap-3">
          <div className="flex-1">
            <p className="text-lg font-bold leading-none text-ink-900 tabular">{formatPaise(total)}</p>
            {discount > 0 ? (
              <p className="mt-1 text-xs font-semibold text-save-700">You save {formatPaise(discount)}</p>
            ) : (
              <p className="mt-1 text-xs text-ink-500">{deliveryFee == null ? "+ delivery" : "incl. delivery"}</p>
            )}
          </div>
          <button onClick={() => navigate("/checkout")} disabled={cart.hasIssues} className="btn btn-primary flex-1">
            Checkout <ArrowRight size={17} />
          </button>
        </div>
      </div>
    </div>
  );
}

function Row({ label, value, valueClass = "" }: { label: string; value: string; valueClass?: string }) {
  return (
    <div className="flex items-baseline justify-between gap-4">
      <dt className="text-ink-600">{label}</dt>
      <dd className={`font-medium text-ink-900 tabular ${valueClass}`}>{value}</dd>
    </div>
  );
}

function CartRow({ line }: { line: CartLine }) {
  const setQty = useSetCartQty();
  const saved = useSavedForLater();
  const toast = useToast();
  const name = titleCase(line.productName);
  const max = Math.max(1, Math.min(line.maxQtyPerOrder, line.availableQty));
  return (
    <li className={`flex gap-4 p-4 sm:p-5 ${setQty.isPending ? "opacity-70" : ""} transition-opacity`}>
      <Link to={`/products/${line.productSlug}`} className="h-20 w-20 shrink-0 overflow-hidden rounded-xl bg-ink-50 sm:h-24 sm:w-24" tabIndex={-1} aria-hidden>
        <ProductImage src={line.imageUrl} alt="" pad="p-2" />
      </Link>
      <div className="min-w-0 flex-1">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <Link to={`/products/${line.productSlug}`} className="font-semibold leading-snug text-ink-900 hover:text-brand-700 line-clamp-2">
              {name}
            </Link>
            <p className="mt-0.5 text-sm text-ink-500">{[line.brandName, line.packSize].filter(Boolean).join(" · ")}</p>
            {line.requiresPrescription && (
              <span className="badge badge-rx mt-1.5">
                <FileText size={12} /> Rx required
              </span>
            )}
          </div>
          <div className="shrink-0 text-right tabular">
            <p className="font-bold text-ink-900">{formatPaise(line.lineTotalPaise)}</p>
            {line.unitMrpPaise > line.unitPricePaise && (
              <p className="text-xs text-ink-400 line-through">{formatPaise(line.unitMrpPaise * line.qty)}</p>
            )}
          </div>
        </div>

        {line.exceeded && (
          <p className="mt-2 flex items-center gap-1.5 text-sm font-medium text-red-700">
            <AlertTriangle size={15} /> Only {line.availableQty} available. Please reduce the quantity.
          </p>
        )}
        {!line.isActive && (
          <p className="mt-2 flex items-center gap-1.5 text-sm font-medium text-red-700">
            <AlertTriangle size={15} /> No longer available. Please remove it to continue.
          </p>
        )}

        <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2">
          <QtyStepper
            size="sm"
            qty={line.qty}
            max={max}
            busy={setQty.isPending}
            label={name}
            // At quantity 1 the "−" becomes a bin icon and removes the line,
            // the same as on product cards (it used to be disabled here).
            onChange={(q) => setQty.mutate({ productId: line.productId, qty: q })}
          />
          <button
            onClick={() => {
              saved.add(line.productSlug);
              setQty.mutate({ productId: line.productId, qty: 0 });
              toast({ tone: "success", title: "Saved for later" });
            }}
            className="inline-flex items-center gap-1.5 text-sm font-medium text-ink-600 hover:text-brand-700"
          >
            <Bookmark size={15} /> Save for later
          </button>
          <button
            onClick={() => setQty.mutate({ productId: line.productId, qty: 0 })}
            className="inline-flex items-center gap-1.5 text-sm font-medium text-ink-600 hover:text-red-700"
            aria-label={`Remove ${name} from cart`}
          >
            <Trash2 size={15} /> Remove
          </button>
        </div>
      </div>
    </li>
  );
}

function SavedForLater() {
  const saved = useSavedForLater();
  const { products, loading } = useProductsBySlug(saved.slugs);
  const add = useAddToCart({ silent: true });
  const toast = useToast();
  if (saved.slugs.length === 0) return null;
  return (
    <section aria-labelledby="saved-title" className="rounded-2xl border border-ink-200/80 bg-white">
      <h2 id="saved-title" className="border-b border-ink-200 px-5 py-4 font-sans text-base font-semibold text-ink-900">
        Saved for later ({saved.slugs.length})
      </h2>
      {loading && products.length === 0 && <div className="skeleton m-5 h-16" />}
      <ul className="divide-y divide-ink-200">
        {products.map((p) => (
          <li key={p.id} className="flex items-center gap-4 p-4 sm:px-5">
            <span className="h-14 w-14 shrink-0 overflow-hidden rounded-xl bg-ink-50">
              <ProductImage src={p.imageUrl} alt="" pad="p-1.5" />
            </span>
            <div className="min-w-0 flex-1">
              <Link to={`/products/${p.slug}`} className="block truncate font-medium text-ink-900 hover:text-brand-700">
                {titleCase(p.name)}
              </Link>
              <p className="text-sm text-ink-600 tabular">
                {formatPaise(p.pricePaise)} {!p.inStock && <span className="text-red-700">· Currently unavailable</span>}
              </p>
            </div>
            <div className="flex shrink-0 items-center gap-1">
              <button
                onClick={() =>
                  add.mutate(
                    { productId: p.id, qty: 1, pricePaise: p.pricePaise },
                    {
                      onSuccess: () => {
                        saved.remove(p.slug);
                        toast({ tone: "success", title: "Moved to cart" });
                      },
                    },
                  )
                }
                disabled={!p.inStock || add.isPending}
                className="btn btn-sm btn-soft"
              >
                Move to cart
              </button>
              <button onClick={() => saved.remove(p.slug)} className="icon-btn h-9 w-9" aria-label={`Remove ${titleCase(p.name)} from saved items`}>
                <Trash2 size={16} />
              </button>
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}

// Complementary items: products the catalogue relates to what's already in
// the cart (first two lines), minus anything already in it.
function CartRecommendations({ cart }: { cart: CartSummary }) {
  const slugs = cart.lines.slice(0, 2).map((l) => l.productSlug);
  const { data: a } = useQuery({
    queryKey: ["product", slugs[0]],
    queryFn: () => api.get<Envelope<{ product: Product; related: Product[] }>>(`/api/v1/products/${slugs[0]}`),
    enabled: !!slugs[0],
    staleTime: 60_000,
  });
  const { data: b } = useQuery({
    queryKey: ["product", slugs[1]],
    queryFn: () => api.get<Envelope<{ product: Product; related: Product[] }>>(`/api/v1/products/${slugs[1]}`),
    enabled: !!slugs[1],
    staleTime: 60_000,
  });
  // Everyday items people commonly add, used to top up the list.
  const { data: otc } = useQuery({
    queryKey: ["products", "otc-recs"],
    queryFn: () => api.get<Envelope<Product[]>>("/api/v1/products?rxOnly=false&pageSize=24"),
    staleTime: 5 * 60_000,
  });
  const inCart = new Set(cart.lines.map((l) => l.productId));
  const seen = new Set<string>();
  // Never upsell prescription medicines: those are decided by a doctor, not
  // by a "you might also need" shelf.
  const recs = [...(a?.data.related ?? []), ...(b?.data.related ?? []), ...rankEssentials(otc?.data ?? [])]
    .filter((p) => {
      if (p.requiresPrescription || !p.inStock || inCart.has(p.id) || seen.has(p.id)) return false;
      seen.add(p.id);
      return true;
    })
    .slice(0, 12);
  return <ProductRail id="cart-recs" title="You might also need" subtitle="Everyday health essentials, no prescription needed" products={recs} />;
}

function EmptyCart() {
  const wishlist = useWishlist();
  const recent = useRecentlyViewed();
  const w = useProductsBySlug(wishlist.slugs.slice(0, 10));
  const r = useProductsBySlug(recent.slice(0, 10));
  return (
    <div className="page py-10">
      <div className="mx-auto max-w-md text-center">
        <span className="mx-auto flex h-20 w-20 items-center justify-center rounded-3xl bg-brand-50 text-brand-600">
          <ShoppingCart size={34} strokeWidth={1.75} />
        </span>
        <h1 className="mt-5 text-2xl font-bold text-ink-900">Your cart is empty</h1>
        <p className="mt-2 text-ink-600">Search for a medicine or browse categories to get started.</p>
        <div className="mt-6 flex flex-col justify-center gap-3 sm:flex-row">
          <Link to="/products" className="btn btn-primary btn-lg">
            Start shopping
          </Link>
          <Link to="/upload-prescription" className="btn btn-secondary btn-lg">
            <FileUp size={18} /> Upload prescription
          </Link>
        </div>
      </div>
      <div className="mt-14 space-y-14">
        <ProductRail id="cart-wishlist" title="From your wishlist" seeAllTo="/wishlist" products={w.products} />
        <ProductRail id="cart-recent" title="Recently viewed" products={r.products} />
      </div>
    </div>
  );
}

function CartSkeleton() {
  return (
    <div className="page py-8" aria-busy="true" aria-label="Loading cart">
      <div className="skeleton h-8 w-48" />
      <div className="mt-6 grid gap-6 lg:grid-cols-[1fr_24rem]">
        <div className="skeleton h-80 rounded-2xl" />
        <div className="skeleton h-72 rounded-2xl" />
      </div>
    </div>
  );
}
