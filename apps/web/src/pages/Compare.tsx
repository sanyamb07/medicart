import { useEffect, useMemo } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, FileText, Info, Plus, Scale } from "lucide-react";
import { api, type Envelope } from "../lib/api";
import type { Product } from "../types";
import { titleCase } from "../lib/categories";
import { compositionKey, marketerShort } from "../lib/catalog";
import { formatPaise } from "../lib/money";
import { ProductImage, QtyStepper } from "../components/product/bits";
import { useAddToCart, useCart, useSetCartQty } from "../hooks/useCart";

// ---------------------------------------------------------------- grouping
//
// Products are compared only when they are the same medicine in the same
// way: identical composition (salt included — metoprolol tartrate and
// succinate are not interchangeable mg for mg), the same strength, the same
// dosage form and the same release type. Anything else is listed in its own
// group and never priced against the others.

// Release codes may be glued to the strength ("1000SR", "35MR"), so a code
// counts when preceded by a word boundary OR a digit.
const code = (c: string) => new RegExp(`(?:\\b|(?<=\\d))(?:${c})\\b`);
const RELEASE: [RegExp, string][] = [
  [code("SR|PR"), "Sustained release"],
  [code("ER|XR|XL"), "Extended release"],
  [code("CR"), "Controlled release"],
  [code("MR"), "Modified release"],
  [code("OD"), "Once daily"],
  [/\bRETARD\b/, "Retard"],
  [/\bDEPOT\b/, "Depot"],
  [code("DT"), "Dispersible"],
  [code("MD|ODS|ODT"), "Mouth dissolving"],
  [/\bEFFERVESCENT\b/, "Effervescent"],
  [code("SOFT|CHEWABLE|CT"), "Chewable / soft"],
];

function releaseOf(name: string): string {
  const n = name.toUpperCase();
  for (const [re, label] of RELEASE) if (re.test(n)) return label;
  return "Standard";
}

function strengthOf(p: Product): string | null {
  const s = (p.strength ?? "").toLowerCase().replace(/\s+/g, "");
  return s || null;
}

interface Group {
  key: string;
  strength: string | null;
  form: string | null;
  release: string;
  items: Product[];
}

function groupProducts(products: Product[]): Group[] {
  const map = new Map<string, Group>();
  for (const p of products) {
    const strength = strengthOf(p);
    const form = p.dosageForm ?? null;
    const release = releaseOf(p.name);
    const key = [strength ?? "?", form ?? "?", release].join("|");
    const g = map.get(key) ?? { key, strength, form, release, items: [] };
    g.items.push(p);
    map.set(key, g);
  }
  for (const g of map.values()) g.items.sort((a, b) => a.pricePaise - b.pricePaise);
  return [...map.values()];
}

function groupTitle(g: Group): string {
  const parts = [g.strength ? g.strength.toUpperCase().replace(/(\d)([A-Z])/g, "$1 $2").toLowerCase() : "Strength not listed"];
  if (g.form) parts.push(titleCase(g.form).toLowerCase());
  if (g.release !== "Standard") parts.push(g.release.toLowerCase());
  return parts.join(" · ");
}

// ---------------------------------------------------------------- page

export default function Compare() {
  const [params] = useSearchParams();
  const salt = (params.get("salt") ?? "").trim();
  const fromSlug = params.get("from") ?? "";
  const key = compositionKey(salt);

  useEffect(() => {
    document.title = salt ? `Compare ${titleCase(salt)} brands — MediCart` : "Compare medicines — MediCart";
  }, [salt]);

  const { data, isLoading, isError } = useQuery({
    queryKey: ["compare", key],
    queryFn: () => api.get<Envelope<Product[]>>(`/api/v1/products?q=${encodeURIComponent(salt)}&sort=price_asc&pageSize=100`),
    enabled: !!salt,
    staleTime: 60_000,
  });

  const matches = useMemo(() => (data?.data ?? []).filter((p) => compositionKey(p.genericName) === key), [data, key]);
  const anchor = matches.find((p) => p.slug === fromSlug) ?? null;
  const groups = useMemo(() => {
    const gs = groupProducts(matches);
    // The group of the medicine the shopper came from leads; then the
    // groups with most brands (where comparing is most useful).
    const anchorKey = anchor ? [strengthOf(anchor) ?? "?", anchor.dosageForm ?? "?", releaseOf(anchor.name)].join("|") : "";
    return gs.sort((a, b) => Number(b.key === anchorKey) - Number(a.key === anchorKey) || b.items.length - a.items.length);
  }, [matches, anchor]);

  return (
    <div className="page py-6 sm:py-10">
      <nav aria-label="Breadcrumb" className="mb-5 text-sm text-ink-500">
        {anchor ? (
          <Link to={`/products/${anchor.slug}`} className="inline-flex items-center gap-1.5 hover:text-ink-800">
            <ArrowLeft size={15} /> Back to {titleCase(anchor.name)}
          </Link>
        ) : (
          <Link to="/products" className="inline-flex items-center gap-1.5 hover:text-ink-800">
            <ArrowLeft size={15} /> Back to products
          </Link>
        )}
      </nav>

      <header className="flex flex-col gap-5 lg:flex-row lg:items-end lg:justify-between">
        <div className="max-w-2xl">
          <p className="eyebrow flex items-center gap-2">
            <Scale size={14} aria-hidden /> Compare brands
          </p>
          <h1 className="mt-2 text-2xl font-bold leading-tight text-ink-900 sm:text-[2rem]">
            {salt ? titleCase(salt) : "Choose a medicine to compare"}
          </h1>
          {salt && !isLoading && (
            <p className="mt-2 text-ink-600">
              {matches.length} {matches.length === 1 ? "product" : "products"} with this exact composition, in {groups.length}{" "}
              {groups.length === 1 ? "group" : "groups"} by strength, form and release type.
            </p>
          )}
        </div>
        <div className="flex max-w-md gap-3 rounded-2xl border border-rx-200 bg-rx-50 p-4 text-sm text-rx-800">
          <Info size={18} className="mt-0.5 shrink-0" aria-hidden />
          <p>
            Prices are compared only within a group. Brands in one group share composition, strength, form and release type, but your
            doctor or pharmacist should confirm any switch of a prescribed medicine.
          </p>
        </div>
      </header>

      {!salt && (
        <p className="mt-10 text-ink-600">
          Open any medicine and choose <strong>Compare all brands</strong>, or{" "}
          <Link to="/products" className="font-semibold text-brand-700 underline">
            browse products
          </Link>
          .
        </p>
      )}

      {isLoading && (
        <div className="mt-8 space-y-4" aria-hidden>
          {Array.from({ length: 2 }).map((_, i) => (
            <div key={i} className="skeleton h-56 rounded-2xl" />
          ))}
        </div>
      )}

      {isError && <p className="mt-8 text-red-700">We couldn't load the comparison. Please try again.</p>}

      {salt && !isLoading && !isError && matches.length === 0 && (
        <p className="mt-8 text-ink-600">No products with exactly this composition were found.</p>
      )}

      <div className="mt-8 space-y-8">
        {groups.map((g) => (
          <GroupTable key={g.key} group={g} anchorId={anchor?.id ?? null} />
        ))}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------- group table

function GroupTable({ group, anchorId }: { group: Group; anchorId: string | null }) {
  const comparable = !!group.strength && group.items.length > 1;
  const lowest = group.items[0]?.pricePaise ?? 0;
  const highest = group.items[group.items.length - 1]?.pricePaise ?? 0;
  return (
    <section aria-labelledby={`g-${group.key}`} className="overflow-hidden rounded-2xl border border-ink-200/80 bg-white">
      <div className="flex flex-wrap items-baseline justify-between gap-2 border-b border-ink-200/80 bg-ink-50/60 px-4 py-3 sm:px-5">
        <h2 id={`g-${group.key}`} className="font-sans text-[15px] font-semibold tracking-normal text-ink-900">
          {groupTitle(group)}
          {group.items.some((p) => p.id === anchorId) && <span className="badge badge-muted ml-2 align-middle">Includes the one you viewed</span>}
        </h2>
        <p className="text-sm text-ink-500">
          {group.items.length} {group.items.length === 1 ? "brand" : "brands"}
          {comparable && lowest !== highest && (
            <>
              {" "}
              · {formatPaise(lowest)} – {formatPaise(highest)}
            </>
          )}
        </p>
      </div>

      {/* Desktop/tablet: a real table so columns line up for comparison. */}
      <table className="hidden w-full table-fixed text-sm md:table">
        <colgroup>
          <col />
          <col className="w-[22%]" />
          <col className="w-[12%]" />
          <col className="w-[10%]" />
          <col className="w-[11%]" />
          <col className="w-[9.5rem]" />
        </colgroup>
        <caption className="sr-only">Brands of {groupTitle(group)}</caption>
        <thead className="text-left text-xs uppercase tracking-wide text-ink-500">
          <tr>
            <th scope="col" className="px-5 py-2.5 font-semibold">Product</th>
            <th scope="col" className="px-3 py-2.5 font-semibold">Marketed by</th>
            <th scope="col" className="px-3 py-2.5 font-semibold">Pack</th>
            <th scope="col" className="px-3 py-2.5 text-right font-semibold">Price</th>
            <th scope="col" className="px-3 py-2.5 font-semibold">Availability</th>
            <th scope="col" className="px-5 py-2.5"><span className="sr-only">Add to cart</span></th>
          </tr>
        </thead>
        <tbody className="divide-y divide-ink-100">
          {group.items.map((p, i) => (
            <tr key={p.id} className={p.id === anchorId ? "bg-brand-50/50" : "hover:bg-ink-50/60"}>
              <td className="px-5 py-3">
                <Link to={`/products/${p.slug}`} className="flex items-center gap-3">
                  <span className="h-14 w-14 shrink-0 overflow-hidden rounded-xl bg-white ring-1 ring-inset ring-ink-100">
                    <ProductImage src={p.imageUrl} alt="" pad="p-1" />
                  </span>
                  <span className="min-w-0">
                    <span className="flex flex-wrap items-center gap-x-2 gap-y-1 font-semibold text-ink-900 hover:text-brand-700">
                      {titleCase(p.name)}
                      {p.requiresPrescription && (
                        <span className="badge badge-rx" title="Prescription required">
                          <FileText size={11} aria-hidden /> Rx
                        </span>
                      )}
                    </span>
                    {comparable && i === 0 && lowest < highest && (
                      <span className="mt-0.5 block text-xs font-semibold text-save-700">Lowest price in this group</span>
                    )}
                  </span>
                </Link>
              </td>
              <td className="truncate px-3 py-3 text-ink-700">{p.brandName ?? marketerShort(p.manufacturerInfo) ?? <span className="text-ink-400">Not confirmed</span>}</td>
              <td className="px-3 py-3 text-ink-700">{p.packSize ?? <span className="text-ink-400">—</span>}</td>
              <td className="px-3 py-3 text-right tabular">
                <span className="block font-bold text-ink-900">{formatPaise(p.pricePaise)}</span>
                {p.mrpPaise > p.pricePaise && <span className="block text-xs text-ink-500 line-through">{formatPaise(p.mrpPaise)}</span>}
              </td>
              <td className="px-3 py-3">
                {p.inStock ? <span className="font-medium text-save-700">In stock</span> : <span className="font-medium text-red-700">Unavailable</span>}
              </td>
              <td className="px-5 py-3 text-right">
                <AddButton product={p} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {/* Phones: stacked rows with the same facts in the same order. */}
      <ul className="divide-y divide-ink-100 md:hidden">
        {group.items.map((p, i) => (
          <li key={p.id} className={`p-4 ${p.id === anchorId ? "bg-brand-50/50" : ""}`}>
            <div className="flex gap-3">
              <Link to={`/products/${p.slug}`} className="h-16 w-16 shrink-0 overflow-hidden rounded-xl bg-white ring-1 ring-inset ring-ink-100">
                <ProductImage src={p.imageUrl} alt="" pad="p-1" />
              </Link>
              <div className="min-w-0 flex-1">
                <Link to={`/products/${p.slug}`} className="block font-semibold leading-snug text-ink-900">
                  {titleCase(p.name)}
                </Link>
                <p className="mt-0.5 truncate text-xs text-ink-500">
                  {[p.brandName ?? marketerShort(p.manufacturerInfo), p.packSize].filter(Boolean).join(" · ") || " "}
                </p>
                {comparable && i === 0 && lowest < highest && (
                  <p className="mt-0.5 text-xs font-semibold text-save-700">Lowest price in this group</p>
                )}
                <div className="mt-2 flex items-center justify-between gap-3">
                  <span className="tabular">
                    <span className="font-bold text-ink-900">{formatPaise(p.pricePaise)}</span>
                    {p.mrpPaise > p.pricePaise && <span className="ml-1.5 text-xs text-ink-500 line-through">{formatPaise(p.mrpPaise)}</span>}
                  </span>
                  <AddButton product={p} />
                </div>
              </div>
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}

function AddButton({ product }: { product: Product }) {
  const { qtyOf } = useCart();
  const add = useAddToCart();
  const setQty = useSetCartQty();
  const inCart = qtyOf(product.id);
  const name = titleCase(product.name);
  if (inCart > 0) {
    return (
      <QtyStepper
        size="sm"
        qty={inCart}
        max={Math.min(product.maxQtyPerOrder, product.availableQty)}
        busy={setQty.isPending}
        label={name}
        onChange={(q) => setQty.mutate({ productId: product.id, qty: q })}
      />
    );
  }
  return (
    <button
      type="button"
      onClick={() => add.mutate({ productId: product.id, qty: 1, pricePaise: product.pricePaise, name })}
      disabled={!product.inStock || add.isPending}
      aria-label={`Add ${name} to cart`}
      className="btn btn-sm bg-brand-50 text-brand-800 hover:bg-brand-600 hover:text-white disabled:bg-ink-50 disabled:text-ink-400"
    >
      {product.inStock ? (
        <>
          <Plus size={15} strokeWidth={2.5} /> Add
        </>
      ) : (
        "Unavailable"
      )}
    </button>
  );
}

