import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  AlertTriangle,
  BadgeIndianRupee,
  Check,
  ChevronRight,
  ClipboardCheck,
  FileText,
  FileUp,
  FlaskConical,
  ExternalLink,
  Info,
  Lock,
  Pill,
  ShieldAlert,
  ShoppingCart,
  Truck,
} from "lucide-react";
import { api, ApiError, type Envelope } from "../lib/api";
import { formatPaise } from "../lib/money";
import type { MedicineInfo, Product } from "../types";
import { track } from "../lib/analytics";
import { recordView, useRecentlyViewed } from "../lib/prefs";
import { compositionKey, marketerShort } from "../lib/catalog";
import { titleCase } from "../lib/categories";
import { useAddToCart, useCart, useSetCartQty } from "../hooks/useCart";
import { useDeliveryEstimate } from "../hooks/useDelivery";
import ProductRail, { useProductsBySlug } from "../components/ProductRail";
import { Price, ProductImage, QtyStepper, WishlistButton } from "../components/product/bits";
import PinSelector from "../components/PinSelector";

const FORM_IMAGES = new Set([
  "capsule", "cream", "device", "drops", "gel", "inhaler", "injection", "lotion", "ointment", "powder", "sachet", "solution", "spray", "suspension", "syrup", "tablet",
]);

export default function ProductDetail() {
  const { slug = "" } = useParams();
  const [qty, setQty] = useState(1);
  const [imageIdx, setImageIdx] = useState(0);

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["product", slug],
    queryFn: () => api.get<Envelope<{ product: Product; related: Product[] }>>(`/api/v1/products/${slug}`),
  });
  const product = data?.data.product;
  // Structured medicine information (uses, precautions, side effects…) from
  // the enrichment pipeline. Optional: the page works without it.
  const { data: infoData } = useQuery({
    queryKey: ["product-info", slug],
    queryFn: () => api.get<Envelope<MedicineInfo>>(`/api/v1/products/${slug}/info`),
    staleTime: 5 * 60_000,
    retry: false,
  });
  const info = infoData?.data;

  useEffect(() => {
    setQty(1);
    setImageIdx(0);
  }, [slug]);

  useEffect(() => {
    if (!product) return;
    track("product_viewed", { productId: product.id, pricePaise: product.pricePaise });
    recordView(product.slug);
    const name = titleCase(product.name);
    document.title = `${name}${product.packSize ? ` (${product.packSize})` : ""} | MediCart`;
    document.querySelector('meta[name="description"]')?.setAttribute(
      "content",
      `${name}${product.genericName ? ` contains ${product.genericName}` : ""}. ${formatPaise(product.pricePaise)}${
        product.discountPercent > 0 ? ` (${product.discountPercent}% off MRP)` : ""
      }.${product.requiresPrescription ? " Prescription required." : ""}`,
    );
  }, [product]);

  if (isLoading) return <DetailSkeleton />;
  if (isError || !data || !product) {
    const notFound = error instanceof ApiError && error.status === 404;
    return (
      <div className="page max-w-xl py-20 text-center">
        <span className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-ink-100 text-ink-400">
          <Pill size={26} />
        </span>
        <h1 className="mt-4 text-2xl font-bold text-ink-900">{notFound ? "We couldn't find that product" : "Something went wrong"}</h1>
        <p className="mt-2 text-ink-600">
          {notFound ? "It may have been renamed or is no longer listed." : "Please check your connection and try again."}
        </p>
        <Link to="/products" className="btn btn-primary mt-6">
          Browse all products
        </Link>
      </div>
    );
  }

  const related = data.data.related;
  const name = titleCase(product.name);
  const form = product.dosageForm?.toLowerCase() ?? "";
  // Every image that is still a drawing (not a verified photo of this pack)
  // is labelled as one, so nobody mistakes it for the product they'll receive.
  const isIllustration = !product.imageUrl || product.imageUrl.startsWith("/img/") || (info ? info.imageStatus !== "verified" : false);
  const images = [
    { src: product.imageUrl ?? (FORM_IMAGES.has(form) ? `/img/forms/${form}.svg` : null), label: "Product" },
  ];
  const category = product.categories?.[0];

  return (
    <div className="pb-28 md:pb-0">
      <div className="page py-5 sm:py-8">
        <nav aria-label="Breadcrumb" className="mb-5 text-sm text-ink-500">
          <ol className="flex flex-wrap items-center gap-1.5">
            <li>
              <Link to="/" className="hover:text-ink-800">Home</Link>
            </li>
            <li aria-hidden><ChevronRight size={14} /></li>
            {category ? (
              <li>
                <Link to={`/products?category=${category.slug}`} className="hover:text-ink-800">{category.name}</Link>
              </li>
            ) : (
              <li>
                <Link to="/products" className="hover:text-ink-800">Products</Link>
              </li>
            )}
            <li aria-hidden><ChevronRight size={14} /></li>
            <li className="max-w-[16rem] truncate font-medium text-ink-800" aria-current="page">{name}</li>
          </ol>
        </nav>

        <div className="grid gap-8 lg:grid-cols-[1fr_1.1fr] lg:gap-12">
          {/* Gallery */}
          <div className="min-w-0 lg:sticky lg:top-36 lg:self-start">
            <div className="relative aspect-[16/10] overflow-hidden rounded-3xl border border-ink-200/80 bg-gradient-to-b from-white to-ink-50 sm:aspect-[4/3]">
              <ProductImage
                src={images[imageIdx]?.src ?? null}
                alt={isIllustration ? `Illustration of a ${form || "medicine"} pack` : name}
                pad={isIllustration ? "p-8 sm:p-14" : "p-4 sm:p-8"}
                eager
              />
              <WishlistButton slug={product.slug} name={name} className="absolute right-4 top-4 h-11 w-11" />
              {product.requiresPrescription && (
                <span className="badge badge-rx absolute left-4 top-4 px-2 py-1 text-sm">
                  <FileText size={14} /> Prescription required
                </span>
              )}
            </div>
            {isIllustration && (
              <p className="mt-2.5 flex items-center gap-1.5 text-xs text-ink-500">
                <Info size={13} aria-hidden /> Illustration of the {form || "product"} form. The pack you receive may look different.
              </p>
            )}
            {images.length > 1 && (
              <div className="mt-3 flex gap-3" role="tablist" aria-label="Product images">
                {images.map((img, i) => (
                  <button
                    key={img.label}
                    role="tab"
                    aria-selected={i === imageIdx}
                    aria-label={`Show ${img.label} image`}
                    onClick={() => setImageIdx(i)}
                    className={`h-14 w-14 sm:h-20 sm:w-20 overflow-hidden rounded-xl border-2 bg-white ${i === imageIdx ? "border-brand-600" : "border-ink-200 hover:border-ink-300"}`}
                  >
                    <ProductImage src={img.src} alt="" pad="p-2" />
                  </button>
                ))}
              </div>
            )}
          </div>

          {/* Buy box */}
          <div className="min-w-0">
            {product.brandName && (
              <Link to={product.brandSlug ? `/products?brand=${product.brandSlug}` : "/products"} className="text-sm font-semibold text-brand-700 hover:underline">
                {product.brandName}
              </Link>
            )}
            <h1 className="mt-1 text-2xl font-bold leading-tight text-ink-900 sm:text-[2rem]">{name}</h1>
            {!product.brandName && marketerShort(product.manufacturerInfo) && (
              <p className="mt-1 text-sm text-ink-500">
                By{" "}
                <a href="#manufacturer" className="font-medium text-ink-700 hover:text-brand-700 hover:underline">
                  {marketerShort(product.manufacturerInfo)}
                </a>
              </p>
            )}
            {product.genericName && (
              <p className="mt-2 text-ink-600">
                Contains{" "}
                <Link to={`/products?q=${encodeURIComponent(product.genericName)}`} className="font-semibold text-ink-900 underline decoration-ink-300 underline-offset-4 hover:decoration-brand-600">
                  {titleCase(product.genericName)}
                </Link>
              </p>
            )}

            <dl className="mt-4 flex flex-wrap gap-2 text-sm">
              {[
                ["Strength", product.strength ? titleCase(product.strength) : null],
                ["Form", product.dosageForm ? titleCase(product.dosageForm) : null],
                ["Pack", product.packSize],
                ["Manufacturer", product.manufacturer],
              ]
                .filter(([, v]) => v)
                .map(([k, v]) => (
                  <div key={k} className="rounded-lg bg-ink-100 px-2.5 py-1">
                    <dt className="inline text-ink-500">{k}: </dt>
                    <dd className="inline font-medium text-ink-800">{v}</dd>
                  </div>
                ))}
            </dl>

            <div className="mt-6 rounded-2xl border border-ink-200/80 bg-white p-5 sm:p-6">
              <Price pricePaise={product.pricePaise} mrpPaise={product.mrpPaise} discountPercent={product.discountPercent} size="lg" />
              <p className="mt-2 text-sm text-ink-500">
                {product.discountPercent > 0 ? (
                  <>
                    MRP {formatPaise(product.mrpPaise)} · <span className="font-semibold text-save-700">You save {formatPaise(product.mrpPaise - product.pricePaise)}</span> ·{" "}
                  </>
                ) : null}
                Inclusive of all taxes
              </p>

              {product.requiresPrescription && (
                <div className="mt-5 flex gap-3 rounded-xl bg-rx-50 p-4 ring-1 ring-inset ring-rx-200">
                  <FileText size={20} className="mt-0.5 shrink-0 text-rx-600" />
                  <div className="text-sm text-rx-800">
                    <p className="font-semibold">A valid prescription is required</p>
                    <p className="mt-1 leading-relaxed">
                      A pharmacist must verify your prescription before an order containing this medicine is confirmed. Payment is only
                      requested after that review.
                    </p>
                    <Link to="/upload-prescription" className="mt-2 inline-flex items-center gap-1 font-semibold underline underline-offset-2">
                      <FileUp size={14} /> Upload prescription
                    </Link>
                  </div>
                </div>
              )}

              <Availability product={product} />

              <BuyActions product={product} qty={qty} setQty={setQty} name={name} />

              <ul className="mt-5 grid grid-cols-3 gap-2 border-t border-ink-100 pt-4 text-center text-xs text-ink-600">
                <li className="flex flex-col items-center gap-1.5">
                  <ClipboardCheck size={18} className="text-brand-600" /> Pharmacist reviewed
                </li>
                <li className="flex flex-col items-center gap-1.5">
                  <BadgeIndianRupee size={18} className="text-brand-600" /> Price vs MRP shown
                </li>
                <li className="flex flex-col items-center gap-1.5">
                  <Lock size={18} className="text-brand-600" /> Secure checkout
                </li>
              </ul>
            </div>

            {product.isDemoData && (
              <p className="mt-3 text-xs text-ink-500">This is a demonstration product listing with sample data.</p>
            )}
          </div>
        </div>

        <ProductInfo product={product} info={info} />
      </div>

      <div className="page space-y-14">
        <Alternatives product={product} />
        <ProductRail id="similar" title="Similar products" subtitle="From the same category" products={related} />
        <RecentlyViewed exclude={product.slug} />
      </div>

      <MobileBuyBar product={product} qty={qty} name={name} />
    </div>
  );
}

// ---------------------------------------------------------------- buy box parts

function Availability({ product }: { product: Product }) {
  const { pin, area, eta } = useDeliveryEstimate();
  if (!product.inStock) {
    return (
      <p className="mt-5 flex items-center gap-2 text-sm font-semibold text-red-700">
        <AlertTriangle size={16} /> Currently unavailable
      </p>
    );
  }
  return (
    <div className="mt-5 space-y-2 text-sm">
      <p className="flex items-center gap-2 font-semibold text-save-700">
        <Check size={16} /> In stock
      </p>
      <PinSelector compact />
      {eta && area && (
        <p className="flex items-center gap-2 pl-0.5 text-ink-600">
          <Truck size={15} className="text-brand-600" />
          {area.deliveryFeePaise === 0 ? "Free delivery" : `${formatPaise(area.deliveryFeePaise)} delivery fee`}
          {area.codAvailable && " · Cash on delivery available"}
        </p>
      )}
      {pin && area && !area.isServiceable && <p className="pl-0.5 text-red-700">We don't deliver to {pin} yet.</p>}
    </div>
  );
}

function BuyActions({ product, qty, setQty, name }: { product: Product; qty: number; setQty: (n: number) => void; name: string }) {
  const add = useAddToCart();
  const { qtyOf } = useCart();
  const inCart = qtyOf(product.id);
  const max = Math.max(1, Math.min(product.maxQtyPerOrder, product.availableQty));
  return (
    <div className="mt-6">
      <div className="flex flex-wrap items-center gap-3">
        <QtyStepper qty={qty} max={max} onChange={setQty} label={name} removeAtOne={false} />
        <button
          onClick={() => add.mutate({ productId: product.id, qty, pricePaise: product.pricePaise, name })}
          disabled={!product.inStock || add.isPending}
          className="btn btn-primary btn-lg flex-1 min-w-[12rem]"
        >
          <ShoppingCart size={18} />
          {!product.inStock ? "Out of stock" : add.isPending ? "Adding…" : inCart > 0 ? "Add more to cart" : "Add to cart"}
        </button>
      </div>
      <p className="mt-2.5 text-xs text-ink-500">
        Up to {product.maxQtyPerOrder} per order
        {inCart > 0 && (
          <>
            {" · "}
            <Link to="/cart" className="font-semibold text-brand-700 hover:underline">
              {inCart} in your cart
            </Link>
          </>
        )}
      </p>
    </div>
  );
}

function MobileBuyBar({ product, qty, name }: { product: Product; qty: number; name: string }) {
  const add = useAddToCart();
  const setCartQty = useSetCartQty();
  const { qtyOf } = useCart();
  const inCart = qtyOf(product.id);
  return (
    <div className="fixed inset-x-0 bottom-0 z-40 border-t border-ink-200 bg-white/95 px-4 py-3 shadow-bar backdrop-blur pb-safe md:hidden">
      <div className="flex items-center gap-3">
        <div className="min-w-0 flex-1">
          <p className="text-lg font-bold leading-none text-ink-900 tabular">{formatPaise(product.pricePaise)}</p>
          {product.discountPercent > 0 && (
            <p className="mt-1 text-xs text-ink-500">
              <span className="line-through">{formatPaise(product.mrpPaise)}</span>{" "}
              <span className="font-semibold text-save-700">{product.discountPercent}% off</span>
            </p>
          )}
        </div>
        {inCart > 0 ? (
          <>
            <QtyStepper
              qty={inCart}
              max={Math.min(product.maxQtyPerOrder, product.availableQty)}
              onChange={(q) => setCartQty.mutate({ productId: product.id, qty: q })}
              busy={setCartQty.isPending}
              label={name}
            />
            <Link to="/cart" className="btn btn-primary">
              Cart
            </Link>
          </>
        ) : (
          <button
            onClick={() => add.mutate({ productId: product.id, qty, pricePaise: product.pricePaise, name })}
            disabled={!product.inStock || add.isPending}
            className="btn btn-primary flex-1 max-w-[12rem]"
          >
            {!product.inStock ? "Out of stock" : add.isPending ? "Adding…" : "Add to cart"}
          </button>
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------- information

// Label text arrives as plain paragraphs; " - " runs are list items and an
// "Ingredient: …" prefix (multi-ingredient products) is shown as a heading.
function Prose({ text }: { text: string }) {
  return (
    <div className="space-y-3">
      {text.split(/\n{2,}/).map((para, i) => {
        const m = para.match(/^([A-Z][A-Za-z0-9 ,'()-]{2,48}):\s+(.*)$/s);
        const label = m && !/\s(is|are|the|of)\s/i.test(m[1]) ? m[1] : null;
        const body = label ? m![2] : para;
        const parts = body.split(/\s+-\s+(?=\S)/);
        return (
          <div key={i}>
            {label && <p className="text-sm font-semibold text-ink-900">{label}</p>}
            {parts.length >= 3 ? (
              <>
                {parts[0] && <p>{parts[0]}</p>}
                <ul className="mt-1.5 list-disc space-y-1 pl-5">
                  {parts.slice(1).map((li, j) => (
                    <li key={j}>{li}</li>
                  ))}
                </ul>
              </>
            ) : (
              <p>{body}</p>
            )}
          </div>
        );
      })}
    </div>
  );
}

// Long label sections start clamped so the page stays scannable.
function Expandable({ text }: { text: string }) {
  const [open, setOpen] = useState(false);
  const long = text.length > 420;
  return (
    <div>
      <div className={long && !open ? "relative max-h-40 overflow-hidden" : ""}>
        <Prose text={text} />
        {long && !open && <div aria-hidden className="absolute inset-x-0 bottom-0 h-12 bg-gradient-to-t from-white" />}
      </div>
      {long && (
        <button type="button" onClick={() => setOpen((v) => !v)} aria-expanded={open} className="mt-2 text-sm font-semibold text-brand-700 hover:underline">
          {open ? "Show less" : "Read more"}
        </button>
      )}
    </div>
  );
}

function ProductInfo({ product, info }: { product: Product; info?: MedicineInfo }) {
  // The pipeline writes a generated description that starts with its short
  // description; a hand-written description is shown as-is instead.
  const generated = !!(info?.shortDescription && product.description?.startsWith(info.shortDescription));
  const overview = generated || !product.description ? info?.shortDescription ?? product.description : product.description;
  const usesText = info?.uses ?? product.keyUses;
  const shortList = usesText && usesText.length < 160 && !usesText.includes("\n") ? usesText.split(/[,;]\s*/).filter(Boolean) : null;

  type Section = { id: string; title: string; body: React.ReactNode; note?: string; tone?: "warn" };
  const sections = [
    overview && {
      id: "overview",
      title: "Overview",
      body: (
        <div className="space-y-2">
          <p>{overview}</p>
          {info?.therapeuticCategory && (
            <p className="text-sm text-ink-500">
              Pharmacological class: <span className="font-medium text-ink-700">{info.therapeuticCategory}</span>
            </p>
          )}
        </div>
      ),
    },
    info?.warnings && { id: "important", title: "Important warning", body: <Expandable text={info.warnings} />, tone: "warn" as const },
    usesText && {
      id: "uses",
      title: "Uses",
      body: shortList ? (
        <ul className="flex flex-wrap gap-2">
          {shortList.map((u) => (
            <li key={u} className="rounded-full bg-brand-50 px-3 py-1 text-sm font-medium text-brand-800">
              {u.charAt(0).toUpperCase() + u.slice(1)}
            </li>
          ))}
        </ul>
      ) : (
        <Expandable text={usesText} />
      ),
    },
    info?.mechanismOfAction && { id: "how", title: "How it works", body: <Expandable text={info.mechanismOfAction} /> },
    info?.precautions && { id: "precautions", title: "Precautions", body: <Expandable text={info.precautions} /> },
    info?.sideEffects && { id: "side-effects", title: "Side effects", body: <Expandable text={info.sideEffects} /> },
    info?.contraindications && { id: "contraindications", title: "Who should not use it", body: <Expandable text={info.contraindications} /> },
    ((product.ingredients && product.ingredients.length > 0) || product.genericName) && {
      id: "composition",
      title: "Composition",
      body:
        product.ingredients && product.ingredients.length > 0 ? (
          <ul className="space-y-1.5">
            {product.ingredients.map((i) => (
              <li key={i.name} className="flex items-center gap-2">
                <FlaskConical size={15} className="text-ink-400" />
                <span className="font-medium text-ink-800">{titleCase(i.name)}</span>
                {i.strength && <span className="text-ink-500">· {i.strength}</span>}
              </li>
            ))}
          </ul>
        ) : (
          <p className="flex items-center gap-2">
            <FlaskConical size={15} className="text-ink-400" /> {titleCase(product.genericName!)}
            {product.strength && <span className="text-ink-500">· {product.strength}</span>}
          </p>
        ),
    },
    product.storageInfo && { id: "storage", title: "Storage", body: <Expandable text={product.storageInfo} /> },
    {
      id: "manufacturer",
      title: "Manufacturer",
      body: <p>{[product.manufacturer, product.manufacturerInfo].filter(Boolean).join(" · ") || <span className="text-ink-500">Not yet confirmed for this listing.</span>}</p>,
    },
    {
      id: "safety",
      title: "Using medicines safely",
      body: (
        <ul className="list-disc space-y-1.5 pl-5">
          {product.requiresPrescription && <li>Use only as prescribed, at the dose and duration on your prescription.</li>}
          <li>Read the leaflet in the pack before use. Do not exceed the recommended dose.</li>
          <li>Tell your doctor about other medicines you take, any allergies, and if you are pregnant or breastfeeding.</li>
          <li>Keep out of the reach of children.</li>
        </ul>
      ),
    },
  ].filter(Boolean) as Section[];

  return (
    <section aria-label="Product information" className="mt-12 grid gap-8 lg:grid-cols-[13rem_1fr]">
      <nav aria-label="On this page" className="hidden lg:block">
        <ul className="sticky top-36 space-y-1 border-l border-ink-200">
          {sections.map((s) => (
            <li key={s.id}>
              <a href={`#${s.id}`} className="-ml-px block border-l-2 border-transparent py-1.5 pl-4 text-sm text-ink-600 hover:border-brand-600 hover:text-ink-900">
                {s.title}
              </a>
            </li>
          ))}
        </ul>
      </nav>
      <div>
        <div className="divide-y divide-ink-200 overflow-hidden rounded-2xl border border-ink-200/80 bg-white">
          {product.requiresPrescription && (
            <div className="flex gap-3 bg-rx-50/60 p-5 sm:p-6">
              <FileText size={20} className="shrink-0 text-rx-600" />
              <div>
                <h2 className="font-sans font-semibold text-ink-900">Prescription requirement</h2>
                <p className="mt-1 leading-relaxed text-ink-700">
                  This is a prescription-only medicine. Orders containing it are held until a pharmacist has verified a valid prescription that
                  names this medicine.{" "}
                  <Link to="/help/prescriptions" className="font-semibold text-brand-700 hover:underline">
                    What counts as valid?
                  </Link>
                </p>
              </div>
            </div>
          )}
          {sections.map((s) => (
            <div key={s.id} id={s.id} className={`scroll-mt-40 p-5 sm:p-6 ${s.tone === "warn" ? "bg-red-50/50" : ""}`}>
              <h2 className={`flex items-center gap-2 font-sans text-base font-semibold ${s.tone === "warn" ? "text-red-800" : "text-ink-900"}`}>
                {s.tone === "warn" && <ShieldAlert size={18} aria-hidden />}
                {s.title}
              </h2>
              <div className="mt-2 max-w-3xl leading-relaxed text-ink-700">{s.body}</div>
              {s.note && <p className="mt-3 text-xs text-ink-500">{s.note}</p>}
            </div>
          ))}
        </div>
        <SourceNote info={info} />
      </div>
    </section>
  );
}

function SourceNote({ info }: { info?: MedicineInfo }) {
  if (!info?.infoSource && !(info?.unmatchedIngredients?.length)) {
    return <p className="mt-3 text-xs text-ink-500">General guidance only. It is not a substitute for advice from your doctor or pharmacist.</p>;
  }
  return (
    <div className="mt-3 space-y-1.5 text-xs leading-relaxed text-ink-500">
      {info.infoSource && (
        <p>
          Medicine information is quoted from the {info.infoSource.replace(/\s*\(openFDA\)/, "")} for the active ingredient and describes the
          ingredient in general, not this brand specifically.{" "}
          {info.infoSourceUrl && (
            <a href={info.infoSourceUrl} target="_blank" rel="noreferrer" className="inline-flex items-center gap-0.5 font-medium text-brand-700 hover:underline">
              View source <ExternalLink size={11} aria-hidden />
            </a>
          )}{" "}
          {info.lastVerifiedAt
            ? `Reviewed by our team on ${new Date(info.lastVerifiedAt).toLocaleDateString("en-IN", { day: "numeric", month: "short", year: "numeric" })}.`
            : "Not yet reviewed by our pharmacist."}
        </p>
      )}
      {info.unmatchedIngredients.length > 0 && <p>We don't yet have verified information for: {info.unmatchedIngredients.join(", ")}.</p>}
      <p>It is not a substitute for advice from your doctor or pharmacist.</p>
    </div>
  );
}

// ---------------------------------------------------------------- alternatives

// Other brands with the same composition, cheapest first. Matching is on the
// catalogue's salt field; differences in strength or form are shown so the
// shopper (and their pharmacist) can judge whether a switch is appropriate.
function Alternatives({ product }: { product: Product }) {
  const salt = product.genericName;
  const { data } = useQuery({
    queryKey: ["alternatives", salt],
    queryFn: () => api.get<Envelope<Product[]>>(`/api/v1/products?q=${encodeURIComponent(salt!)}&sort=price_asc&pageSize=30`),
    enabled: !!salt,
    staleTime: 60_000,
  });
  // Only same salt AND same dosage form: an injection is not an alternative
  // to a tablet, whatever it costs.
  const alts = (data?.data ?? [])
    .filter(
      (p) =>
        p.id !== product.id &&
        compositionKey(p.genericName) === compositionKey(salt) &&
        (p.dosageForm ?? "") === (product.dosageForm ?? ""),
    )
    .slice(0, 6);
  if (!salt || alts.length === 0) return null;

  return (
    <section aria-labelledby="alts-title">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <h2 id="alts-title" className="section-title">
          Other brands with {titleCase(salt)}
        </h2>
        <Link
          to={`/compare?salt=${encodeURIComponent(salt)}&from=${encodeURIComponent(product.slug)}`}
          className="btn btn-sm btn-secondary"
        >
          Compare all brands
        </Link>
      </div>
      <p className="section-sub">
        Same salt and form, sorted by price. Price differences are shown only where the strength matches. Ask your doctor or pharmacist
        before switching.
      </p>
      <ul className="mt-5 divide-y divide-ink-200 overflow-hidden rounded-2xl border border-ink-200/80 bg-white">
        <li className="flex items-center gap-4 bg-brand-50/60 p-4">
          <AltRow p={product} current reference={product} />
        </li>
        {alts.map((p) => (
          <li key={p.id} className="p-4 hover:bg-ink-50">
            <Link to={`/products/${p.slug}`} className="flex items-center gap-4">
              <AltRow p={p} reference={product} />
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}

function AltRow({ p, current, reference }: { p: Product; current?: boolean; reference: Product }) {
  const norm = (s: string | null) => (s ?? "").toLowerCase().replace(/\s+/g, "");
  const comparable = !!p.strength && norm(p.strength) === norm(reference.strength);
  const diff = comparable && reference.pricePaise > 0 ? Math.round(((reference.pricePaise - p.pricePaise) / reference.pricePaise) * 100) : 0;
  return (
    <>
      <span className="h-14 w-14 shrink-0 overflow-hidden rounded-xl bg-ink-50">
        <ProductImage src={p.imageUrl} alt="" pad="p-1.5" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate font-semibold text-ink-900">
          {titleCase(p.name)} {current && <span className="badge badge-muted ml-1 align-middle">Viewing</span>}
        </span>
        <span className="block truncate text-sm text-ink-500">{[p.brandName, p.strength && titleCase(p.strength), p.dosageForm && titleCase(p.dosageForm), p.packSize].filter(Boolean).join(" · ")}</span>
      </span>
      <span className="shrink-0 text-right">
        <span className="block font-bold text-ink-900 tabular">{formatPaise(p.pricePaise)}</span>
        {!current && diff > 0 && <span className="block text-xs font-semibold text-save-700">{diff}% lower price</span>}
        {!current && diff < 0 && <span className="block text-xs text-ink-500">{Math.abs(diff)}% higher</span>}
        {!current && !comparable && <span className="block text-xs text-ink-500">Different strength</span>}
      </span>
    </>
  );
}

function RecentlyViewed({ exclude }: { exclude: string }) {
  const slugs = useRecentlyViewed().filter((s) => s !== exclude).slice(0, 10);
  const { products } = useProductsBySlug(slugs);
  return <ProductRail id="recent" title="Recently viewed" products={products} />;
}

function DetailSkeleton() {
  return (
    <div className="page py-8" aria-busy="true" aria-label="Loading product">
      <div className="skeleton mb-6 h-4 w-64" />
      <div className="grid gap-8 lg:grid-cols-[1fr_1.1fr] lg:gap-12">
        <div className="skeleton aspect-square rounded-3xl" />
        <div className="space-y-4">
          <div className="skeleton h-4 w-24" />
          <div className="skeleton h-9 w-3/4" />
          <div className="skeleton h-5 w-1/3" />
          <div className="skeleton mt-6 h-64 rounded-2xl" />
        </div>
      </div>
    </div>
  );
}
