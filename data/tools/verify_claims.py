"""Independently re-verify researcher marketer claims.

A claim is confirmed only when the cited page (on the company's own domain,
never a retailer) can be fetched now and its text contains the brand name
from the product row. Pages that block automated access, error, or do not
mention the brand stay unconfirmed. PDFs are checked with pdftotext.

Usage: python3 -I data/tools/verify_claims.py <claims.json> <out.json>
"""
import json
import os
import re
import subprocess
import sys
import tempfile
import time

claims_path, out_path = sys.argv[1:3]
claims = json.load(open(claims_path))
STOP = {"TAB", "TABS", "TABLET", "CAP", "CAPS", "INJ", "CREAM", "GEL", "SYP", "SYRUP", "DROP", "DROPS", "EYE", "EAR",
        "DRY", "SUSP", "INHALER", "MG", "ML", "GM", "IU", "MCG", "SR", "XL", "DT", "OD", "ER", "CR", "MD", "DS", "PLUS", "FORTE",
        "SPRAY", "OINT", "RES", "SOL", "LOTION", "SHAMPOO", "SOFT", "DUO", "KIT", "VIAL", "AMP"}


def brand_of(name):
    words = [w for w in re.sub(r"[^A-Z ]", " ", name.upper()).split() if w not in STOP and len(w) >= 3]
    return words[0] if words else None


cache, out = {}, []
for c in claims:
    brand = brand_of(c["name"])
    url = c["url"]
    if url not in cache:
        with tempfile.NamedTemporaryFile(delete=False) as tmp:
            path = tmp.name
        r = subprocess.run(["curl", "-s", "-L", "-A", "Mozilla/5.0 (catalogue check)", "--max-time", "30",
                            "-o", path, "-w", "%{http_code} %{content_type}", url], capture_output=True, text=True)
        code_ct = r.stdout.strip()
        text = ""
        if code_ct.startswith("200"):
            if "pdf" in code_ct.lower() or url.lower().endswith(".pdf"):
                p = subprocess.run(["pdftotext", "-q", path, "-"], capture_output=True, text=True)
                text = p.stdout
            else:
                text = open(path, encoding="utf-8", errors="replace").read()
        os.unlink(path)
        cache[url] = (code_ct, re.sub(r"\s+", " ", re.sub(r"<[^>]+>", " ", text)).upper())
        time.sleep(1)
    code_ct, text = cache[url]
    ok = bool(brand) and code_ct.startswith("200") and re.search(r"\b" + re.escape(brand), text) is not None
    out.append({**c, "brand": brand, "http": code_ct, "confirmed": ok,
                "note": "brand named on the official page" if ok else
                        ("page could not be fetched (" + code_ct + ")" if not code_ct.startswith("200") else f"page does not name '{brand}'")})
json.dump(out, open(out_path, "w"), indent=1)
print(json.dumps({"claims": len(out), "confirmed": sum(o["confirmed"] for o in out)}))
