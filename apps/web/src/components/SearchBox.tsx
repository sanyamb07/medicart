import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ArrowUpLeft, Clock, FlaskConical, LayoutGrid, Loader2, Search, TrendingUp, X } from "lucide-react";
import { api, type Envelope } from "../lib/api";
import type { Category, Product } from "../types";
import { formatPaise } from "../lib/money";
import { track } from "../lib/analytics";
import { useSearchHistory } from "../lib/prefs";
import { categoryLook, SEARCH_SUGGESTIONS, titleCase } from "../lib/categories";
import { ProductImage } from "./product/bits";
import { marketerShort } from "../lib/catalog";

type Option =
  | { kind: "recent"; label: string }
  | { kind: "suggest"; label: string }
  | { kind: "category"; label: string; slug: string }
  | { kind: "product"; product: Product }
  | { kind: "generic"; label: string; count: number }
  | { kind: "all"; label: string };

interface Props {
  variant?: "header" | "hero";
  autoFocus?: boolean;
  /** Called after a navigation happens (e.g. to close a mobile overlay). */
  onDone?: () => void;
}

// Combobox search used in the header and the homepage hero.
//
// What the user sees while typing:
//   1. matching categories   2. live product matches (with price and Rx)
//   3. "same salt" groups, which double as generic-alternative discovery
//   4. a "see all results" escape hatch
// With an empty box: recent searches (this browser) and starter ideas.
// Typo tolerance comes from the API (Postgres trigram fallback), so a
// misspelling like "paracetmol" still returns paracetamol products here.
export default function SearchBox({ variant = "header", autoFocus, onDone }: Props) {
  const navigate = useNavigate();
  const history = useSearchHistory();
  const listId = useId();
  const [q, setQ] = useState("");
  const [debounced, setDebounced] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const root = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const t = setTimeout(() => setDebounced(q.trim()), 180);
    return () => clearTimeout(t);
  }, [q]);

  useEffect(() => {
    function onDown(e: MouseEvent) {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false);
    }
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, []);

  // "/" focuses search from anywhere, a common shortcut on large catalogues.
  useEffect(() => {
    if (variant !== "header") return;
    function onKey(e: KeyboardEvent) {
      const t = e.target as HTMLElement;
      if (e.key === "/" && !["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName) && !t.isContentEditable) {
        e.preventDefault();
        input.current?.focus();
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [variant]);

  const { data: categories } = useQuery({
    queryKey: ["categories"],
    queryFn: () => api.get<Envelope<Category[]>>("/api/v1/categories"),
    staleTime: 5 * 60_000,
  });

  const typing = debounced.length >= 2;
  const { data: results, isFetching } = useQuery({
    queryKey: ["suggest", debounced],
    queryFn: () => api.get<Envelope<Product[]>>(`/api/v1/products?q=${encodeURIComponent(debounced)}&pageSize=12`),
    enabled: typing,
    staleTime: 30_000,
    placeholderData: (prev) => prev,
  });

  const options = useMemo<Option[]>(() => {
    if (!typing) {
      return [
        ...history.items.slice(0, 5).map((label) => ({ kind: "recent" as const, label })),
        ...SEARCH_SUGGESTIONS.filter((s) => !history.items.some((h) => h.toLowerCase() === s.toLowerCase())).map(
          (label) => ({ kind: "suggest" as const, label }),
        ),
      ];
    }
    const needle = debounced.toLowerCase();
    const cats = (categories?.data ?? [])
      .filter((c) => c.name.toLowerCase().includes(needle))
      .slice(0, 2)
      .map((c) => ({ kind: "category" as const, label: c.name, slug: c.slug }));
    const prods = (results?.data ?? []).slice(0, 6).map((product) => ({ kind: "product" as const, product }));

    // Group the result set by salt. Several brands sharing one salt is
    // exactly the "generic alternatives" a shopper wants to compare.
    const salts = new Map<string, number>();
    for (const p of results?.data ?? []) {
      if (p.genericName) salts.set(p.genericName, (salts.get(p.genericName) ?? 0) + 1);
    }
    const generics = [...salts.entries()]
      .sort((a, b) => b[1] - a[1])
      .slice(0, 2)
      .map(([label, count]) => ({ kind: "generic" as const, label, count }));

    return [...cats, ...prods, ...generics, { kind: "all" as const, label: debounced }];
  }, [typing, debounced, results, categories, history.items]);

  function finish() {
    setOpen(false);
    setActive(-1);
    input.current?.blur();
    onDone?.();
  }

  function runSearch(term: string) {
    const t = term.trim();
    if (!t) {
      navigate("/products");
      finish();
      return;
    }
    history.add(t);
    track("search_performed", { query_len: t.length });
    setQ(t);
    navigate(`/products?q=${encodeURIComponent(t)}`);
    finish();
  }

  function choose(o: Option) {
    switch (o.kind) {
      case "generic":
        // A salt opens the side-by-side comparison (grouped by strength,
        // form and release), not just another result list.
        history.add(o.label);
        finish();
        navigate(`/compare?salt=${encodeURIComponent(o.label)}`);
        return;
      case "recent":
      case "suggest":
      case "all":
        runSearch(o.label);
        return;
      case "category":
        navigate(`/products?category=${o.slug}`);
        finish();
        return;
      case "product":
        history.add(q.trim() || o.product.name);
        navigate(`/products/${o.product.slug}`);
        setQ("");
        finish();
    }
  }

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setOpen(true);
      setActive((i) => Math.min(i + 1, options.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive((i) => Math.max(i - 1, -1));
    } else if (e.key === "Escape") {
      if (open) setOpen(false);
      else setQ("");
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (open && active >= 0 && options[active]) choose(options[active]);
      else runSearch(q);
    }
  }

  const hero = variant === "hero";
  const optId = (i: number) => `${listId}-opt-${i}`;
  const exact = typing && (results?.data ?? []).some((p) => p.name.toLowerCase().includes(debounced.toLowerCase()) || (p.genericName ?? "").toLowerCase().includes(debounced.toLowerCase()));
  const productCount = options.filter((o) => o.kind === "product").length;

  return (
    <div ref={root} className="relative w-full">
      <div
        className={`relative flex items-center bg-white transition-shadow ${
          hero
            ? "h-14 sm:h-16 rounded-2xl ring-1 ring-ink-200 shadow-search focus-within:ring-2 focus-within:ring-brand-500"
            : "h-11 rounded-xl ring-1 ring-ink-200 bg-ink-50 focus-within:bg-white focus-within:ring-2 focus-within:ring-brand-500"
        }`}
      >
        <Search size={hero ? 22 : 18} className={`pointer-events-none absolute text-ink-400 ${hero ? "left-5" : "left-3.5"}`} aria-hidden />
        <input
          ref={input}
          value={q}
          autoFocus={autoFocus}
          onChange={(e) => {
            setQ(e.target.value);
            setOpen(true);
            setActive(-1);
          }}
          onFocus={() => setOpen(true)}
          onKeyDown={onKeyDown}
          type="search"
          enterKeyHint="search"
          autoComplete="off"
          spellCheck={false}
          placeholder={hero ? "Search medicines, salts or health products" : "Search medicines & health products"}
          className={`h-full w-full min-w-0 bg-transparent outline-none placeholder:text-ink-400 [&::-webkit-search-cancel-button]:hidden ${
            hero ? "pl-14 pr-32 text-base sm:text-lg" : "pl-10 pr-10 text-[15px]"
          }`}
          role="combobox"
          aria-label="Search medicines and health products"
          aria-expanded={open && options.length > 0}
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={active >= 0 ? optId(active) : undefined}
        />
        <div className={`absolute flex items-center gap-1 ${hero ? "right-2" : "right-1.5"}`}>
          {isFetching && typing && <Loader2 size={16} className="animate-spin text-ink-400" aria-hidden />}
          {q && (
            <button
              type="button"
              onClick={() => {
                setQ("");
                input.current?.focus();
              }}
              aria-label="Clear search"
              className="inline-flex h-8 w-8 items-center justify-center rounded-lg text-ink-400 hover:bg-ink-100 hover:text-ink-700"
            >
              <X size={16} />
            </button>
          )}
          {hero && (
            <button type="button" onClick={() => runSearch(q)} className="btn btn-primary h-10 sm:h-12 px-4 sm:px-6 rounded-xl">
              Search
            </button>
          )}
          {!hero && !q && (
            <kbd className="hidden lg:inline-flex h-6 items-center rounded-md border border-ink-200 bg-white px-1.5 text-xs font-medium text-ink-400 mr-1.5">
              /
            </kbd>
          )}
        </div>
      </div>

      {open && options.length > 0 && (
        <div
          className={`absolute left-0 right-0 z-50 mt-2 max-h-[70vh] overflow-y-auto overscroll-contain rounded-2xl border border-ink-200 bg-white py-2 shadow-pop animate-pop-in text-left`}
        >
          <ul id={listId} role="listbox" aria-label="Search suggestions">
            {options.map((o, i) => {
              const prev = options[i - 1];
              const header = groupHeader(o, prev, history.items.length > 0, exact);
              return (
                <li key={optionKey(o, i)} role="presentation">
                  {header && (
                    <div className="flex items-center justify-between px-4 pb-1 pt-2.5 text-xs font-semibold uppercase tracking-wide text-ink-400" role="presentation">
                      <span>{header}</span>
                      {o.kind === "recent" && (
                        <button
                          type="button"
                          onMouseDown={(e) => e.preventDefault()}
                          onClick={() => history.clear()}
                          className="normal-case tracking-normal font-medium text-brand-700 hover:underline"
                        >
                          Clear
                        </button>
                      )}
                    </div>
                  )}
                  <div
                    id={optId(i)}
                    role="option"
                    aria-selected={i === active}
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => choose(o)}
                    onMouseEnter={() => setActive(i)}
                    className={`mx-2 flex cursor-pointer items-center gap-3 rounded-xl px-2.5 ${
                      o.kind === "product" ? "py-2" : "py-2.5"
                    } ${i === active ? "bg-brand-50" : ""}`}
                  >
                    <OptionBody o={o} query={debounced} onRemoveRecent={(label) => history.remove(label)} />
                  </div>
                </li>
              );
            })}
          </ul>
          {typing && productCount === 0 && !isFetching && (
            <p className="px-4 pb-2 pt-1 text-sm text-ink-500">
              No product names match "{debounced}". Try the salt name (e.g. "paracetamol") or a brand.
            </p>
          )}
        </div>
      )}
    </div>
  );
}

function groupHeader(o: Option, prev: Option | undefined, _hasHistory: boolean, exact: boolean): string | null {
  if (prev && prev.kind === o.kind) return null;
  switch (o.kind) {
    case "recent":
      return "Recent searches";
    case "suggest":
      return "Try searching for";
    case "category":
      return "Categories";
    case "product":
      return exact ? "Products" : "Closest matches";
    case "generic":
      return "Compare brands by salt";
    default:
      return null;
  }
}

function optionKey(o: Option, i: number) {
  return o.kind === "product" ? o.product.id : `${o.kind}-${"label" in o ? o.label : i}`;
}

function OptionBody({ o, query, onRemoveRecent }: { o: Option; query: string; onRemoveRecent: (l: string) => void }) {
  switch (o.kind) {
    case "recent":
      return (
        <>
          <Clock size={16} className="shrink-0 text-ink-400" aria-hidden />
          <span className="flex-1 truncate text-[15px] text-ink-800">{o.label}</span>
          <button
            type="button"
            aria-label={`Remove ${o.label} from recent searches`}
            onMouseDown={(e) => e.preventDefault()}
            onClick={(e) => {
              e.stopPropagation();
              onRemoveRecent(o.label);
            }}
            className="inline-flex h-7 w-7 items-center justify-center rounded-lg text-ink-400 hover:bg-white hover:text-ink-700"
          >
            <X size={14} />
          </button>
        </>
      );
    case "suggest":
      return (
        <>
          <TrendingUp size={16} className="shrink-0 text-ink-400" aria-hidden />
          <span className="flex-1 truncate text-[15px] text-ink-800">{o.label}</span>
          <ArrowUpLeft size={15} className="text-ink-300" aria-hidden />
        </>
      );
    case "category": {
      const look = categoryLook(o.slug);
      const Icon = look.icon;
      return (
        <>
          <span className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg ${look.tile} ${look.icon_fg}`}>
            <Icon size={17} aria-hidden />
          </span>
          <span className="flex-1 truncate text-[15px] font-medium text-ink-800">{o.label}</span>
          <LayoutGrid size={15} className="text-ink-300" aria-hidden />
        </>
      );
    }
    case "product": {
      const p = o.product;
      return (
        <>
          <span className="h-11 w-11 shrink-0 overflow-hidden rounded-lg bg-ink-50">
            <ProductImage src={p.imageUrl} alt="" pad="p-1" />
          </span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-[15px] font-medium text-ink-900">
              <Highlight text={titleCase(p.name)} query={query} />
            </span>
            <span className="block truncate text-xs text-ink-500">
              {[p.genericName, p.brandName ?? marketerShort(p.manufacturerInfo), p.packSize].filter(Boolean).join(" · ")}
            </span>
          </span>
          <span className="shrink-0 text-right">
            <span className="block text-sm font-semibold text-ink-900 tabular">{formatPaise(p.pricePaise)}</span>
            {p.requiresPrescription ? (
              <span className="block text-xs font-medium text-rx-700">Rx required</span>
            ) : p.discountPercent > 0 ? (
              <span className="block text-xs font-medium text-save-700">{p.discountPercent}% off</span>
            ) : null}
          </span>
        </>
      );
    }
    case "generic":
      return (
        <>
          <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-brand-50 text-brand-700">
            <FlaskConical size={17} aria-hidden />
          </span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-[15px] font-medium text-ink-800">{titleCase(o.label)}</span>
            <span className="block text-xs text-ink-500">See every brand with this salt and compare prices</span>
          </span>
        </>
      );
    case "all":
      return (
        <>
          <Search size={16} className="shrink-0 text-brand-700" aria-hidden />
          <span className="flex-1 truncate text-[15px] font-semibold text-brand-700">See all results for "{o.label}"</span>
        </>
      );
  }
}

function Highlight({ text, query }: { text: string; query: string }) {
  const idx = query ? text.toLowerCase().indexOf(query.toLowerCase()) : -1;
  if (idx < 0) return <>{text}</>;
  return (
    <>
      {text.slice(0, idx)}
      <mark className="bg-transparent font-bold text-ink-900">{text.slice(idx, idx + query.length)}</mark>
      {text.slice(idx + query.length)}
    </>
  );
}
