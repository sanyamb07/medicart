import type { Product } from "../types";

// Consumables that are useful to stock but make a poor first impression as a
// featured shelf (a ₹1 needle is not an "everyday essential" for most people).
const CONSUMABLE = /\b(syringe|syring|needle|cannula|prep pad|swab|lancet)\b/i;

// rankEssentials orders no-prescription products for a featured shelf:
// branded catalogue items first, then medicines and wellness products, then
// devices, with single-use consumables last. Order within a group is kept.
export function rankEssentials(products: Product[]): Product[] {
  const score = (p: Product) => {
    if (CONSUMABLE.test(p.name)) return 3;
    if (p.dosageForm === "DEVICE") return 2;
    if (p.brandName) return 0;
    return 1;
  };
  return products
    .map((p, i) => ({ p, i, s: score(p) }))
    .sort((a, b) => a.s - b.s || a.i - b.i)
    .map((x) => x.p);
}

// "Marketed by Healing Pharma India Pvt. Ltd.; manufactured by …" → "Healing Pharma India".
export function marketerShort(info: string | null | undefined): string | null {
  if (!info) return null;
  const m = /^Marketed by\s+([^;]+)/i.exec(info);
  const name = (m ? m[1] : info).replace(/\b(Pvt\.?|Private|Ltd\.?|Limited|LLP)\b\.?/gi, "").replace(/[\s,.]+$/, "").trim();
  return name || null;
}

/** "Paracetamol & Dicyclomine Hcl" and "dicyclomine hcl + paracetamol" -> one key. */
export function compositionKey(generic: string | null | undefined): string {
  if (!generic) return "";
  return generic
    .toLowerCase()
    .replace(/\s+/g, " ")
    .split(/\s*(?:\+|&|,|\band\b|\bwith\b)\s*/)
    // "HCl" and "hydrochloride" name the same salt; nothing else is folded.
    .map((s) => s.replace(/[^a-z0-9 .%]/g, "").replace(/\bhcl\b/g, "hydrochloride").trim())
    .filter(Boolean)
    .sort()
    .join(" + ");
}
