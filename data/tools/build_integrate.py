"""Join researcher results with the QA verdicts into a catalog-sync input file.

Only what QA approved passes:
  * image      -> only when QA verdict for the image is "approve"
  * manufacturer / official URLs / pack size -> only when QA confirmed them
Everything else becomes a review reason, so nothing is silently dropped.

Usage: python3 -I data/tools/build_integrate.py <results.json> <qa.json> <out.json>
"""
import json
import sys

results_path, qa_path, out_path = sys.argv[1:4]
results = {r["sku"]: r for r in json.load(open(results_path))}
qa = {q["sku"]: q for q in json.load(open(qa_path))}

out, stats = [], {"rows": 0, "image_approved": 0, "image_rejected_by_qa": 0, "manufacturer_confirmed": 0, "missing_qa": 0}
for sku, r in results.items():
    stats["rows"] += 1
    q = qa.get(sku)
    if q is None:
        stats["missing_qa"] += 1
        continue
    item = {"sku": sku, "manufacturer": None, "manufacturer_source_url": None,
            "official_product_page_url": None, "pack_size": None, "review_reasons": [], "image": None}
    if q.get("manufacturer_confirmed"):
        stats["manufacturer_confirmed"] += 1
        item["manufacturer"] = r.get("manufacturer")
        item["manufacturer_source_url"] = r.get("manufacturer_source_url")
        item["official_product_page_url"] = r.get("official_product_page_url")
        if q.get("pack_size_confirmed"):
            item["pack_size"] = r.get("pack_size")
    img = r.get("image") or {}
    if q.get("image_verdict") == "approve" and img.get("local_file"):
        stats["image_approved"] += 1
        item["image"] = {
            "local_file": img["local_file"], "image_url": img.get("image_url") or "",
            "page_url": img.get("page_url") or "", "source_name": q.get("source_name") or r.get("manufacturer") or "",
            "visible_pack_text": q.get("visible_pack_text") or img.get("visible_pack_text") or "", "qa_note": q.get("note") or "",
        }
    elif q.get("image_verdict") == "reject":
        stats["image_rejected_by_qa"] += 1
        item["review_reasons"].append("Image candidate rejected by QA: " + (q.get("note") or "mismatch"))
    if r.get("composition_matches_list") is False:
        item["review_reasons"].append("Official page composition differs from list: " + (r.get("composition_on_source") or "?"))
    if not item["image"]:
        item["review_reasons"].append("No verified manufacturer pack image found")
    item["review_reasons"] += [x for x in (q.get("extra_reasons") or []) if x]
    out.append(item)

json.dump(out, open(out_path, "w"), indent=1)
print(json.dumps(stats))
