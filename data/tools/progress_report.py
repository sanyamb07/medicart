"""Write data/enrichment/PROGRESS.md from the live database.

Every number is queried, never typed, so the report cannot drift from the
data. Run: python3 -I data/tools/progress_report.py
"""
import json
import os
import subprocess
import sys
from datetime import datetime, timezone

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def q(sql):
    out = subprocess.run(
        ["docker", "exec", "-i", "medicart-db", "psql", "-U", "medicart", "-d", "medicart", "-At", "-F", "\t", "-c", sql],
        check=True, capture_output=True, text=True).stdout
    return [line.split("\t") for line in out.strip().splitlines() if line]


def one(sql):
    return q(sql)[0][0]


IMP = "p.sku LIKE 'IMP-%'"
J = "FROM products p JOIN product_enrichment e ON e.product_id = p.id"

src = json.load(open(os.path.join(ROOT, "source", "medicine_list_2026A.json")))
lines = []
w = lines.append
w("# MediCart enrichment progress")
w(f"_Generated {datetime.now(timezone.utc).strftime('%Y-%m-%d %H:%M UTC')} from the live database._\n")
w("| Measure | Count |\n|---|---|")
rows = [
    ("Medicines in source list", len(src)),
    ("Source rows present in DB (one product each, SKU IMP-<SR>)", one(f"SELECT count(*) FROM products p WHERE {IMP}")),
    ("Other (pre-existing demo) products in DB", one(f"SELECT count(*) FROM products p WHERE NOT {IMP}")),
    ("Prices identical to source", one(f"SELECT count(*) {J} WHERE {IMP} AND (e.source_row->>'price')::numeric*100 = p.price_paise")),
    ("Active (listed)", one(f"SELECT count(*) FROM products p WHERE {IMP} AND p.is_active")),
    ("Inactive pending regulatory / data review", one(f"SELECT count(*) FROM products p WHERE {IMP} AND NOT p.is_active")),
    ("VERIFIED (pharmacist checked info + image)", one(f"SELECT count(*) {J} WHERE {IMP} AND e.verification_status='VERIFIED'")),
    ("PARTIALLY_VERIFIED", one(f"SELECT count(*) {J} WHERE {IMP} AND e.verification_status='PARTIALLY_VERIFIED'")),
    ("NEEDS_REVIEW", one(f"SELECT count(*) {J} WHERE {IMP} AND e.verification_status='NEEDS_REVIEW'")),
    ("With description from a cited source", one(f"SELECT count(*) {J} WHERE {IMP} AND e.info_source_url IS NOT NULL AND p.description IS NOT NULL")),
    ("Description text withheld (suspected wrong composition)", one(f"SELECT count(*) {J} WHERE {IMP} AND e.hold_generated_text")),
    ("With manufacturer (from official source)", one(f"SELECT count(*) {J} WHERE {IMP} AND p.manufacturer_info IS NOT NULL")),
    ("Product photos awaiting pharmacist review (hidden from customers)", one(f"SELECT count(DISTINCT i.product_id) FROM product_images i JOIN products p ON p.id=i.product_id WHERE {IMP} AND NOT i.is_placeholder AND i.verification_status='PENDING'")),
    ("Product photos verified (shown to customers)", one(f"SELECT count(DISTINCT i.product_id) FROM product_images i JOIN products p ON p.id=i.product_id WHERE {IMP} AND NOT i.is_placeholder AND i.verification_status='VERIFIED'")),
    ("Showing neutral placeholder", one(f"SELECT count(*) FROM products p WHERE {IMP} AND NOT EXISTS (SELECT 1 FROM customer_product_images c WHERE c.product_id=p.id AND c.display_order < 1000000)")),
    ("Enrichment failed", one(f"SELECT count(*) {J} WHERE {IMP} AND e.status='failed'")),
]
for k, v in rows:
    w(f"| {k} | {v} |")

A = "FROM products p"
w("\n## Images (whole database, all products)\n| Measure | Count |\n|---|---|")
for k, v in [
    ("Products in database", one(f"SELECT count(*) {A}")),
    ("Showing a dummy illustration (placeholder) to customers", one(f"SELECT count(*) {A} WHERE EXISTS (SELECT 1 FROM customer_product_images c WHERE c.product_id=p.id) AND NOT EXISTS (SELECT 1 FROM customer_product_images c WHERE c.product_id=p.id AND c.display_order < 1000000)")),
    ("With no image at all", one(f"SELECT count(*) {A} WHERE NOT EXISTS (SELECT 1 FROM product_images i WHERE i.product_id=p.id)")),
    ("Genuine images held for manual verification / pharmacist decision", one("SELECT count(DISTINCT product_id) FROM product_images WHERE NOT is_placeholder AND verification_status='PENDING'")),
    ("Genuine images approved and shown to customers", one("SELECT count(DISTINCT product_id) FROM product_images WHERE NOT is_placeholder AND verification_status='VERIFIED'")),
    ("  of which approved by Claude on the owner's instruction (not a pharmacist)", one("SELECT count(DISTINCT product_id) FROM product_images WHERE NOT is_placeholder AND verification_status='VERIFIED' AND verified_by IS NULL")),
    ("  of which approved by a named staff account", one("SELECT count(DISTINCT product_id) FROM product_images WHERE NOT is_placeholder AND verification_status='VERIFIED' AND verified_by IS NOT NULL")),
    ("Image rows rejected by a pharmacist in admin (QA rejections are recorded as review reasons instead)", one("SELECT count(*) FROM product_images WHERE verification_status='REJECTED'")),
]:
    w(f"| {k} | {v} |")

w("\n## By regulatory group\n| Group | Active | Inactive |\n|---|---|---|")
for g, a, i in q(f"SELECT COALESCE(e.regulatory_group,'-'), count(*) FILTER (WHERE p.is_active), count(*) FILTER (WHERE NOT p.is_active) {J} WHERE {IMP} GROUP BY 1 ORDER BY 1"):
    w(f"| {g} | {a} | {i} |")

w("\n## By pipeline status\n| Status | Count |\n|---|---|")
for s, n in q(f"SELECT e.status, count(*) {J} WHERE {IMP} GROUP BY 1 ORDER BY 2 DESC"):
    w(f"| {s} | {n} |")

w("\n## Products needing manual review\nFull list with reasons: `data/enrichment/manual_review.tsv`.\n")
review = q(f"""SELECT p.sku, p.name, COALESCE(p.generic_name,''), p.is_active::text, e.verification_status,
                      array_to_string(e.review_reasons, ' | '), array_to_string(e.unmatched_ingredients, ', ')
                 {J} WHERE {IMP} AND e.verification_status <> 'VERIFIED'
                   AND (e.verification_status = 'NEEDS_REVIEW' OR cardinality(e.review_reasons) > 0)
                 ORDER BY p.sku""")
with open(os.path.join(ROOT, "enrichment", "manual_review.tsv"), "w") as fh:
    fh.write("sku\tname\tgeneric\tactive\tverification\treasons\tno_source_for\n")
    for r in review:
        fh.write("\t".join(x.replace("\t", " ").replace("\n", " ") for x in r) + "\n")
w(f"{len(review)} products listed.")

open(os.path.join(ROOT, "enrichment", "PROGRESS.md"), "w").write("\n".join(lines) + "\n")
print("\n".join(lines))
