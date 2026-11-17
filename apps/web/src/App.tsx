import { lazy, Suspense, useEffect } from "react";
import { Link, Route, Routes, useLocation } from "react-router-dom";
import { SearchX } from "lucide-react";
import { captureReferralFromURL } from "./lib/affiliateRef";
import Header from "./components/Header";
import Footer from "./components/Footer";
import DemoBanner from "./components/DemoBanner";
import AIChatWidget from "./components/AIChatWidget";
import MobileNav from "./components/MobileNav";
import Home from "./pages/Home";
import ProductListing from "./pages/ProductListing";
import ProductDetail from "./pages/ProductDetail";
import Compare from "./pages/Compare";
import Cart from "./pages/Cart";
const Checkout = lazy(() => import("./pages/Checkout"));
const OrderConfirmation = lazy(() => import("./pages/OrderConfirmation"));
const OrderTracking = lazy(() => import("./pages/OrderTracking"));
const Login = lazy(() => import("./pages/Login"));
const MockCheckout = lazy(() => import("./pages/MockCheckout"));
const Account = lazy(() => import("./pages/Account"));
const Businesses = lazy(() => import("./pages/Businesses"));
const Wishlist = lazy(() => import("./pages/Wishlist"));
const Categories = lazy(() => import("./pages/Categories"));
const UploadPrescription = lazy(() => import("./pages/UploadPrescription"));
const Help = lazy(() => import("./pages/Help"));
const AdminLogin = lazy(() => import("./pages/admin/AdminLogin"));
const AdminOrders = lazy(() => import("./pages/admin/AdminOrders"));
const AdminOrderDetail = lazy(() => import("./pages/admin/AdminOrderDetail"));
const AdminPrescriptions = lazy(() => import("./pages/admin/AdminPrescriptions"));
const AdminCatalog = lazy(() => import("./pages/admin/AdminCatalog"));
const AdminCatalogBatches = lazy(() => import("./pages/admin/AdminCatalogBatches"));
const AdminCustomers = lazy(() => import("./pages/admin/AdminCustomers"));
const AdminEnquiries = lazy(() => import("./pages/admin/AdminEnquiries"));
const PlatformLogin = lazy(() => import("./pages/platform/PlatformLogin"));
const PlatformBusinesses = lazy(() => import("./pages/platform/PlatformBusinesses"));
const PlatformProducts = lazy(() => import("./pages/platform/PlatformProducts"));
const PlatformProductForm = lazy(() => import("./pages/platform/PlatformProductForm"));
const PlatformAffiliates = lazy(() => import("./pages/platform/PlatformAffiliates"));
const PlatformMedicines = lazy(() => import("./pages/platform/PlatformMedicines"));
const AffiliateLogin = lazy(() => import("./pages/AffiliateLogin"));
const AffiliateApply = lazy(() => import("./pages/AffiliateApply"));
const AffiliateDashboard = lazy(() => import("./pages/AffiliateDashboard"));

export default function App() {
  const location = useLocation();
  const isAdmin = location.pathname.startsWith("/admin");
  const isPlatform = location.pathname.startsWith("/platform");
  const isAffiliate = location.pathname.startsWith("/affiliate");
  // Checkout and payment stay focused: no footer links or chat bubble to
  // pull people away mid-order.
  const isFocused = location.pathname === "/checkout" || location.pathname.startsWith("/pay/");
  // On phones the floating chat bubble covers product cards and sticky buy
  // bars, so there it only shows on pages without those (help, account,
  // tracking). Desktop always has it.
  const chatOnPhones = ["/account", "/track"].includes(location.pathname) || location.pathname.startsWith("/help");

  useEffect(() => {
    captureReferralFromURL();
  }, []);

  // New page → start at the top (but keep in-page #anchors working).
  useEffect(() => {
    if (!location.hash) window.scrollTo({ top: 0 });
  }, [location.pathname, location.hash]);

  if (isPlatform) {
    return (
      <Suspense fallback={<RouteFallback />}>
      <Routes>
        <Route path="/platform" element={<PlatformLogin />} />
        <Route path="/platform/businesses" element={<PlatformBusinesses />} />
        <Route path="/platform/affiliates" element={<PlatformAffiliates />} />
        <Route path="/platform/medicines" element={<PlatformMedicines />} />
        <Route path="/platform/products" element={<PlatformProducts />} />
        <Route path="/platform/products/new" element={<PlatformProductForm />} />
        <Route path="/platform/products/:id" element={<PlatformProductForm />} />
        <Route path="*" element={<NotFound />} />
      </Routes>
      </Suspense>
    );
  }

  if (isAffiliate) {
    return (
      <Suspense fallback={<RouteFallback />}>
      <Routes>
        <Route path="/affiliate" element={<AffiliateLogin />} />
        <Route path="/affiliate/apply" element={<AffiliateApply />} />
        <Route path="/affiliate/dashboard" element={<AffiliateDashboard />} />
        <Route path="*" element={<NotFound />} />
      </Routes>
      </Suspense>
    );
  }

  return (
    <div className="min-h-screen flex flex-col">
      {!isAdmin && <DemoBanner />}
      <Header />
      <main id="main" className="flex-1">
        <Suspense fallback={<RouteFallback />}>
      <Routes>
          <Route path="/" element={<Home />} />
          <Route path="/products" element={<ProductListing />} />
          <Route path="/products/:slug" element={<ProductDetail />} />
          <Route path="/compare" element={<Compare />} />
          <Route path="/cart" element={<Cart />} />
          <Route path="/checkout" element={<Checkout />} />
          <Route path="/orders/:reference/confirmation" element={<OrderConfirmation />} />
          <Route path="/track" element={<OrderTracking />} />
          <Route path="/login" element={<Login />} />
          <Route path="/account" element={<Account />} />
          <Route path="/businesses" element={<Businesses />} />
          <Route path="/wishlist" element={<Wishlist />} />
          <Route path="/categories" element={<Categories />} />
          <Route path="/upload-prescription" element={<UploadPrescription />} />
          <Route path="/help/:topic" element={<Help />} />
          <Route path="/pay/mock/:providerOrderId" element={<MockCheckout />} />

          <Route path="/admin" element={<AdminLogin />} />
          <Route path="/admin/orders" element={<AdminOrders />} />
          <Route path="/admin/orders/:id" element={<AdminOrderDetail />} />
          <Route path="/admin/prescriptions" element={<AdminPrescriptions />} />
          <Route path="/admin/catalog" element={<AdminCatalog />} />
          <Route path="/admin/catalog/:productId/batches" element={<AdminCatalogBatches />} />
          <Route path="/admin/customers" element={<AdminCustomers />} />
          <Route path="/admin/enquiries" element={<AdminEnquiries />} />

          <Route path="*" element={<NotFound />} />
        </Routes>
      </Suspense>
      </main>
      {!isFocused && <Footer />}
      {!isAdmin && !isFocused && (
        <div className={chatOnPhones ? undefined : "hidden md:block"}>
          <AIChatWidget />
        </div>
      )}
      {!isAdmin && <MobileNav />}
    </div>
  );
}

function NotFound() {
  return (
    <div className="max-w-xl mx-auto px-4 py-20 text-center">
      <span className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-ink-100 text-ink-400">
        <SearchX size={26} />
      </span>
      <h1 className="text-2xl font-bold text-ink-900">We couldn't find that page</h1>
      <p className="mt-2 text-ink-500">The link may be old, or the page may have moved.</p>
      <div className="mt-6 flex justify-center gap-3">
        <Link to="/" className="btn btn-primary">Go to home</Link>
        <Link to="/products" className="btn btn-secondary">Browse products</Link>
      </div>
    </div>
  );
}

// Staff, platform and affiliate screens are code-split so shoppers never
// download them; this is what briefly shows while one loads.
function RouteFallback() {
  return (
    <div className="page py-16" aria-busy="true">
      <div className="skeleton h-8 w-48" />
      <div className="skeleton mt-6 h-64 rounded-2xl" />
    </div>
  );
}
