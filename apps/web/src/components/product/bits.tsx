// Small, shared product UI primitives: price block, wishlist toggle, quantity
// stepper and product image. Kept together because they are always used as a
// set and each is a few lines.
import { useState } from "react";
import { Heart, Minus, Pill, Plus, Trash2 } from "lucide-react";
import { formatPaise } from "../../lib/money";
import { useWishlist } from "../../lib/prefs";
import { useToast } from "../Toast";

export function Price({
  pricePaise,
  mrpPaise,
  discountPercent,
  size = "md",
}: {
  pricePaise: number;
  mrpPaise: number;
  discountPercent: number;
  size?: "sm" | "md" | "lg";
}) {
  const main = size === "lg" ? "text-3xl" : size === "sm" ? "text-[15px]" : "text-lg";
  return (
    <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 tabular">
      <span className={`${main} font-bold text-ink-900 leading-none`}>{formatPaise(pricePaise)}</span>
      {discountPercent > 0 && (
        <>
          <span className={`${size === "lg" ? "text-base" : "text-xs"} text-ink-400 line-through`}>
            <span className="sr-only">MRP </span>
            {formatPaise(mrpPaise)}
          </span>
          <span className={`badge badge-save ${size === "lg" ? "text-sm" : ""}`}>{discountPercent}% off</span>
        </>
      )}
    </div>
  );
}

export function WishlistButton({ slug, name, className = "" }: { slug: string; name: string; className?: string }) {
  const wishlist = useWishlist();
  const toast = useToast();
  const [bump, setBump] = useState(false);
  const active = wishlist.has(slug);
  return (
    <button
      type="button"
      aria-pressed={active}
      aria-label={active ? `Remove ${name} from wishlist` : `Save ${name} to wishlist`}
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        const added = wishlist.toggle(slug);
        setBump(true);
        toast(
          added
            ? { tone: "wishlist", title: "Saved to wishlist", action: { label: "View", to: "/wishlist" } }
            : { tone: "success", title: "Removed from wishlist" },
        );
      }}
      onAnimationEnd={() => setBump(false)}
      className={`inline-flex h-9 w-9 items-center justify-center rounded-full bg-white/95 shadow-sm ring-1 ring-ink-200/70 transition-colors hover:ring-pink-200 ${
        active ? "text-pink-600" : "text-ink-400 hover:text-pink-600"
      } ${className}`}
    >
      <Heart size={17} fill={active ? "currentColor" : "none"} className={bump ? "animate-bump" : ""} />
    </button>
  );
}

export function QtyStepper({
  qty,
  max,
  onChange,
  busy,
  size = "md",
  label,
  removeAtOne = true,
}: {
  qty: number;
  max: number;
  onChange: (next: number) => void;
  busy?: boolean;
  size?: "sm" | "md";
  label: string;
  removeAtOne?: boolean;
}) {
  const h = size === "sm" ? "h-9" : "h-11";
  const w = size === "sm" ? "w-9" : "w-11";
  const showTrash = removeAtOne && qty === 1;
  return (
    <div
      className={`inline-flex ${h} items-center rounded-xl bg-brand-50 text-brand-800 ring-1 ring-inset ring-brand-200 ${busy ? "opacity-70" : ""}`}
      role="group"
      aria-label={`Quantity for ${label}`}
    >
      <button
        type="button"
        onClick={() => onChange(qty - 1)}
        disabled={busy || (!removeAtOne && qty <= 1)}
        aria-label={showTrash ? `Remove ${label}` : `Decrease quantity of ${label}`}
        className={`${h} ${w} inline-flex items-center justify-center rounded-l-xl hover:bg-brand-100 disabled:text-brand-300`}
      >
        {showTrash ? <Trash2 size={15} /> : <Minus size={15} />}
      </button>
      <span className="min-w-[2rem] text-center text-sm font-bold tabular" aria-live="polite">
        {qty}
      </span>
      <button
        type="button"
        onClick={() => onChange(qty + 1)}
        disabled={busy || qty >= max}
        aria-label={`Increase quantity of ${label}`}
        className={`${h} ${w} inline-flex items-center justify-center rounded-r-xl hover:bg-brand-100 disabled:text-brand-300`}
      >
        <Plus size={15} />
      </button>
    </div>
  );
}

export function ProductImage({
  src,
  alt,
  className = "",
  pad = "p-5",
  eager = false,
}: {
  src: string | null;
  alt: string;
  className?: string;
  pad?: string;
  eager?: boolean;
}) {
  // State is keyed to the URL so a gallery switching images re-runs the
  // loading shimmer and clears an earlier failure.
  const [state, setState] = useState<{ src: string | null; status: "loading" | "loaded" | "failed" }>({
    src,
    status: "loading",
  });
  const status = state.src === src ? state.status : "loading";
  if (!src || status === "failed") {
    // Neutral fallback: never a picture of some other medicine.
    return (
      <div className={`flex h-full w-full items-center justify-center text-ink-300 ${className}`} aria-hidden>
        <Pill size={36} />
      </div>
    );
  }
  const small = smallVariant(src);
  return (
    <div className="relative h-full w-full">
      {status === "loading" && (
        <div className="absolute inset-0 animate-pulse rounded-[inherit] bg-ink-100/70" aria-hidden />
      )}
      <img
        src={src}
        srcSet={small ? `${small} 400w, ${src} 1200w` : undefined}
        sizes={small ? "(min-width: 1024px) 40vw, (min-width: 640px) 33vw, 50vw" : undefined}
        alt={alt}
        width={240}
        height={240}
        loading={eager ? "eager" : "lazy"}
        fetchPriority={eager ? "high" : undefined}
        decoding="async"
        onLoad={() => setState({ src, status: "loaded" })}
        onError={() => setState({ src, status: "failed" })}
        className={`h-full w-full object-contain transition-opacity duration-300 ${status === "loaded" ? "opacity-100" : "opacity-0"} ${pad} ${className}`}
      />
    </div>
  );
}

// Product photos stored by the catalogue pipeline have a 400 px card-size
// variant next to them ("<name>.webp" + "<name>-400.webp").
function smallVariant(src: string): string | null {
  return /\/products\/IMP-\d{4}\/[a-z0-9-]+\.webp$/.test(src) ? src.replace(/\.webp$/, "-400.webp") : null;
}
