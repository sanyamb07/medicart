import { Link, useLocation, useNavigate } from "react-router-dom";
import { useMutation } from "@tanstack/react-query";
import { ClipboardList, LogOut, Package, Percent, ShieldCheck, Store } from "lucide-react";
import { api } from "../../lib/api";

const NAV = [
  { to: "/platform/businesses", label: "Businesses", icon: Store },
  { to: "/platform/products", label: "Master catalog", icon: Package },
  { to: "/platform/medicines", label: "Medicine info", icon: ClipboardList },
  { to: "/platform/affiliates", label: "Affiliates", icon: Percent },
];

export default function PlatformShell({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  const navigate = useNavigate();
  const logout = useMutation({
    mutationFn: () => api.post("/api/v1/staff/logout"),
    onSuccess: () => navigate("/platform"),
  });

  return (
    <div className="min-h-screen bg-slate-100 flex">
      <aside className="w-56 shrink-0 bg-slate-950 text-slate-300 flex flex-col">
        <div className="flex items-center gap-2 px-4 py-4 text-white border-b border-slate-800">
          <span className="h-8 w-8 rounded-lg bg-brand-600 flex items-center justify-center">
            <ShieldCheck size={16} />
          </span>
          <span className="font-bold text-sm">MediCart Platform</span>
        </div>
        <nav className="flex-1 px-2 py-4 space-y-1">
          {NAV.map(({ to, label, icon: Icon }) => {
            const active = location.pathname.startsWith(to);
            return (
              <Link
                key={to}
                to={to}
                className={`flex items-center gap-2.5 px-3 py-2 rounded-lg text-sm font-medium ${
                  active ? "bg-slate-800 text-white" : "hover:bg-slate-900 hover:text-white"
                }`}
              >
                <Icon size={16} /> {label}
              </Link>
            );
          })}
        </nav>
        <button
          onClick={() => logout.mutate()}
          className="flex items-center gap-2.5 px-5 py-4 text-sm font-medium border-t border-slate-800 hover:text-white"
        >
          <LogOut size={16} /> Sign out
        </button>
      </aside>
      <main className="flex-1 min-w-0">{children}</main>
    </div>
  );
}
