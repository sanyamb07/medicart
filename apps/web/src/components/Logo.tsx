import { Link } from "react-router-dom";

export function LogoMark({ size = 36 }: { size?: number }) {
  // A rounded cross inside a soft square: reads as "health" at favicon size
  // without borrowing the red-cross emblem (which is legally protected).
  return (
    <svg width={size} height={size} viewBox="0 0 36 36" aria-hidden>
      <rect width="36" height="36" rx="10" fill="#127567" />
      <path
        d="M15 9.5h6a1 1 0 0 1 1 1V14h3.5a1 1 0 0 1 1 1v6a1 1 0 0 1-1 1H22v3.5a1 1 0 0 1-1 1h-6a1 1 0 0 1-1-1V22h-3.5a1 1 0 0 1-1-1v-6a1 1 0 0 1 1-1H14v-3.5a1 1 0 0 1 1-1Z"
        fill="#fff"
      />
      <circle cx="18" cy="18" r="2.25" fill="#40ad99" />
    </svg>
  );
}

export default function Logo({ tone = "dark" }: { tone?: "dark" | "light" }) {
  return (
    <Link to="/" className="flex shrink-0 items-center gap-2 rounded-lg" aria-label="MediCart home">
      <LogoMark />
      <span className={`font-display text-xl font-extrabold tracking-tight ${tone === "light" ? "text-white" : "text-ink-900"}`}>
        Medi<span className={tone === "light" ? "text-brand-300" : "text-brand-600"}>Cart</span>
      </span>
    </Link>
  );
}
