import { useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { useQueries } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { api, type Envelope } from "../lib/api";
import type { Product } from "../types";
import ProductCard, { ProductCardSkeleton } from "./ProductCard";

interface RailProps {
  id?: string;
  title: string;
  subtitle?: string;
  seeAllTo?: string;
  products: Product[] | undefined;
  loading?: boolean;
  /** Optional element at the right of the heading (e.g. a "Clear" button). */
  action?: React.ReactNode;
}

// A horizontally scrolling product row. Native scroll-snap does the work so
// it stays smooth on low-end phones; arrow buttons appear on pointer devices.
export default function ProductRail({ id, title, subtitle, seeAllTo, products, loading, action }: RailProps) {
  const scroller = useRef<HTMLDivElement>(null);
  const [edges, setEdges] = useState({ start: true, end: false });

  function updateEdges() {
    const el = scroller.current;
    if (!el) return;
    setEdges({ start: el.scrollLeft < 8, end: el.scrollLeft + el.clientWidth >= el.scrollWidth - 8 });
  }
  useEffect(updateEdges, [products]);

  function scrollBy(dir: 1 | -1) {
    const el = scroller.current;
    if (el) el.scrollBy({ left: dir * el.clientWidth * 0.85, behavior: "smooth" });
  }

  if (!loading && (!products || products.length === 0)) return null;
  const headingId = id ? `${id}-title` : undefined;

  return (
    <section aria-labelledby={headingId} className="relative">
      <div className="mb-4 flex items-end justify-between gap-4">
        <div className="min-w-0">
          <h2 id={headingId} className="section-title">
            {title}
          </h2>
          {subtitle && <p className="section-sub">{subtitle}</p>}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {action}
          {seeAllTo && (
            <Link to={seeAllTo} className="text-sm font-semibold text-brand-700 hover:text-brand-800 whitespace-nowrap">
              See all
            </Link>
          )}
          <div className="hidden md:flex items-center gap-1.5 ml-2">
            <button
              type="button"
              onClick={() => scrollBy(-1)}
              disabled={edges.start}
              aria-label={`Scroll ${title} left`}
              className="icon-btn h-9 w-9 border border-ink-200 bg-white disabled:opacity-40"
            >
              <ChevronLeft size={18} />
            </button>
            <button
              type="button"
              onClick={() => scrollBy(1)}
              disabled={edges.end}
              aria-label={`Scroll ${title} right`}
              className="icon-btn h-9 w-9 border border-ink-200 bg-white disabled:opacity-40"
            >
              <ChevronRight size={18} />
            </button>
          </div>
        </div>
      </div>

      <div
        ref={scroller}
        onScroll={updateEdges}
        className="no-scrollbar -mx-4 flex snap-x snap-mandatory gap-3 overflow-x-auto scroll-px-4 px-4 pb-2 sm:-mx-6 sm:scroll-px-6 sm:px-6 lg:mx-0 lg:px-0 lg:scroll-px-0 sm:gap-4"
      >
        {loading
          ? Array.from({ length: 6 }).map((_, i) => (
              <div key={i} className="w-[46%] shrink-0 snap-start sm:w-[30%] md:w-[23%] lg:w-[calc((100%-5*1rem)/6)]">
                <ProductCardSkeleton />
              </div>
            ))
          : products!.map((p) => (
              <div key={p.id} className="w-[46%] shrink-0 snap-start sm:w-[30%] md:w-[23%] lg:w-[calc((100%-5*1rem)/6)]">
                <ProductCard product={p} />
              </div>
            ))}
      </div>
    </section>
  );
}

/**
 * Resolves locally-remembered slugs (wishlist, recently viewed) to live
 * product data. Shares the ["product", slug] cache with the product page, so
 * anything already visited costs no extra request.
 */
export function useProductsBySlug(slugs: string[]) {
  const results = useQueries({
    queries: slugs.map((slug) => ({
      queryKey: ["product", slug],
      queryFn: () => api.get<Envelope<{ product: Product; related: Product[] }>>(`/api/v1/products/${slug}`),
      staleTime: 60_000,
      retry: false,
    })),
  });
  const loading = results.some((r) => r.isLoading);
  const products = results.map((r) => r.data?.data.product).filter((p): p is Product => !!p);
  return { products, loading };
}
