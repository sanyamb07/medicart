"""Match a manufacturer's WooCommerce catalogue to the supplier list.

A list row matches a catalogue product only when
  * the brand words are identical (after removing form/unit words), and
  * the strength numbers are identical as a set, and
  * the dosage forms agree when both sides state one.
Ambiguous results (several catalogue products) are reported, never guessed.

Usage: python3 -I data/tools/match_catalogue.py <catalogue_dir> <source.json> <db_forms.tsv> <out.json>
"""
import glob
import html
import json
import re
import sys

cat_dir, source_path, forms_path, out_path = sys.argv[1:5]

STOP = set("""MG MCG GM G ML IU TAB TABS TABLET TABLETS CAP CAPS CAPSULE CAPSULES SOFTGEL SG INJ INJECTION
CREAM GEL OINT OINTMENT LOTION SOL SOLUTION SPRAY NASAL DROP DROPS EYE EAR SYP SYRP SYRUP SUSP SUSPENSION DRY
SHAMPOO SACHET POWDER IP BP USP W V WW AMP VIAL STRIP PACK OF X SOFT""".split())
# Release/variant words are part of the product identity (Metoprolol XL != plain), so they are KEPT.

FORM_WORDS = [
    ("INHALER", r"INHALER|ROTACAP"), ("INJECTION", r"INJ|INJECTION|VIAL|AMPOULE"), ("DROPS", r"\bDROPS?\b"),
    ("OINTMENT", r"OINT"), ("CREAM", r"CREAM"), ("GEL", r"\bGEL\b"), ("LOTION", r"LOTION"),
    ("SPRAY", r"SPRAY"), ("SHAMPOO", r"SHAMPOO"), ("SYRUP", r"SYRUP|\bSYP\b"), ("SUSPENSION", r"SUSPENSION|DRY SYRUP"),
    ("SOLUTION", r"SOLUTION|\bSOL\b"), ("CAPSULE", r"CAPSULE|\bCAPS?\b|SOFTGEL"), ("TABLET", r"TABLET|\bTABS?\b"),
]


def form_of(text):
    t = text.upper()
    for f, rx in FORM_WORDS:
        if re.search(rx, t):
            return f
    return None


def compatible(a, b):
    if not a or not b or a == b:
        return True
    pairs = {frozenset(["SYRUP", "SUSPENSION"]), frozenset(["SOLUTION", "SHAMPOO"]), frozenset(["SOLUTION", "DROPS"])}
    return frozenset([a, b]) in pairs


def key(name):
    t = html.unescape(name).split(" \u2013 ")[0]     # "Brand 20 mg – Composition" -> "Brand 20 mg"
    t = t.upper().replace("®", " ").replace("™", " ")
    t = re.sub(r"\([^)]*\)", " ", t)                 # packaging codes: (ALU), (BL), (STRIP)
    t = re.sub(r"\b\d+(?:\s*\*\s*\d+)+\b", " ", t)    # pack counts: 20*10, 10*1*10
    t = re.sub(r"(\d)\s*(MG|MCG|GM|ML|IU|G)\b", r"\1 ", t)
    t = re.sub(r"[^A-Z0-9.]+", " ", t)
    words, nums = [], []
    for tok in t.split():
        tok = tok.strip(".")
        if not tok:
            continue
        if re.fullmatch(r"\d+(?:\.\d+)?", tok):
            nums.append(str(float(tok)).rstrip("0").rstrip("."))
        elif re.fullmatch(r"\d+[A-Z]+", tok):            # "60K", "3L" style
            nums.append(tok)
        elif tok not in STOP:
            words.append(tok)
    return " ".join(words), tuple(sorted(nums))


catalogue = []
for f in sorted(glob.glob(f"{cat_dir}/page_*.json")):
    for p in json.load(open(f)):
        short = re.sub(r"<[^>]+>", " ", html.unescape(p.get("short_description") or "")).strip()
        catalogue.append({
            "name": html.unescape(p["name"]), "url": p["permalink"], "sku": p.get("sku"),
            "composition": re.sub(r"\s+", " ", short),
            "images": [i["src"] for i in p.get("images") or []],
            "key": key(p["name"]), "form": form_of(p["name"] + " " + short),
        })
by_key = {}
for c in catalogue:
    by_key.setdefault(c["key"], []).append(c)

db_forms = {}
for line in open(forms_path):
    sku, form = line.rstrip("\n").split("\t")
    db_forms[sku] = form or None

rows = json.load(open(source_path))
out, stats = [], {"matched": 0, "ambiguous": 0, "form_conflict": 0, "with_image": 0}
for r in rows:
    sku = "IMP-%04d" % r["sr"]
    k = key(r["name"])
    cands = by_key.get(k, [])
    if not cands:
        continue
    list_form = form_of(r["name"]) or db_forms.get(sku)
    ok = [c for c in cands if compatible(list_form, c["form"])]
    if not ok:
        stats["form_conflict"] += 1
        out.append({"sku": sku, "name": r["name"], "status": "form_conflict",
                    "candidates": [{"name": c["name"], "url": c["url"], "form": c["form"]} for c in cands], "list_form": list_form})
        continue
    if len(ok) > 1:
        stats["ambiguous"] += 1
        out.append({"sku": sku, "name": r["name"], "status": "ambiguous",
                    "candidates": [{"name": c["name"], "url": c["url"]} for c in ok]})
        continue
    c = ok[0]
    stats["matched"] += 1
    stats["with_image"] += bool(c["images"])
    out.append({"sku": sku, "sr": r["sr"], "name": r["name"], "list_generic": r["generic"], "status": "matched",
                "catalogue_name": c["name"], "catalogue_sku": c["sku"], "url": c["url"], "composition": c["composition"],
                "catalogue_form": c["form"], "list_form": list_form, "images": c["images"]})

json.dump(out, open(out_path, "w"), indent=1)
print(json.dumps({"catalogue_products": len(catalogue), **stats}))
