import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { Link, NavLink, useLocation } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { FileUp, Heart, Package, ShoppingCart, User } from "lucide-react";
import { api, type Envelope } from "../lib/api";
import type { Category } from "../types";
import { useAuth } from "../hooks/useAuth";
import { useCart } from "../hooks/useCart";
import { useWishlist } from "../lib/prefs";
import { categoryLook } from "../lib/categories";
import SearchBox from "./SearchBox";
import PinSelector from "./PinSelector";
import Logo from "./Logo";

export default function Header() {
  const { user } = useAuth();
  const { cart } = useCart();
  const wishlist = useWishlist();
  const location = useLocation();
  const itemCount = cart?.itemCount ?? 0;
  const [bump, setBump] = useState(false);
  const [prevCount, setPrevCount] = useState(itemCount);

  // Animate the cart badge when the count goes up — confirms an add from
  // anywhere on the page without a modal.
  if (itemCount !== prevCount) {
    if (itemCount > prevCount) setBump(true);
    setPrevCount(itemCount);
  }


  const { data: categories } = useQuery({
    queryKey: ["categories"],
    queryFn: () => api.get<Envelope<Category[]>>("/api/v1/categories"),
    staleTime: 5 * 60_000,
  });

  // Exactly one search box is mounted (desktop row or mobile row) so focus,
  // the "/" shortcut and screen readers never meet a hidden duplicate.
  const isDesktop = useMediaQuery("(min-width: 768px)");

  // Publish the header's live height so sticky sub-bars (e.g. listing
  // toolbar) sit right under it at every breakpoint and scroll state.
  const headerRef = useRef<HTMLElement>(null);
  useEffect(() => {
    const el = headerRef.current;
    if (!el) return;
    const ro = new ResizeObserver(() => document.documentElement.style.setProperty("--header-h", `${el.offsetHeight}px`));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const [scrolled, setScrolled] = useState(false);
  useEffect(() => {
    // Hysteresis: the mobile PIN row collapses at 80px and returns near the
    // top, so the height change can't bounce the threshold back and forth.
    const onScroll = () => setScrolled((was) => (was ? window.scrollY > 4 : window.scrollY > 80));
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  // Checkout is a focused flow: keep the brand and a way back, drop the
  // shopping chrome so nothing competes with finishing the order.
  const focused = location.pathname === "/checkout";

  // On the home page the hero carries the big search box. While it is on
  // screen the header's copy stays out of the way, so the page never shows
  // two search bars at once; it appears as soon as the hero scrolls away.
  const [heroSearchVisible, setHeroSearchVisible] = useState(false);
  useEffect(() => {
    const el = location.pathname === "/" ? document.getElementById("hero-search") : null;
    if (!el || typeof IntersectionObserver === "undefined") {
      setHeroSearchVisible(false);
      return;
    }
    const io = new IntersectionObserver(([e]) => setHeroSearchVisible(e.isIntersecting && e.boundingClientRect.width > 0), {
      rootMargin: "-72px 0px 0px 0px",
    });
    io.observe(el);
    return () => io.disconnect();
  }, [location.pathname]);
  const firstName = user?.fullName?.split(" ")[0];

  return (
    <header
      ref={headerRef}
      className={`sticky top-0 z-40 border-b bg-white/95 backdrop-blur supports-[backdrop-filter]:bg-white/85 transition-shadow ${
        scrolled ? "border-ink-200 shadow-[0_4px_16px_-8px_rgba(20,27,34,0.15)]" : "border-ink-200/70"
      }`}
    >
      <a href="#main" className="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-2 focus:z-50 btn btn-primary">
        Skip to content
      </a>
      <div className="page">
        <div className="flex h-16 items-center gap-2 sm:gap-4 lg:h-[4.5rem]">
          <Logo />

          {!focused && (
            <>
              <div className="hidden lg:block shrink-0">
                <PinSelector />
              </div>

              {isDesktop && (
                <div
                  className={`flex-1 max-w-2xl transition-[opacity,transform] duration-200 ${
                    heroSearchVisible ? "pointer-events-none -translate-y-1 opacity-0" : "opacity-100"
                  }`}
                  aria-hidden={heroSearchVisible || undefined}
                  inert={heroSearchVisible || undefined}
                >
                  <SearchBox />
                </div>
              )}

              <nav aria-label="Account and cart" className="ml-auto flex items-center gap-0.5 sm:gap-1">
                <Link
                  to="/upload-prescription"
                  className="hidden xl:inline-flex btn btn-sm btn-soft mr-1"
                >
                  <FileUp size={16} aria-hidden /> Upload Rx
                </Link>
                <Link to="/track" className="hidden lg:inline-flex icon-btn w-auto px-2.5 gap-1.5 text-sm font-medium" title="Track an order">
                  <Package size={19} aria-hidden /> <span className="hidden xl:inline">Orders</span>
                  <span className="sr-only xl:hidden">Track an order</span>
                </Link>
                <Link
                  to={user ? "/account" : "/login"}
                  className="hidden md:inline-flex icon-btn w-auto px-2.5 gap-1.5 text-sm font-medium"
                >
                  <User size={19} aria-hidden />
                  <span className="hidden lg:inline max-w-[7rem] truncate">{user ? firstName ?? "Account" : "Sign in"}</span>
                  <span className="sr-only lg:hidden">{user ? "Your account" : "Sign in"}</span>
                </Link>
                <Link to="/wishlist" className="icon-btn relative" aria-label={`Wishlist, ${wishlist.slugs.length} items`}>
                  <Heart size={20} aria-hidden />
                  {wishlist.slugs.length > 0 && (
                    <span className="absolute right-1 top-1 h-2 w-2 rounded-full bg-pink-500 ring-2 ring-white" aria-hidden />
                  )}
                </Link>
                <Link
                  to="/cart"
                  className="relative inline-flex h-10 items-center gap-2 rounded-xl px-2.5 text-ink-800 hover:bg-ink-100 sm:bg-ink-900 sm:px-3.5 sm:text-white sm:hover:bg-ink-800"
                  aria-label={`Cart, ${itemCount} items`}
                >
                  <ShoppingCart size={19} aria-hidden />
                  <span className="hidden sm:inline text-sm font-semibold">Cart</span>
                  {itemCount > 0 && (
                    <span
                      onAnimationEnd={() => setBump(false)}
                      className={`absolute -right-1 -top-1 sm:static inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-brand-500 px-1.5 text-xs font-bold text-white tabular ${
                        bump ? "animate-bump" : ""
                      }`}
                    >
                      {itemCount}
                    </span>
                  )}
                </Link>
              </nav>
            </>
          )}

          {focused && (
            <p className="ml-auto flex items-center gap-1.5 text-sm text-ink-500">
              <span className="hidden sm:inline">Secure checkout</span>
              <Link to="/cart" className="font-semibold text-brand-700 hover:underline">
                Back to cart
              </Link>
            </p>
          )}
        </div>

        {!focused && (
          <>
            {/* Mobile / tablet: search gets its own full-width row. */}
            {!isDesktop && (
              <div className="pb-3">
                <SearchBox />
                {/* Collapses once the page scrolls so the sticky header stays slim. */}
                {!scrolled && (
                  <div className="mt-2.5">
                    <PinSelector compact />
                  </div>
                )}
              </div>
            )}
            <div className="hidden md:flex lg:hidden pb-2.5">
              <PinSelector compact />
            </div>

            <nav aria-label="Shop by category" className="hidden md:block -mb-px">
              <ul className="no-scrollbar flex items-center gap-1 overflow-x-auto">
                <li>
                  <NavLink
                    to="/products"
                    end
                    className={({ isActive }) => navClass(isActive && !location.search)}
                  >
                    All products
                  </NavLink>
                </li>
                {(categories?.data ?? []).map((c) => {
                  const Icon = categoryLook(c.slug).icon;
                  const active = location.pathname === "/products" && new URLSearchParams(location.search).get("category") === c.slug;
                  return (
                    <li key={c.id}>
                      <Link to={`/products?category=${c.slug}`} className={navClass(active)}>
                        <Icon size={15} className="text-ink-400" aria-hidden />
                        {c.name}
                      </Link>
                    </li>
                  );
                })}
              </ul>
            </nav>
          </>
        )}
      </div>
    </header>
  );
}

function navClass(active: boolean) {
  return `flex items-center gap-1.5 whitespace-nowrap border-b-2 px-2.5 py-2.5 text-sm font-medium transition-colors ${
    active ? "border-brand-600 text-brand-800" : "border-transparent text-ink-600 hover:text-ink-900"
  }`;
}

function useMediaQuery(query: string) {
  return useSyncExternalStore(
    (cb) => {
      const mql = window.matchMedia(query);
      mql.addEventListener("change", cb);
      return () => mql.removeEventListener("change", cb);
    },
    () => window.matchMedia(query).matches,
    () => false,
  );
}
