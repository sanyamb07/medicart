import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api, type Envelope } from "../lib/api";
import type { Category } from "../types";
import Logo from "./Logo";

export default function Footer() {
  const { data: categories } = useQuery({
    queryKey: ["categories"],
    queryFn: () => api.get<Envelope<Category[]>>("/api/v1/categories"),
    staleTime: 5 * 60_000,
  });

  return (
    <footer className="mt-16 bg-ink-900 text-ink-300 pb-20 md:pb-0">
      <div className="page grid grid-cols-2 gap-8 py-12 text-sm sm:grid-cols-4 lg:grid-cols-5">
        <div className="col-span-2 sm:col-span-4 lg:col-span-2 max-w-sm">
          <Logo tone="light" />
          <p className="mt-4 leading-relaxed text-ink-400">
            Search medicines by brand or salt, see the price against MRP before you order, and have prescriptions
            checked by a pharmacist before anything ships.
          </p>
          <p className="mt-4 rounded-xl border border-ink-700 px-3 py-2 text-xs leading-relaxed text-ink-400">
            MediCart is a demonstration platform, not a licensed pharmacy. Products, prices and stock are sample data and
            no real orders are fulfilled.
          </p>
        </div>
        <FooterCol title="Shop">
          {(categories?.data ?? []).slice(0, 6).map((c) => (
            <FooterLink key={c.id} to={`/products?category=${c.slug}`}>
              {c.name}
            </FooterLink>
          ))}
          <FooterLink to="/products">All products</FooterLink>
        </FooterCol>
        <FooterCol title="Your orders">
          <FooterLink to="/account">Account &amp; orders</FooterLink>
          <FooterLink to="/track">Track an order</FooterLink>
          <FooterLink to="/upload-prescription">Upload prescription</FooterLink>
          <FooterLink to="/wishlist">Wishlist</FooterLink>
          <FooterLink to="/cart">Cart</FooterLink>
        </FooterCol>
        <FooterCol title="Help">
          <FooterLink to="/help/delivery">Delivery</FooterLink>
          <FooterLink to="/help/returns">Returns &amp; cancellations</FooterLink>
          <FooterLink to="/help/prescriptions">Prescriptions</FooterLink>
          <FooterLink to="/help/payments">Payments</FooterLink>
          <FooterLink to="/businesses">Pharmacy storefronts</FooterLink>
          <FooterLink to="/admin">Pharmacy staff login</FooterLink>
        </FooterCol>
      </div>
      <div className="border-t border-ink-800">
        <div className="page flex flex-col gap-2 py-5 text-xs text-ink-500 sm:flex-row sm:items-center sm:justify-between">
          <p>© {new Date().getFullYear()} MediCart (demo)</p>
          <p>Always follow your doctor's advice. Information on this site is not a substitute for medical advice.</p>
        </div>
      </div>
    </footer>
  );
}

function FooterCol({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <h2 className="mb-3 font-sans text-sm font-semibold text-white">{title}</h2>
      <ul className="space-y-2.5">{children}</ul>
    </div>
  );
}

function FooterLink({ to, children }: { to: string; children: React.ReactNode }) {
  return (
    <li>
      <Link to={to} className="text-ink-400 transition-colors hover:text-white">
        {children}
      </Link>
    </li>
  );
}
