import { useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, FlaskConical, SearchX, SlidersHorizontal, X } from "lucide-react";
import { api, type Envelope } from "../lib/api";
import type { Category, Product } from "../types";
import ProductCard, { ProductCardSkeleton } from "../components/ProductCard";
import { track } from "../lib/analytics";
import { categoryLook, SEARCH_SUGGESTIONS, titleCase } from "../lib/categories";

const PAGE_SIZE = 24;

const SORTS = [
  ["relevance", "Relevance"],
  ["price_asc", "Price: low to high"],
  ["price_desc", "Price: high to low"],
  ["name_asc", "Name: A to Z"],
  ["newest", "Newest"],
] as const;

const PRICE_BANDS = [
  { label: "Under ₹100", min: "", max: "100" },
  { label: "₹100 – ₹500", min: "100", max: "500" },
  { label: "₹500 – ₹1,000", min: "500", max: "1000" },
  { label: "Over ₹1,000", min: "1000", max: "" },
];

interface Brand {
  id: string;
  name: string;
  slug: string;
}

export default function ProductListing() {
  const [params, setParams] = useSearchParams();
  const q = params.get("q") ?? "";
  const category = params.get("category") ?? "";
  const brand = params.get("brand") ?? "";
  const rx = params.get("rx") ?? ""; // "" | "otc" | "rx"
  const min = params.get("min") ?? "";
  const max = params.get("max") ?? "";
  const sort = params.get("sort") ?? "relevance";
  const page = Math.max(1, Number(params.get("page") ?? "1") || 1);
  const [sheetOpen, setSheetOpen] = useState(false);

  const { data: categories } = useQuery({
    queryKey: ["categories"],
    queryFn: () => api.get<Envelope<Category[]>>("/api/v1/categories"),
    staleTime: 5 * 60_000,
  });
  const { data: brands } = useQuery({
    queryKey: ["brands"],
    queryFn: () => api.get<Envelope<Brand[]>>("/api/v1/brands"),
    staleTime: 5 * 60_000,
  });

  const { data, isLoading, isError, isFetching, refetch } = useQuery({
    queryKey: ["products", { q, category, brand, rx, min, max, sort, page }],
    queryFn: () => {
      const qs = new URLSearchParams();
      if (q) qs.set("q", q);
      if (category) qs.set("category", category);
      if (brand) qs.set("brand", brand);
      if (rx) qs.set("rxOnly", rx === "rx" ? "true" : "false");
      if (min) qs.set("minPrice", min);
      if (max) qs.set("maxPrice", max);
      qs.set("sort", sort);
      qs.set("page", String(page));
      qs.set("pageSize", String(PAGE_SIZE));
      return api.get<Envelope<Product[]>>(`/api/v1/products?${qs.toString()}`);
    },
    placeholderData: keepPreviousData,
  });

  const products = data?.data ?? [];
  const total = data?.meta?.totalItems ?? 0;
  const totalPages = data?.meta?.totalPages ?? 1;

  useEffect(() => {
    if (!data || isLoading) return;
    if (q && data.data.length === 0) track("search_no_results", { query_len: q.length });
  }, [data, isLoading, q]);

  const categoryName = categories?.data.find((c) => c.slug === category)?.name;
  const brandName = brands?.data.find((b) => b.slug === brand)?.name;
  const title = q ? `Results for “${q}”` : categoryName ?? (rx === "otc" ? "Everyday essentials" : rx === "rx" ? "Prescription medicines" : "All products");

  useEffect(() => {
    document.title = `${q ? `${q} – search` : title} | MediCart`;
  }, [q, title]);

  function update(patch: Record<string, string>, resetPage = true) {
    const next = new URLSearchParams(params);
    for (const [k, v] of Object.entries(patch)) {
      if (v) next.set(k, v);
      else next.delete(k);
    }
    if (resetPage) next.delete("page");
    setParams(next);
  }

  function goToPage(p: number) {
    update({ page: p > 1 ? String(p) : "" }, false);
    window.scrollTo({ top: 0, behavior: "smooth" });
  }

  // GoodRx-style salt comparison: when a search lands on several brands that
  // share one composition, offer to line them up by price.
  const saltMatch =
    q && products.length > 1
      ? (() => {
          const counts = new Map<string, number>();
          for (const p of products) if (p.genericName) counts.set(p.genericName, (counts.get(p.genericName) ?? 0) + 1);
          const [salt, n] = [...counts.entries()].sort((a, b) => b[1] - a[1])[0] ?? [];
          return salt && n && n >= 2 ? { salt, n } : null;
        })()
      : null;

  const activeChips = [
    category && { key: "category", label: categoryName ?? category },
    brand && { key: "brand", label: brandName ?? brand },
    rx && { key: "rx", label: rx === "otc" ? "No prescription needed" : "Prescription only" },
    (min || max) && { key: "price", label: PRICE_BANDS.find((b) => b.min === min && b.max === max)?.label ?? `₹${min || 0} – ₹${max || "∞"}` },
  ].filter(Boolean) as { key: string; label: string }[];

  const filters = (
    <Filters
      categories={categories?.data ?? []}
      // Only brands that appear in these results: most catalogue items are
      // listed by product name without a brand record, so a full brand list
      // would offer filters that match almost nothing.
      brands={(brands?.data ?? []).filter((b) => b.slug === brand || products.some((p) => p.brandSlug === b.slug))}
      category={category}
      brand={brand}
      rx={rx}
      min={min}
      max={max}
      update={update}
    />
  );

  return (
    <div className="page py-6 sm:py-8">
      <nav aria-label="Breadcrumb" className="mb-3 text-sm text-ink-500">
        <ol className="flex flex-wrap items-center gap-1.5">
          <li>
            <Link to="/" className="hover:text-ink-800">
              Home
            </Link>
          </li>
          <li aria-hidden>/</li>
          <li>
            <Link to="/products" className="hover:text-ink-800">
              Products
            </Link>
          </li>
          {(categoryName || q) && (
            <>
              <li aria-hidden>/</li>
              <li className="text-ink-800 font-medium truncate max-w-[14rem]" aria-current="page">
                {q ? "Search" : categoryName}
              </li>
            </>
          )}
        </ol>
      </nav>

      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold text-ink-900 sm:text-3xl">{title}</h1>
          <p className="mt-1 text-sm text-ink-500" aria-live="polite">
            {isLoading ? "Loading products…" : `${total.toLocaleString("en-IN")} product${total === 1 ? "" : "s"}`}
          </p>
        </div>
      </div>

      {saltMatch && (
        <div className="mt-4 flex items-center gap-3 rounded-2xl border border-brand-200 bg-brand-50 p-3 sm:p-4">
          <span className="hidden h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-white text-brand-700 ring-1 ring-brand-200 sm:flex">
            <FlaskConical size={20} />
          </span>
          <p className="flex-1 text-sm text-ink-700">
            <strong className="text-ink-900">{saltMatch.n} brands contain {titleCase(saltMatch.salt)}.</strong>
            <span className="hidden sm:inline"> Same salt can mean very different prices. Check strength and form match your prescription before switching.</span>
          </p>
          <Link to={`/compare?salt=${encodeURIComponent(saltMatch.salt)}`} className="btn btn-sm btn-secondary shrink-0">
            Compare brands
          </Link>
          {sort !== "price_asc" && (
            <button onClick={() => update({ sort: "price_asc" })} className="btn btn-sm btn-primary shrink-0">
              <span className="sm:hidden">Cheapest first</span>
              <span className="hidden sm:inline">Show lowest price first</span>
            </button>
          )}
        </div>
      )}

      <div className="mt-6 grid gap-8 lg:grid-cols-[15.5rem_1fr]">
        <aside className="hidden lg:block" aria-label="Filters">
          <div className="sticky top-36">{filters}</div>
        </aside>

        <div className="min-w-0">
          {/* Toolbar */}
          <div className="sticky top-[var(--header-h,7.5rem)] lg:static z-20 -mx-4 mb-4 flex items-center gap-2 border-b border-ink-200/70 bg-canvas/95 px-4 py-2.5 backdrop-blur sm:-mx-6 sm:px-6 lg:mx-0 lg:border-0 lg:bg-transparent lg:p-0 lg:backdrop-blur-none">
            <button onClick={() => setSheetOpen(true)} className="btn btn-sm btn-secondary lg:hidden">
              <SlidersHorizontal size={15} /> Filters
              {activeChips.length > 0 && (
                <span className="inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-brand-600 px-1 text-xs text-white">
                  {activeChips.length}
                </span>
              )}
            </button>
            <div className="no-scrollbar hidden flex-1 items-center gap-2 overflow-x-auto sm:flex">
              {activeChips.map((c) => (
                <button
                  key={c.key}
                  onClick={() => update(c.key === "price" ? { min: "", max: "" } : { [c.key]: "" })}
                  className="chip h-8 bg-brand-50 border-brand-200 text-brand-800 text-[13px]"
                  aria-label={`Remove filter: ${c.label}`}
                >
                  {c.label} <X size={14} />
                </button>
              ))}
              {activeChips.length > 1 && (
                <button onClick={() => update({ category: "", brand: "", rx: "", min: "", max: "" })} className="text-sm font-medium text-ink-500 hover:text-ink-800 whitespace-nowrap">
                  Clear all
                </button>
              )}
            </div>
            <label className="ml-auto flex items-center gap-2 text-sm text-ink-500">
              <span className="hidden sm:inline">Sort by</span>
              <select
                value={sort}
                onChange={(e) => update({ sort: e.target.value === "relevance" ? "" : e.target.value })}
                className="h-9 rounded-lg border border-ink-200 bg-white pl-3 pr-8 text-sm font-medium text-ink-800 focus:border-brand-500 focus:ring-4 focus:ring-brand-100 outline-none"
              >
                {SORTS.map(([v, l]) => (
                  <option key={v} value={v}>
                    {l}
                  </option>
                ))}
              </select>
            </label>
          </div>

          {isLoading && (
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 sm:gap-4 xl:grid-cols-4">
              {Array.from({ length: 8 }).map((_, i) => (
                <ProductCardSkeleton key={i} />
              ))}
            </div>
          )}

          {isError && (
            <div className="panel p-10 text-center">
              <p className="font-semibold text-ink-800">We couldn't load products right now.</p>
              <p className="mt-1 text-sm text-ink-500">Check your connection and try again.</p>
              <button onClick={() => refetch()} className="btn btn-secondary mt-5">
                Try again
              </button>
            </div>
          )}

          {!isLoading && !isError && products.length === 0 && (
            <EmptyResults q={q} hasFilters={activeChips.length > 0} clear={() => update({ category: "", brand: "", rx: "", min: "", max: "" })} categories={categories?.data ?? []} />
          )}

          {!isLoading && !isError && products.length > 0 && (
            <>
              <ul className={`grid grid-cols-2 gap-3 sm:grid-cols-3 sm:gap-4 xl:grid-cols-4 transition-opacity ${isFetching ? "opacity-60" : ""}`}>
                {products.map((p, i) => (
                  <li key={p.id}>
                    <ProductCard product={p} eager={i < 4} />
                  </li>
                ))}
              </ul>
              {totalPages > 1 && <Pagination page={page} totalPages={totalPages} onPage={goToPage} />}
            </>
          )}
        </div>
      </div>

      {sheetOpen && (
        <div className="fixed inset-0 z-50 lg:hidden" role="dialog" aria-modal="true" aria-label="Filters">
          <div className="absolute inset-0 bg-ink-900/40 animate-fade-in" onClick={() => setSheetOpen(false)} />
          <div className="absolute inset-x-0 bottom-0 flex max-h-[85vh] flex-col rounded-t-3xl bg-white animate-sheet-up">
            <div className="flex items-center justify-between border-b border-ink-200 px-5 py-4">
              <h2 className="text-lg font-bold">Filters</h2>
              <button onClick={() => setSheetOpen(false)} className="icon-btn" aria-label="Close filters">
                <X size={20} />
              </button>
            </div>
            <div className="flex-1 overflow-y-auto px-5 py-4">{filters}</div>
            <div className="flex gap-3 border-t border-ink-200 px-5 py-3 pb-safe">
              <button onClick={() => update({ category: "", brand: "", rx: "", min: "", max: "" })} className="btn btn-secondary flex-1">
                Clear all
              </button>
              <button onClick={() => setSheetOpen(false)} className="btn btn-primary flex-[2]">
                Show {total.toLocaleString("en-IN")} results
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function Filters({
  categories,
  brands,
  category,
  brand,
  rx,
  min,
  max,
  update,
}: {
  categories: Category[];
  brands: Brand[];
  category: string;
  brand: string;
  rx: string;
  min: string;
  max: string;
  update: (patch: Record<string, string>) => void;
}) {
  return (
    <div className="space-y-7">
      <FilterGroup title="Category">
        <Radio name="category" checked={!category} onChange={() => update({ category: "" })} label="All categories" />
        {categories.map((c) => {
          const Icon = categoryLook(c.slug).icon;
          return (
            <Radio
              key={c.id}
              name="category"
              checked={category === c.slug}
              onChange={() => update({ category: c.slug })}
              label={
                <span className="flex flex-1 items-center gap-2">
                  <Icon size={15} className="text-ink-400" /> {c.name}
                  <span className="ml-auto text-xs text-ink-400 tabular">{c.productCount}</span>
                </span>
              }
            />
          );
        })}
      </FilterGroup>

      <FilterGroup title="Prescription">
        <Radio name="rx" checked={!rx} onChange={() => update({ rx: "" })} label="Show all" />
        <Radio name="rx" checked={rx === "otc"} onChange={() => update({ rx: "otc" })} label="No prescription needed" />
        <Radio name="rx" checked={rx === "rx"} onChange={() => update({ rx: "rx" })} label="Prescription only (Rx)" />
      </FilterGroup>

      <FilterGroup title="Price">
        <Radio name="price" checked={!min && !max} onChange={() => update({ min: "", max: "" })} label="Any price" />
        {PRICE_BANDS.map((b) => (
          <Radio key={b.label} name="price" checked={min === b.min && max === b.max} onChange={() => update({ min: b.min, max: b.max })} label={b.label} />
        ))}
      </FilterGroup>

      {brands.length > 0 && (
        <FilterGroup title="Brand">
          <Radio name="brand" checked={!brand} onChange={() => update({ brand: "" })} label="All brands" />
          {brands.map((b) => (
            <Radio key={b.id} name="brand" checked={brand === b.slug} onChange={() => update({ brand: b.slug })} label={b.name} />
          ))}
        </FilterGroup>
      )}
    </div>
  );
}

function FilterGroup({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <fieldset>
      <legend className="mb-2 font-sans text-sm font-semibold text-ink-900">{title}</legend>
      <div className="space-y-0.5">{children}</div>
    </fieldset>
  );
}

function Radio({ name, checked, onChange, label }: { name: string; checked: boolean; onChange: () => void; label: React.ReactNode }) {
  return (
    <label className={`flex cursor-pointer items-center gap-2.5 rounded-lg px-2 py-1.5 text-sm hover:bg-ink-100 ${checked ? "font-semibold text-ink-900" : "text-ink-700"}`}>
      <input type="radio" name={name} checked={checked} onChange={onChange} className="h-4 w-4 accent-brand-600" />
      {label}
    </label>
  );
}

function Pagination({ page, totalPages, onPage }: { page: number; totalPages: number; onPage: (p: number) => void }) {
  const pages = new Set([1, totalPages, page - 1, page, page + 1].filter((p) => p >= 1 && p <= totalPages));
  const sorted = [...pages].sort((a, b) => a - b);
  return (
    <nav aria-label="Pagination" className="mt-10 flex items-center justify-center gap-1.5">
      <button onClick={() => onPage(page - 1)} disabled={page <= 1} className="btn btn-sm btn-secondary disabled:opacity-40" aria-label="Previous page">
        <ChevronLeft size={16} /> <span className="hidden sm:inline">Previous</span>
      </button>
      {sorted.map((p, i) => (
        <span key={p} className="flex items-center gap-1.5">
          {i > 0 && p - sorted[i - 1] > 1 && <span className="px-1 text-ink-400">…</span>}
          <button
            onClick={() => onPage(p)}
            aria-current={p === page ? "page" : undefined}
            className={`h-9 min-w-9 rounded-lg px-2 text-sm font-semibold tabular ${p === page ? "bg-ink-900 text-white" : "text-ink-700 hover:bg-ink-100"}`}
          >
            {p}
          </button>
        </span>
      ))}
      <button onClick={() => onPage(page + 1)} disabled={page >= totalPages} className="btn btn-sm btn-secondary disabled:opacity-40" aria-label="Next page">
        <span className="hidden sm:inline">Next</span> <ChevronRight size={16} />
      </button>
    </nav>
  );
}

function EmptyResults({ q, hasFilters, clear, categories }: { q: string; hasFilters: boolean; clear: () => void; categories: Category[] }) {
  return (
    <div className="panel px-6 py-12 text-center">
      <span className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-ink-100 text-ink-400">
        <SearchX size={26} />
      </span>
      <h2 className="mt-4 text-xl font-bold text-ink-900">{q ? `No matches for “${q}”` : "No products match these filters"}</h2>
      <p className="mx-auto mt-2 max-w-md text-ink-600">
        {hasFilters
          ? "Try removing a filter to see more results."
          : "Check the spelling, or search by the salt name printed on your prescription (for example “paracetamol”)."}
      </p>
      {hasFilters && (
        <button onClick={clear} className="btn btn-primary mt-5">
          Clear filters
        </button>
      )}
      <div className="mt-8">
        <p className="text-sm font-medium text-ink-500">Try one of these</p>
        <div className="mt-3 flex flex-wrap justify-center gap-2">
          {SEARCH_SUGGESTIONS.slice(0, 4).map((s) => (
            <Link key={s} to={`/products?q=${encodeURIComponent(s)}`} className="chip">
              {s}
            </Link>
          ))}
          {categories.slice(0, 3).map((c) => (
            <Link key={c.id} to={`/products?category=${c.slug}`} className="chip">
              {c.name}
            </Link>
          ))}
        </div>
      </div>
    </div>
  );
}
