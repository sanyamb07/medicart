import { Link } from "react-router-dom";
import { FileText, Plus, Truck } from "lucide-react";
import type { Product } from "../types";
import { useAddToCart, useCart, useSetCartQty } from "../hooks/useCart";
import { useDeliveryEstimate } from "../hooks/useDelivery";
import { titleCase } from "../lib/categories";
import { ProductImage, QtyStepper, WishlistButton } from "./product/bits";
import { formatPaise } from "../lib/money";
import { marketerShort } from "../lib/catalog";

// The storefront's one product card. Information order follows how people
// actually scan a pharmacy listing: what is it (name, pack) → is it for me
// (salt, Rx) → what does it cost → can I get it → act.
export default function ProductCard({ product, eager = false }: { product: Product; eager?: boolean }) {
  const { qtyOf } = useCart();
  const add = useAddToCart();
  const setQty = useSetCartQty();
  const { eta } = useDeliveryEstimate();
  const inCart = qtyOf(product.id);
  const name = titleCase(product.name);
  const href = `/products/${product.slug}`;
  // Strength is often already in the name ("Paracip 500mg"); don't repeat it.
  const squash = (v: string) => v.toLowerCase().replace(/\s+/g, "");
  const strength = product.strength && !squash(product.name).includes(squash(product.strength)) ? product.strength : null;
  const meta = [product.dosageForm && titleCase(product.dosageForm), strength, product.packSize].filter(Boolean).join(" · ");
  const maker = marketerShort(product.manufacturerInfo);
  // The drawn form illustration is a stand-in, not the pack: render it
  // smaller and quieter so real pack photos clearly lead a shelf.
  const isIllustration = !product.imageUrl || product.imageUrl.startsWith("/img/");

  return (
    <article className="group relative flex h-full flex-col rounded-2xl border border-ink-200/80 bg-white p-3 transition-[box-shadow,border-color] duration-200 hover:border-ink-300/80 hover:shadow-cardHover">
      <div className="relative">
        <Link
          to={href}
          tabIndex={-1}
          aria-hidden
          className={`block aspect-[4/3] overflow-hidden rounded-xl ${
            isIllustration ? "bg-ink-50 [&_img]:saturate-[.55] [&_img]:brightness-105" : "bg-white ring-1 ring-inset ring-ink-100"
          }`}
        >
          <ProductImage
            src={product.imageUrl}
            alt=""
            eager={eager}
            pad={isIllustration ? "p-8" : "p-2"}
            className="transition-transform duration-300 group-hover:scale-[1.04]"
          />
        </Link>
        {product.requiresPrescription && (
          <span className="badge badge-rx absolute left-2 top-2" title="Prescription required">
            <FileText size={12} aria-hidden /> Rx
          </span>
        )}
        <WishlistButton slug={product.slug} name={name} className="absolute right-2 top-2 z-10" />
      </div>

      <div className="flex flex-1 flex-col pt-3">
        <p className="truncate text-xs font-medium text-ink-500">{meta || "\u00a0"}</p>
        <h3 className="mt-0.5 min-h-[2.5rem] text-[15px] font-semibold leading-5 text-ink-900 line-clamp-2 font-sans tracking-normal">
          <Link to={href} className="after:absolute after:inset-0 after:rounded-2xl focus-visible:outline-none">
            {name}
          </Link>
        </h3>
        {/* The salt is what a pharmacy shopper compares on; always show it. */}
        <p className="mt-0.5 truncate text-xs text-ink-500" title={maker ? product.manufacturerInfo ?? undefined : undefined}>
          {product.brandName ?? (maker ? <>By {maker}</> : "\u00a0")}
        </p>
        <p className="mt-1 truncate text-xs text-ink-700" title={product.genericName ?? undefined}>
          {product.genericName ? (
            <>
              <span className="text-ink-400">Contains </span>
              {titleCase(product.genericName)}
            </>
          ) : (
            "\u00a0"
          )}
        </p>

        <div className="mt-auto pt-3">
          {/* Fixed two-line price block so every card in a row lines up. */}
          <div className="flex items-center gap-2 whitespace-nowrap tabular">
            <span className="text-lg font-bold leading-none text-ink-900">{formatPaise(product.pricePaise)}</span>
            {product.discountPercent > 0 && <span className="badge badge-save">{product.discountPercent}% off</span>}
          </div>
          <p className="mt-1 text-xs text-ink-500 tabular">
            {product.discountPercent > 0 ? (
              <>
                MRP <span className="line-through">{formatPaise(product.mrpPaise)}</span>
              </>
            ) : (
              "MRP, incl. taxes"
            )}
          </p>
          <p className="mt-1.5 flex items-center gap-1 text-xs text-ink-500 min-h-[1rem]">
            {!product.inStock ? (
              <span className="font-medium text-red-700">Currently unavailable</span>
            ) : eta ? (
              <>
                <Truck size={13} className="text-brand-600" aria-hidden /> Delivery in {eta}
              </>
            ) : (
              <span className="text-save-700 font-medium">In stock</span>
            )}
          </p>

          {/* relative z-10 lifts actions above the card-wide link overlay */}
          <div className="relative z-10 mt-3">
            {inCart > 0 ? (
              <QtyStepper
                size="sm"
                qty={inCart}
                max={Math.min(product.maxQtyPerOrder, product.availableQty)}
                busy={setQty.isPending}
                label={name}
                onChange={(q) => setQty.mutate({ productId: product.id, qty: q })}
              />
            ) : (
              <button
                type="button"
                onClick={() => add.mutate({ productId: product.id, qty: 1, pricePaise: product.pricePaise, name })}
                disabled={!product.inStock || add.isPending}
                aria-label={`Add ${name} to cart`}
                className="btn btn-sm w-full bg-brand-50 text-brand-800 hover:bg-brand-600 hover:text-white disabled:text-ink-400 disabled:bg-ink-50"
              >
                {!product.inStock ? "Out of stock" : add.isPending ? "Adding…" : (
                  <>
                    <Plus size={15} strokeWidth={2.5} /> Add to cart
                  </>
                )}
              </button>
            )}
          </div>
        </div>
      </div>
    </article>
  );
}

export function ProductCardSkeleton() {
  return (
    <div className="rounded-2xl border border-ink-200/80 bg-white p-3" aria-hidden>
      <div className="skeleton aspect-[4/3] rounded-xl" />
      <div className="skeleton mt-3 h-3 w-1/3" />
      <div className="skeleton mt-2 h-4 w-full" />
      <div className="skeleton mt-1.5 h-4 w-2/3" />
      <div className="skeleton mt-5 h-5 w-1/2" />
      <div className="skeleton mt-3 h-9 w-full rounded-lg" />
    </div>
  );
}
