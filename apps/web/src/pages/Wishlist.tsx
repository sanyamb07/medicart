import { useEffect } from "react";
import { Link } from "react-router-dom";
import { Heart } from "lucide-react";
import { useWishlist } from "../lib/prefs";
import ProductCard, { ProductCardSkeleton } from "../components/ProductCard";
import { useProductsBySlug } from "../components/ProductRail";

// Wishlist is stored on this device (see lib/prefs) — prices and stock shown
// here are fetched live, so a saved item never displays a stale price.
export default function Wishlist() {
  const wishlist = useWishlist();
  const { products, loading } = useProductsBySlug(wishlist.slugs);
  useEffect(() => {
    document.title = "Wishlist | MediCart";
  }, []);

  return (
    <div className="page py-8">
      <h1 className="text-2xl font-bold text-ink-900 sm:text-3xl">Your wishlist</h1>
      <p className="mt-1 text-sm text-ink-500">
        {wishlist.slugs.length > 0 ? `${wishlist.slugs.length} saved item${wishlist.slugs.length === 1 ? "" : "s"} · saved on this device` : "Saved on this device"}
      </p>

      {wishlist.slugs.length === 0 ? (
        <div className="mx-auto mt-10 max-w-md rounded-2xl border border-ink-200/80 bg-white p-10 text-center">
          <span className="mx-auto flex h-16 w-16 items-center justify-center rounded-3xl bg-pink-50 text-pink-600">
            <Heart size={28} />
          </span>
          <h2 className="mt-4 text-xl font-bold text-ink-900">Nothing saved yet</h2>
          <p className="mt-2 text-ink-600">Tap the heart on any product to keep it here for later, with its current price and stock.</p>
          <Link to="/products" className="btn btn-primary mt-6">
            Browse products
          </Link>
        </div>
      ) : (
        <ul className="mt-6 grid grid-cols-2 gap-3 sm:grid-cols-3 sm:gap-4 lg:grid-cols-4 xl:grid-cols-5">
          {loading && products.length === 0
            ? wishlist.slugs.slice(0, 8).map((s) => (
                <li key={s}>
                  <ProductCardSkeleton />
                </li>
              ))
            : products.map((p) => (
                <li key={p.id}>
                  <ProductCard product={p} />
                </li>
              ))}
        </ul>
      )}
    </div>
  );
}
