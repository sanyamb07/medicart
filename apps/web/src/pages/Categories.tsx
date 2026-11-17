import { useEffect } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, FileUp, Leaf, Pill } from "lucide-react";
import { api, type Envelope } from "../lib/api";
import type { Category } from "../types";
import { categoryLook } from "../lib/categories";

// Full category directory. On phones this is the "Categories" tab of the
// bottom navigation, so it is laid out as a tappable list.
export default function Categories() {
  useEffect(() => {
    document.title = "All categories | MediCart";
  }, []);
  const { data, isLoading } = useQuery({
    queryKey: ["categories"],
    queryFn: () => api.get<Envelope<Category[]>>("/api/v1/categories"),
    staleTime: 5 * 60_000,
  });

  return (
    <div className="page max-w-4xl py-8">
      <h1 className="text-2xl font-bold text-ink-900 sm:text-3xl">Shop by category</h1>
      <p className="mt-1 text-ink-500">Everything in the catalogue, organised by health need.</p>

      <div className="mt-6 grid gap-3 sm:grid-cols-2">
        <QuickLink to="/products?rx=otc" icon={Leaf} title="Everyday essentials" body="No prescription needed" />
        <QuickLink to="/products?rx=rx" icon={Pill} title="Prescription medicines" body="Pharmacist-verified before dispatch" />
      </div>

      <ul className="mt-6 divide-y divide-ink-200 overflow-hidden rounded-2xl border border-ink-200/80 bg-white">
        {isLoading &&
          Array.from({ length: 6 }).map((_, i) => (
            <li key={i} className="flex items-center gap-4 p-4">
              <div className="skeleton h-12 w-12 rounded-xl" />
              <div className="flex-1 space-y-2">
                <div className="skeleton h-4 w-1/3" />
                <div className="skeleton h-3 w-1/2" />
              </div>
            </li>
          ))}
        {(data?.data ?? []).map((c) => {
          const look = categoryLook(c.slug);
          const Icon = look.icon;
          return (
            <li key={c.id}>
              <Link to={`/products?category=${c.slug}`} className="flex items-center gap-4 p-4 hover:bg-ink-50">
                <span className={`flex h-12 w-12 shrink-0 items-center justify-center rounded-xl ${look.tile} ${look.icon_fg}`}>
                  <Icon size={22} strokeWidth={1.75} />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block font-semibold text-ink-900">{c.name}</span>
                  <span className="block text-sm text-ink-500">
                    {look.blurb} · {c.productCount.toLocaleString("en-IN")} products
                  </span>
                </span>
                <ChevronRight size={18} className="text-ink-300" />
              </Link>
            </li>
          );
        })}
      </ul>

      <Link to="/upload-prescription" className="mt-6 flex items-center gap-4 rounded-2xl bg-brand-900 p-5 text-white">
        <span className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-white/10">
          <FileUp size={22} />
        </span>
        <span className="flex-1">
          <span className="block font-semibold">Have a prescription?</span>
          <span className="block text-sm text-brand-100">Upload it once and reuse it for future orders.</span>
        </span>
        <ChevronRight size={18} />
      </Link>
    </div>
  );
}

function QuickLink({ to, icon: Icon, title, body }: { to: string; icon: typeof Pill; title: string; body: string }) {
  return (
    <Link to={to} className="flex items-center gap-4 rounded-2xl border border-ink-200/80 bg-white p-4 hover:shadow-cardHover transition-shadow">
      <span className="flex h-11 w-11 items-center justify-center rounded-xl bg-brand-50 text-brand-700">
        <Icon size={20} />
      </span>
      <span className="flex-1">
        <span className="block font-semibold text-ink-900">{title}</span>
        <span className="block text-sm text-ink-500">{body}</span>
      </span>
      <ChevronRight size={18} className="text-ink-300" />
    </Link>
  );
}
