// Visual identity for storefront categories. The API owns which categories
// exist (GET /api/v1/categories); this file only decides how each one looks.
// Unknown slugs fall back to a neutral identity so a newly added category
// still renders correctly.
import {
  Activity,
  Baby,
  Bandage,
  Droplet,
  Dumbbell,
  Flower2,
  HeartPulse,
  type LucideIcon,
  Pill,
  ShieldPlus,
  Sparkles,
  Stethoscope,
  Thermometer,
  Zap,
} from "lucide-react";

export interface CategoryLook {
  icon: LucideIcon;
  /** Tinted tile background + icon colour. Each pair passes AA for the icon. */
  tile: string;
  icon_fg: string;
  blurb: string;
}

const LOOKS: Record<string, CategoryLook> = {
  "pain-relief": { icon: Zap, tile: "bg-[#fdf0ec]", icon_fg: "text-[#b4441f]", blurb: "Headache, body ache, fever" },
  "vitamins-supplements": { icon: Sparkles, tile: "bg-[#fdf6e3]", icon_fg: "text-[#9a6700]", blurb: "Daily nutrition & immunity" },
  "cold-immunity": { icon: Thermometer, tile: "bg-[#ebf4fd]", icon_fg: "text-[#1d5fa8]", blurb: "Cough, cold & flu care" },
  "digestive-care": { icon: Activity, tile: "bg-[#eef7ee]", icon_fg: "text-[#2f7a34]", blurb: "Acidity, gut & digestion" },
  "skin-care": { icon: Flower2, tile: "bg-[#fbeff4]", icon_fg: "text-[#a23767]", blurb: "Creams, lotions & treatments" },
  "diabetes-care": { icon: Droplet, tile: "bg-[#f1effc]", icon_fg: "text-[#5b3fc4]", blurb: "Monitoring & daily management" },
  antibiotics: { icon: ShieldPlus, tile: "bg-[#e9f6f4]", icon_fg: "text-[#0f6a5e]", blurb: "Prescription anti-infectives" },
  "baby-care": { icon: Baby, tile: "bg-[#fdf3ea]", icon_fg: "text-[#a45a12]", blurb: "Gentle care for little ones" },
  "fitness-nutrition": { icon: Dumbbell, tile: "bg-[#edf5ff]", icon_fg: "text-[#1f5fbf]", blurb: "Protein & performance" },
  "healthcare-devices": { icon: Stethoscope, tile: "bg-[#eef3f6]", icon_fg: "text-[#305469]", blurb: "Monitors & home devices" },
  "elderly-care": { icon: HeartPulse, tile: "bg-[#fdeff0]", icon_fg: "text-[#b12a37]", blurb: "Support for healthy ageing" },
  "medical-essentials": { icon: Bandage, tile: "bg-[#f3f1ec]", icon_fg: "text-[#6b5a2e]", blurb: "First aid & everyday supplies" },
};

const FALLBACK: CategoryLook = { icon: Pill, tile: "bg-brand-50", icon_fg: "text-brand-700", blurb: "Browse the range" };

export function categoryLook(slug: string): CategoryLook {
  return LOOKS[slug] ?? FALLBACK;
}

/**
 * Starter suggestions shown in an empty search box. These are curated
 * examples of what can be searched (salt, brand, need) — not a claim about
 * what other customers search for, which the API does not expose.
 */
export const SEARCH_SUGGESTIONS = ["Paracetamol", "Vitamin C", "Cetirizine", "Atorvastatin", "Glucometer strips", "ORS"];

export function titleCase(s: string) {
  // Catalogue imports arrive in SHOUTING CASE ("AUGMENTIN 625MG"); render
  // them calmly without mangling units like "mg"/"ml" or numbers.
  if (s !== s.toUpperCase()) return s;
  return s
    .toLowerCase()
    .replace(/\b([a-z])/g, (m) => m.toUpperCase())
    .replace(/(\d)(Mg|Ml|Gm|Mcg|Iu|Kg)\b/g, (_, d, u) => d + u.toLowerCase())
    // Pharmacy abbreviations stay in capitals ("ORS", "Paracip SR", "Dolo DT").
    .replace(ACRONYM_RE, (m) => m.toUpperCase());
}

const ACRONYM_RE = /\b(?:Ors|Sr|Er|Xr|Cr|Mr|Dt|Od|Ds|Hcl|Iv|Spf|Ez|Ct|Am|Lc|Xl|Pr)\b/g;
