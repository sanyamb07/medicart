"""Resolve catalogue form-conflicts and ambiguous matches where the evidence
is conclusive, and write (a) dosage-form corrections and (b) image candidates
for visual QA. Everything else stays MANUAL_VERIFICATION_REQUIRED.

Rules
  * form_conflict: resolvable only when the list NAME states no form (the DB
    form was an assumption) and the catalogue states one. The catalogue form
    wins; the list name is unchanged.
  * ambiguous: resolvable when (a) the list name's form/variant words pick one
    candidate (SOL -> the solution; no "SOFT" -> not the Soft variant), or
    (b) the candidates are the same product listed twice AND their primary
    images are the same file.

Usage: python3 -I data/tools/resolve_conflicts.py
"""
import glob
import hashlib
import html
import json
import os
import re
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
E = os.path.join(ROOT, "enrichment", "catalogues")
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

MAKERS = {"healingpharma": "Healing Pharma India Pvt. Ltd.", "knoll": "Knoll Healthcare Pvt Ltd",
          "americanremedies": "American Remedies Healthcare Private Limited", "hab": "HAB Pharmaceuticals"}
FORM_WORD = re.compile(r"\b(TAB|TABS|TABLET|CAP|CAPS|CAPSULE|INJ|INJECTION|SOL|SOLUTION|CREAM|OINT|GEL|SYP|SUSP|DROP|DROPS|SPRAY|LOTION)\b")


def catalogue(c):
    by_url = {}
    for f in glob.glob(f"{E}/{c}/page_*.json"):
        for p in json.load(open(f)):
            by_url[p["permalink"]] = p
    return by_url


def fetch_sha(url, dest):
    if not os.path.exists(dest):
        subprocess.run(["curl", "-s", "-A", "Mozilla/5.0 (catalogue check)", "--max-time", "60", "-o", dest, url], check=True)
    return hashlib.sha256(open(dest, "rb").read()).hexdigest()


multi = json.load(open(f"{E}/multi_manufacturer.json"))
form_fixes, candidates, manual = [], [], []
os.makedirs(os.path.join(ROOT, "enrichment", "images_raw", "r4"), exist_ok=True)
files = {c: (f"{E}/{c}_matches_v3.json" if c != "hab" else f"{E}/hab_matches.json") for c in MAKERS}
for c, path in files.items():
    cat = catalogue(c)
    for x in json.load(open(path)):
        if x["status"] not in ("form_conflict", "ambiguous") or x["sku"] in multi:
            continue
        name = x["name"].upper()
        chosen, why = None, None
        if x["status"] == "form_conflict":
            cand = x["candidates"][0]
            if len(x["candidates"]) == 1 and not FORM_WORD.search(name) and cand.get("form") in ("CAPSULE", "SOLUTION"):
                chosen, why = cand, f"catalogue states {cand['form']}; list name states no form (DB form was assumed)"
                form_fixes.append({"sku": x["sku"], "to": cand["form"], "evidence": cand["url"]})
        else:
            cands = x["candidates"]
            if re.search(r"\bSOL\b", name):
                sol = [cc for cc in cands if "%" in cc["name"] and not re.search(r"\d\s*mg", cc["name"], re.I)]
                if len(sol) == 1:
                    chosen, why = sol[0], "list says SOL: the % solution entry"
            elif "SOFT" not in name and len([cc for cc in cands if "SOFT" not in cc["name"].upper()]) == 1 and len(cands) == 2 \
                    and "ODS" not in name:
                chosen = [cc for cc in cands if "SOFT" not in cc["name"].upper()][0]
                why = "list does not say Soft: the plain entry"
            if not chosen:
                # Same product listed twice: accept only if the images are the same file.
                shas = []
                for i, cc in enumerate(cands):
                    p = cat.get(cc["url"])
                    imgs = [im["src"] for im in (p or {}).get("images") or []]
                    if not imgs:
                        shas.append(None)
                        continue
                    ext = imgs[0].rsplit(".", 1)[-1].split("?")[0]
                    shas.append(fetch_sha(imgs[0], os.path.join(ROOT, "enrichment", "images_raw", "r4", f"{x['sku']}_{i}.{ext}")))
                real = [s for s in shas if s]
                if real and len(set(real)) == 1:
                    chosen = cands[shas.index(real[0])]
                    why = "duplicate catalogue entries carry the same image file"
        if not chosen:
            manual.append({"sku": x["sku"], "name": x["name"], "status": x["status"], "candidates": [cc["name"] for cc in x["candidates"]]})
            continue
        p = cat.get(chosen["url"])
        imgs = [im["src"] for im in (p or {}).get("images") or []]
        candidates.append({"sr": int(x["sku"][4:]), "sku": x["sku"], "name": x["name"], "list_generic": "",
                           "manufacturer": MAKERS[c], "manufacturer_source_url": chosen["url"], "official_product_page_url": chosen["url"],
                           "composition_on_source": re.sub(r"<[^>]+>", " ", html.unescape((p or {}).get("short_description") or "")).strip() or None,
                           "catalogue_name": chosen["name"], "pack_size": None, "resolution": why,
                           "image": {"status": "candidate", "image_url": imgs[0], "page_url": chosen["url"], "local_file": None,
                                     "visible_pack_text": None} if imgs else None})

src = {r["sr"]: r for r in json.load(open(os.path.join(ROOT, "source", "medicine_list_2026A.json")))}
for cnd in candidates:
    cnd["list_generic"] = src[cnd["sr"]]["generic"]
    if cnd["image"]:
        ext = cnd["image"]["image_url"].rsplit(".", 1)[-1].split("?")[0]
        dest = os.path.join(ROOT, "enrichment", "images_raw", "r4", f"{cnd['sku']}.{ext}")
        fetch_sha(cnd["image"]["image_url"], dest)
        cnd["image"]["local_file"] = os.path.relpath(dest, os.path.dirname(ROOT))
json.dump(form_fixes, open(f"{E}/r4_form_fixes.json", "w"), indent=1)
json.dump(candidates, open(f"{E}/round4_results.json", "w"), indent=1)
json.dump(manual, open(f"{E}/r4_manual.json", "w"), indent=1)
print(json.dumps({"form_fixes": len(form_fixes), "resolved_candidates": len(candidates),
                  "with_image": sum(1 for c in candidates if c["image"]), "still_manual": len(manual)}))
for m in manual:
    print("  manual:", m["sku"], m["name"], m["status"], m["candidates"])
