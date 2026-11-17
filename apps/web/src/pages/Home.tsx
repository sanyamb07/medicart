import { useEffect } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowRight,
  BadgeIndianRupee,
  ChevronRight,
  ClipboardCheck,
  FileUp,
  FlaskConical,
  Lock,
  MessageCircle,
  PackageCheck,
  ShieldCheck,
  Thermometer,
  Truck,
} from "lucide-react";
import { api, type Envelope } from "../lib/api";
import type { Category, Product } from "../types";
import { rankEssentials } from "../lib/catalog";
import ProductRail, { useProductsBySlug } from "../components/ProductRail";
import SearchBox from "../components/SearchBox";
import { categoryLook, SEARCH_SUGGESTIONS, titleCase } from "../lib/categories";
import { clearRecentlyViewed, useRecentlyViewed, useSearchHistory, useWishlist } from "../lib/prefs";
import { useDeliveryEstimate } from "../hooks/useDelivery";
import { useAuth } from "../hooks/useAuth";
import { ProductImage } from "../components/product/bits";
import { formatPaise } from "../lib/money";

function useProducts(key: string, qs: string, enabled = true) {
  return useQuery({
    queryKey: ["products", "home", key],
    queryFn: () => api.get<Envelope<Product[]>>(`/api/v1/products?${qs}`),
    staleTime: 60_000,
    enabled,
  });
}

export default function Home() {
  useEffect(() => {
    document.title = "MediCart — Medicines & Health Products Delivered (Demo)";
  }, []);

  const { data: categories, isLoading: loadingCats } = useQuery({
    queryKey: ["categories"],
    queryFn: () => api.get<Envelope<Category[]>>("/api/v1/categories"),
    staleTime: 5 * 60_000,
  });

  // Shelves come back photo-first from the API (products with an approved
  // genuine pack photo sort ahead of illustrated ones), so the categories
  // with the most real packs lead the page.
  const essentials = useProducts("otc", "rxOnly=false&pageSize=40");
  const pain = useProducts("pain", "category=pain-relief&pageSize=12");
  const diabetes = useProducts("diabetes", "category=diabetes-care&pageSize=12");
  const skin = useProducts("skin", "category=skin-care&pageSize=12");

  const recentSlugs = useRecentlyViewed();
  const recent = useProductsBySlug(recentSlugs.slice(0, 10));
  const wishlist = useWishlist();
  const saved = useProductsBySlug(wishlist.slugs.slice(0, 10));

  // "Recommended for you" = what the catalogue relates to the last thing this
  // shopper looked at (same API the product page uses). No profile, no
  // tracking beyond this browser.
  const anchorSlug = recentSlugs[0];
  const { data: anchor } = useQuery({
    queryKey: ["product", anchorSlug],
    queryFn: () => api.get<Envelope<{ product: Product; related: Product[] }>>(`/api/v1/products/${anchorSlug}`),
    enabled: !!anchorSlug,
    staleTime: 60_000,
  });
  const recommended = (anchor?.data.related ?? []).filter((p) => !recentSlugs.includes(p.slug));

  const returning = recentSlugs.length > 0;

  // Monthly buyers: what this signed-in customer ordered before, with live
  // prices and stock (each product is loaded through the catalogue).
  const { user: me } = useAuth();
  const { data: buyAgainSlugs } = useQuery({
    queryKey: ["buy-again", me?.id],
    queryFn: () => api.get<Envelope<string[]>>("/api/v1/me/buy-again"),
    enabled: !!me,
    staleTime: 60_000,
  });
  const buyAgain = useProductsBySlug((buyAgainSlugs?.data ?? []).slice(0, 12));

  return (
    <div>
      <Hero />

      <div className="page space-y-14 py-10 sm:space-y-16 sm:py-14">
        {(buyAgainSlugs?.data.length ?? 0) > 0 && (
          <ProductRail
            id="buy-again"
            title="Buy again"
            subtitle="From your previous orders, at today's price"
            seeAllTo="/account"
            products={buyAgain.products}
            loading={buyAgain.loading && buyAgain.products.length === 0}
          />
        )}

        {returning && (
          <ProductRail
            id="recent"
            title="Continue where you left off"
            subtitle="Recently viewed on this device"
            products={recent.products}
            loading={recent.loading && recent.products.length === 0}
            action={
              <button onClick={clearRecentlyViewed} className="text-sm font-medium text-ink-500 hover:text-ink-800">
                Clear
              </button>
            }
          />
        )}

        <CategoryGrid categories={categories?.data} loading={loadingCats} />

        {recommended.length > 0 && anchor && (
          <ProductRail
            id="recommended"
            title="Recommended for you"
            subtitle={`Based on your interest in ${titleCase(anchor.data.product.name)}`}
            products={recommended}
          />
        )}

        <ProductRail
          id="pain"
          title="Pain & fever relief"
          subtitle="Compare brands by salt, strength and price"
          seeAllTo="/products?category=pain-relief"
          products={pain.data?.data}
          loading={pain.isLoading}
        />

        <ProductRail
          id="diabetes"
          title="Diabetes care"
          subtitle="Everyday treatments, shown with the manufacturer's own pack"
          seeAllTo="/products?category=diabetes-care"
          products={diabetes.data?.data}
          loading={diabetes.isLoading}
        />

        <PrescriptionBand />

        <ProductRail
          id="skin"
          title="Skin care"
          subtitle="Creams, gels and treatments"
          seeAllTo="/products?category=skin-care"
          products={skin.data?.data}
          loading={skin.isLoading}
        />

        <ProductRail
          id="essentials"
          title="Everyday essentials"
          subtitle="Health products you can order without a prescription"
          seeAllTo="/products?rx=otc"
          products={essentials.data ? rankEssentials(essentials.data.data).slice(0, 12) : undefined}
          loading={essentials.isLoading}
        />

        {wishlist.slugs.length > 0 && (
          <ProductRail
            id="wishlist"
            title="From your wishlist"
            subtitle="Live prices and stock for items you saved"
            seeAllTo="/wishlist"
            products={saved.products}
            loading={saved.loading && saved.products.length === 0}
          />
        )}

        <SeasonalCollection />

        <HealthGuides />

        <FAQ />
      </div>
    </div>
  );
}

// ---------------------------------------------------------------- hero

function Hero() {
  const navigate = useNavigate();
  const history = useSearchHistory();
  const { eta, area, pin } = useDeliveryEstimate();
  const quick = history.items.length > 0 ? history.items.slice(0, 4) : SEARCH_SUGGESTIONS.slice(0, 5);

  return (
    <section className="relative overflow-hidden border-b border-ink-200/70 bg-white">
      {/* Soft clinical backdrop: one quiet tint + a faint cross grid, no stock photo. */}
      <div aria-hidden className="pointer-events-none absolute inset-0">
        <div className="absolute -right-40 -top-40 h-[34rem] w-[34rem] rounded-full bg-brand-50" />
        <div className="absolute -bottom-48 left-1/3 h-[26rem] w-[26rem] rounded-full bg-[#f3f7ef]" />
        <svg className="absolute inset-0 h-full w-full opacity-[0.35]" xmlns="http://www.w3.org/2000/svg">
          <defs>
            <pattern id="crosses" width="44" height="44" patternUnits="userSpaceOnUse">
              <path d="M20 17h4v3h3v4h-3v3h-4v-3h-3v-4h3z" fill="#d4f1e9" />
            </pattern>
          </defs>
          <rect width="100%" height="100%" fill="url(#crosses)" />
        </svg>
      </div>

      <div className="page relative grid items-center gap-8 py-7 sm:py-10 lg:grid-cols-[1.35fr_1fr] lg:gap-14 lg:py-14">
        {/* min-w-0: the side-scrolling chip row must not widen the grid column. */}
        <div className="min-w-0">
          <p className="eyebrow flex items-center gap-2">
            <span className="inline-block h-1.5 w-1.5 rounded-full bg-brand-500" aria-hidden />
            Online pharmacy &amp; health store
          </p>
          <h1 className="mt-3 text-[1.9rem] font-extrabold leading-[1.08] text-ink-900 [text-wrap:balance] sm:text-[2.75rem] lg:text-[3.25rem]">
            Genuine medicines.
            <span className="block text-brand-700">Honest prices.</span>
          </h1>
          <p className="mt-4 hidden max-w-xl text-lg leading-relaxed text-ink-600 sm:block">
            Search by brand or salt, compare every brand with the same composition, and see MRP next to our price.
            Prescription items are checked by a pharmacist before they ship.
          </p>

          <div className="mt-5 max-w-2xl sm:mt-7">
            {/* Desktop: the hero owns the search (the header copy hides while
                this is visible). Phones and tablets use the header search. */}
            <div id="hero-search" className="hidden lg:block">
              <SearchBox variant="hero" />
            </div>
            <div className="no-scrollbar -mx-4 flex items-center gap-2 overflow-x-auto px-4 lg:mx-0 lg:mt-3 lg:flex-wrap lg:px-0">
              <span className="shrink-0 text-sm text-ink-500">{history.items.length > 0 ? "Search again:" : "Popular:"}</span>
              {quick.map((s) => (
                <button
                  key={s}
                  onClick={() => navigate(`/products?q=${encodeURIComponent(s)}`)}
                  className="chip h-8 shrink-0 px-3 text-[13px]"
                >
                  {s}
                </button>
              ))}
            </div>
          </div>

          {/* Phones: the prescription path as one tappable row, above the fold. */}
          <Link
            to="/upload-prescription"
            className="mt-5 flex items-center gap-3 rounded-2xl border border-brand-200 bg-brand-50 p-3.5 lg:hidden"
          >
            <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-brand-600 text-white">
              <FileUp size={19} />
            </span>
            <span className="min-w-0 flex-1">
              <span className="block text-sm font-semibold text-ink-900">Have a prescription?</span>
              <span className="block text-xs text-ink-600">Upload it once. A pharmacist reviews it before dispatch.</span>
            </span>
            <ChevronRight size={18} className="shrink-0 text-brand-700" />
          </Link>

          <ul className="mt-6 grid max-w-2xl grid-cols-2 gap-x-6 gap-y-2.5 text-[13px] text-ink-700 sm:mt-8 sm:grid-cols-4 sm:text-sm">
            <HeroPoint icon={ClipboardCheck}>Pharmacist-checked Rx</HeroPoint>
            <HeroPoint icon={BadgeIndianRupee}>Price vs MRP shown</HeroPoint>
            <HeroPoint icon={Truck}>{eta ? `${area?.city || pin}: ${eta}` : "Delivery time by PIN"}</HeroPoint>
            <HeroPoint icon={Lock}>Private &amp; secure</HeroPoint>
          </ul>
        </div>

        <div className="hidden lg:block">
          <PackShowcase />
        </div>
      </div>
    </section>
  );
}

function HeroPoint({ icon: Icon, children }: { icon: React.ComponentType<{ size?: number; className?: string }>; children: React.ReactNode }) {
  return (
    <li className="flex items-center gap-2">
      <Icon size={18} className="shrink-0 text-brand-600" />
      <span className="font-medium leading-tight">{children}</span>
    </li>
  );
}

// A window onto the real catalogue: live products whose pack photo was
// approved, so the first screen shows what the store actually sells. If the
// API has none yet, the card falls back to the prescription path alone.
function PackShowcase() {
  const { data, isLoading } = useProducts("showcase", "photo=1&category=diabetes-care&pageSize=4");
  const items = (data?.data ?? []).filter((p) => p.imageUrl && !p.imageUrl.startsWith("/img/")).slice(0, 4);
  return (
    <div className="rounded-3xl border border-ink-200/80 bg-white p-5 shadow-card">
      {(isLoading || items.length > 0) && (
        <>
          <div className="flex items-baseline justify-between gap-3 px-1">
            <p className="text-sm font-semibold text-ink-900">Real packs, from the manufacturer</p>
            <Link to="/products?category=diabetes-care" className="text-xs font-semibold text-brand-700 hover:text-brand-800">
              Browse
            </Link>
          </div>
          <ul className="mt-3 grid grid-cols-2 gap-3">
            {isLoading &&
              Array.from({ length: 4 }).map((_, i) => (
                <li key={i} className="skeleton aspect-[5/4] rounded-2xl" />
              ))}
            {items.map((p) => (
              <li key={p.id}>
                <Link
                  to={`/products/${p.slug}`}
                  className="group block rounded-2xl border border-ink-100 bg-white p-2 transition-[box-shadow,border-color] hover:border-ink-200 hover:shadow-cardHover"
                >
                  <span className="block aspect-[4/3]">
                    <ProductImage src={p.imageUrl} alt={titleCase(p.name)} pad="p-1" eager />
                  </span>
                  <span className="mt-1.5 flex items-baseline justify-between gap-2 px-1 pb-0.5">
                    <span className="truncate text-[13px] font-semibold text-ink-900">{titleCase(p.name)}</span>
                    <span className="shrink-0 text-[13px] font-bold text-ink-900 tabular">{formatPaise(p.pricePaise)}</span>
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </>
      )}
      <Link
        to="/upload-prescription"
        className={`flex items-center gap-3 rounded-2xl bg-brand-900 p-3.5 text-white transition-colors hover:bg-brand-800 ${
          isLoading || items.length > 0 ? "mt-4" : ""
        }`}
      >
        <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-white/10">
          <FileUp size={19} />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block text-sm font-semibold">Have a prescription?</span>
          <span className="block text-xs text-brand-100">Upload once. A pharmacist reviews it before dispatch.</span>
        </span>
        <ChevronRight size={18} className="shrink-0 text-brand-200" />
      </Link>
    </div>
  );
}

// ---------------------------------------------------------------- categories

function CategoryGrid({ categories, loading }: { categories: Category[] | undefined; loading: boolean }) {
  return (
    <section aria-labelledby="cats-title">
      <div className="mb-5 flex items-end justify-between gap-4">
        <div>
          <h2 id="cats-title" className="section-title">
            Shop by health need
          </h2>
          <p className="section-sub">Browse the full range in each category</p>
        </div>
        <Link to="/categories" className="text-sm font-semibold text-brand-700 hover:text-brand-800 whitespace-nowrap">
          All categories
        </Link>
      </div>
      <ul className="no-scrollbar -mx-4 flex snap-x gap-3 overflow-x-auto px-4 pb-1 sm:mx-0 sm:grid sm:grid-cols-4 sm:gap-4 sm:overflow-visible sm:px-0 xl:grid-cols-7">
        {loading &&
          Array.from({ length: 7 }).map((_, i) => (
            <li key={i} className="w-[8.5rem] shrink-0 sm:w-auto">
              <div className="skeleton h-[8.5rem] rounded-2xl" />
            </li>
          ))}
        {(categories ?? []).map((c) => {
          const look = categoryLook(c.slug);
          const Icon = look.icon;
          return (
            <li key={c.id} className="w-[8.5rem] shrink-0 snap-start sm:w-auto">
              <Link
                to={`/products?category=${c.slug}`}
                className={`group flex h-full flex-col rounded-2xl p-4 ring-1 ring-inset ring-black/[0.04] transition-[box-shadow,transform] duration-200 hover:-translate-y-0.5 hover:shadow-cardHover ${look.tile}`}
              >
                <span className={`flex h-11 w-11 items-center justify-center rounded-xl bg-white/80 shadow-sm ${look.icon_fg} transition-transform group-hover:scale-105`}>
                  <Icon size={22} strokeWidth={1.75} />
                </span>
                <span className="mt-4 text-[15px] font-semibold leading-snug text-ink-900">{c.name}</span>
                <span className="mt-1 hidden text-xs leading-snug text-ink-600 sm:block">{look.blurb}</span>
                <span className="mt-auto flex items-center gap-1 pt-3 text-xs font-medium text-ink-600 group-hover:text-ink-900">
                  {c.productCount.toLocaleString("en-IN")} items <ArrowRight size={13} />
                </span>
              </Link>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

// ---------------------------------------------------------------- prescription band

function PrescriptionBand() {
  return (
    <section className="overflow-hidden rounded-4xl bg-brand-900 text-white">
      <div className="grid gap-8 p-7 sm:p-10 lg:grid-cols-[1.1fr_1fr] lg:items-center lg:gap-12">
        <div>
          <p className="text-xs font-semibold uppercase tracking-[0.08em] text-brand-300">Prescription medicines</p>
          <h2 className="mt-3 text-2xl font-bold leading-tight sm:text-3xl">
            Every prescription order is reviewed by a pharmacist before dispatch
          </h2>
          <p className="mt-3 max-w-lg leading-relaxed text-brand-100">
            Items marked <strong className="text-white">Rx</strong> need a valid prescription. Upload it once and a pharmacist
            verifies it before an order containing those items is confirmed. Payment is only requested after the review is
            complete.
          </p>
          <div className="mt-6 flex flex-wrap gap-3">
            <Link to="/upload-prescription" className="btn btn-lg bg-white text-brand-800 hover:bg-brand-50">
              <FileUp size={18} /> Upload prescription
            </Link>
            <Link to="/products?rx=rx" className="btn btn-lg border border-white/30 text-white hover:bg-white/10">
              Browse Rx medicines
            </Link>
          </div>
        </div>
        <ul className="grid gap-3 sm:grid-cols-2">
          {[
            [ClipboardCheck, "Checked, not just stored", "Doctor details and validity are reviewed and logged on your order."],
            [Lock, "Private by default", "Only you and our pharmacist reviewers can open your uploads."],
            [FlaskConical, "Compare by salt", "See every brand with the same composition before you choose."],
            [MessageCircle, "Questions? Ask us", "Order updates and support in one WhatsApp thread."],
          ].map(([Icon, title, body]) => {
            const I = Icon as typeof Lock;
            return (
              <li key={title as string} className="rounded-2xl bg-white/[0.07] p-4 ring-1 ring-inset ring-white/10">
                <I size={20} className="text-brand-300" />
                <p className="mt-3 font-semibold">{title as string}</p>
                <p className="mt-1 text-sm leading-relaxed text-brand-100">{body as string}</p>
              </li>
            );
          })}
        </ul>
      </div>
    </section>
  );
}

// ---------------------------------------------------------------- seasonal

function SeasonalCollection() {
  // Curated entry points into real catalogue searches/categories for the
  // current season (winter in most of India from November).
  const items = [
    { title: "Cough, cold & flu", body: "Relief for seasonal sniffles", to: "/products?category=cold-immunity", icon: Thermometer, tone: "bg-[#ebf4fd] text-[#1d5fa8]" },
    { title: "Immunity boosters", body: "Vitamin C, zinc & multivitamins", to: "/products?q=vitamin%20c", icon: ShieldCheck, tone: "bg-[#fdf6e3] text-[#9a6700]" },
    { title: "Dry winter skin", body: "Moisturisers & skin repair", to: "/products?category=skin-care", icon: PackageCheck, tone: "bg-[#fbeff4] text-[#a23767]" },
  ];
  return (
    <section aria-labelledby="season-title">
      <h2 id="season-title" className="section-title">
        Winter wellness
      </h2>
      <p className="section-sub">Seasonal picks for the colder months</p>
      <div className="mt-5 grid gap-3 sm:grid-cols-3 sm:gap-4">
        {items.map(({ title, body, to, icon: Icon, tone }) => (
          <Link
            key={title}
            to={to}
            className="group flex items-center gap-4 rounded-2xl border border-ink-200/80 bg-white p-5 transition-shadow hover:shadow-cardHover"
          >
            <span className={`flex h-14 w-14 shrink-0 items-center justify-center rounded-2xl ${tone}`}>
              <Icon size={26} strokeWidth={1.75} />
            </span>
            <span className="min-w-0 flex-1">
              <span className="block font-semibold text-ink-900">{title}</span>
              <span className="block text-sm text-ink-500">{body}</span>
            </span>
            <ArrowRight size={18} className="shrink-0 text-ink-300 transition-transform group-hover:translate-x-0.5 group-hover:text-brand-600" />
          </Link>
        ))}
      </div>
    </section>
  );
}

// ---------------------------------------------------------------- guides

function HealthGuides() {
  const guides = [
    { to: "/help/generics", tag: "Save smarter", title: "Branded vs generic: what's the difference?", body: "Why medicines with the same salt can cost very different amounts." },
    { to: "/help/storage", tag: "Medicine safety", title: "How to store medicines at home", body: "Heat, humidity and light: simple rules that keep medicines effective." },
    { to: "/help/prescriptions", tag: "Ordering", title: "What makes a prescription valid?", body: "The details a pharmacist checks before dispensing Rx medicines." },
  ];
  return (
    <section aria-labelledby="guides-title">
      <div className="mb-5 flex items-end justify-between gap-4">
        <div>
          <h2 id="guides-title" className="section-title">
            Health guides
          </h2>
          <p className="section-sub">Short, practical reads. Always follow your doctor's advice.</p>
        </div>
      </div>
      <div className="grid gap-4 md:grid-cols-3">
        {guides.map((g) => (
          <Link key={g.to} to={g.to} className="group flex flex-col rounded-2xl border border-ink-200/80 bg-white p-6 transition-shadow hover:shadow-cardHover">
            <span className="text-xs font-semibold uppercase tracking-wide text-brand-700">{g.tag}</span>
            <span className="mt-2 font-display text-lg font-bold leading-snug text-ink-900">{g.title}</span>
            <span className="mt-2 text-sm leading-relaxed text-ink-600">{g.body}</span>
            <span className="mt-4 flex items-center gap-1 text-sm font-semibold text-brand-700">
              Read guide <ArrowRight size={15} className="transition-transform group-hover:translate-x-0.5" />
            </span>
          </Link>
        ))}
      </div>
    </section>
  );
}

// ---------------------------------------------------------------- FAQ

function FAQ() {
  const faqs = [
    ["Do I need a prescription for every medicine?", "Only for items marked Rx. Those are held for pharmacist review before dispatch; everyday health products ship without one."],
    ["How long does delivery take?", "It depends on your PIN code. Set your location at the top of the page to see the delivery window, fee and cash-on-delivery availability for your area."],
    ["How do I compare brands of the same medicine?", "Search by the salt name (for example \"atorvastatin\"), or open any product and see the brands that share its composition, with prices side by side."],
    ["Can I track my order?", "Yes. Use the order reference from your confirmation or WhatsApp message on the Track an order page, or see all orders in your account."],
    ["Is my prescription data private?", "Uploaded prescriptions are only visible to you and our pharmacist reviewers. Access is checked on every request."],
  ];
  return (
    <section aria-labelledby="faq-title" className="grid gap-8 lg:grid-cols-[1fr_2fr]">
      <div>
        <h2 id="faq-title" className="section-title">
          Questions, answered
        </h2>
        <p className="section-sub">More in our help pages on delivery, returns and payments.</p>
        <Link to="/help/delivery" className="btn btn-secondary mt-5">
          Visit help centre
        </Link>
      </div>
      <div className="divide-y divide-ink-200 rounded-2xl border border-ink-200/80 bg-white">
        {faqs.map(([q, a]) => (
          <details key={q} className="group px-5 py-1 sm:px-6">
            <summary className="flex cursor-pointer list-none items-center justify-between gap-4 py-4 font-medium text-ink-900 [&::-webkit-details-marker]:hidden">
              {q}
              <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-ink-100 text-ink-500 transition-transform group-open:rotate-45" aria-hidden>
                +
              </span>
            </summary>
            <p className="-mt-1 pb-4 leading-relaxed text-ink-600">{a}</p>
          </details>
        ))}
      </div>
    </section>
  );
}
