"""Write data/enrichment/images_unresolved.tsv: every product with no genuine
image (pending or verified), with the most specific reason known.

Run: python3 -I data/tools/images_unresolved.py
"""
import collections
import glob
import json
import os
import subprocess

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
E = os.path.join(ROOT, "enrichment", "catalogues")

reason = {}
for s, o in json.load(open(f"{E}/multi_manufacturer.json")).items():
    reason[s] = "Brand sold by several manufacturers (" + ", ".join(o) + "): confirm supplier's maker"
for f in sorted(glob.glob(f"{E}/*_matches_v3.json")) + [f"{E}/hab_matches.json"]:
    c = os.path.basename(f).split("_")[0]
    for x in json.load(open(f)):
        if x["status"] == "ambiguous":
            reason.setdefault(x["sku"], f"Ambiguous ({c}): several catalogue products match: " + "; ".join(cc["name"] for cc in x["candidates"]))
        elif x["status"] == "form_conflict":
            reason.setdefault(x["sku"], f"Form conflict ({c}): catalogue form differs from list: " + "; ".join(cc["name"] for cc in x["candidates"]))
        elif x["status"] == "matched" and not x.get("images"):
            reason.setdefault(x["sku"], f"Manufacturer confirmed ({c}) but its catalogue has no pack image")
for qf in ["hp_qa_merged.json", "r2_qa.json", "r3_qa.json", "hab_qa.json"]:
    p = f"{E}/qa/{qf}"
    if os.path.exists(p):
        for q in json.load(open(p)):
            if q["image_verdict"] == "reject":
                reason[q["sku"]] = "QA rejected manufacturer image: " + (q.get("note") or "")

out = subprocess.run(
    ["docker", "exec", "-i", "medicart-db", "psql", "-U", "medicart", "-d", "medicart", "-At", "-F", "\t", "-c",
     "SELECT p.sku, p.name, p.is_active::text, COALESCE(e.regulatory_group,'-') FROM products p "
     "LEFT JOIN product_enrichment e ON e.product_id=p.id WHERE NOT EXISTS (SELECT 1 FROM product_images i "
     "WHERE i.product_id=p.id AND NOT i.is_placeholder AND i.verification_status<>'REJECTED') ORDER BY p.sku"],
    capture_output=True, text=True, check=True).stdout

counts, n = collections.Counter(), 0
with open(os.path.join(ROOT, "enrichment", "images_unresolved.tsv"), "w") as fh:
    fh.write("sku\tname\tactive\tregulatory_group\treason\n")
    for line in out.splitlines():
        if not line:
            continue
        sku, name, active, grp = line.split("\t", 3)
        r = reason.get(sku) or (
            "Inactive: not approved for human use (not researched)" if grp == "not_approved_for_human_use"
            else "Demo/test product with a fictional brand: no genuine pack exists" if not sku.startswith("IMP-")
            else "No genuine image on the official manufacturer catalogues checked (Healing Pharma, Knoll, American Remedies, HAB)")
        counts[r.split(":")[0].split(" (")[0]] += 1
        n += 1
        fh.write(f"{sku}\t{name}\t{active}\t{grp}\t{r}\n")
print(n, "products without a genuine image")
for k, v in counts.most_common():
    print(f"  {v:5d}  {k}")
