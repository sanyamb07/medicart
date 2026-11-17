import { NavLink, useLocation } from "react-router-dom";
import { FileUp, Home, LayoutGrid, ShoppingCart, User } from "lucide-react";
import { useCart } from "../hooks/useCart";
import { useAuth } from "../hooks/useAuth";

// Thumb-reach navigation for phones. Hidden on pages that render their own
// sticky action bar (product detail, cart, checkout) so two bars never stack.
const PAGES_WITH_ACTION_BAR = [/^\/products\/[^/]+$/, /^\/cart$/, /^\/checkout$/];

export default function MobileNav() {
  const { cart } = useCart();
  const { user } = useAuth();
  const { pathname } = useLocation();
  if (PAGES_WITH_ACTION_BAR.some((r) => r.test(pathname))) return null;
  const count = cart?.itemCount ?? 0;

  const items = [
    { to: "/", label: "Home", icon: Home, end: true },
    { to: "/categories", label: "Categories", icon: LayoutGrid },
    { to: "/upload-prescription", label: "Upload Rx", icon: FileUp },
    { to: user ? "/account" : "/login", label: user ? "Account" : "Sign in", icon: User },
    { to: "/cart", label: "Cart", icon: ShoppingCart, badge: count },
  ];

  return (
    <nav
      aria-label="Primary"
      className="fixed inset-x-0 bottom-0 z-40 border-t border-ink-200 bg-white/95 backdrop-blur shadow-bar pb-safe md:hidden"
    >
      <ul className="grid grid-cols-5">
        {items.map(({ to, label, icon: Icon, end, badge }) => (
          <li key={label}>
            <NavLink
              to={to}
              end={end}
              className={({ isActive }) =>
                `relative flex h-16 flex-col items-center justify-center gap-1 text-xs font-medium ${
                  isActive ? "text-brand-700" : "text-ink-500"
                }`
              }
            >
              {({ isActive }) => (
                <>
                  {isActive && <span className="absolute top-0 h-0.5 w-8 rounded-full bg-brand-600" aria-hidden />}
                  <span className="relative">
                    <Icon size={22} strokeWidth={isActive ? 2.25 : 1.75} aria-hidden />
                    {!!badge && (
                      <span className="absolute -right-2.5 -top-1.5 inline-flex h-[18px] min-w-[18px] items-center justify-center rounded-full bg-brand-600 px-1 text-[11px] font-bold text-white tabular">
                        {badge}
                      </span>
                    )}
                  </span>
                  {label}
                </>
              )}
            </NavLink>
          </li>
        ))}
      </ul>
    </nav>
  );
}
