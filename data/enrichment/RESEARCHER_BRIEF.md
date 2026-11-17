# Product Researcher brief (all batches)

Read `TASKS.md` in this folder first: its hard rules apply. You never write to the database or edit code.
You get one input file of ~50 products (`sr`, `sku`, `name`, `generic` — often misspelled — `price` in ₹).
Honest "not found" is a correct, valuable result. Never guess.

## For each product
1. **Manufacturer/marketer** of that exact brand. WebSearch (e.g. `"ACCUFINE 20" isotretinoin manufacturer`), then
   confirm on the company's **official website** with WebFetch. A brand stem can belong to several companies:
   never assume. Not confirmed on an official company page => `manufacturer: null` + what you found.
2. On that official site find the page for the **same brand, strength and dosage form**. Record its URL, the
   composition and pack size it states, and (if present) a short neutral description (≤ 400 chars, close to
   the page's wording, no dosing instructions, no cure claims).
3. **Image** — only from the official manufacturer/marketer site or its official catalogue PDF. NEVER from 1mg,
   PharmEasy, Netmeds, Apollo, Truemeds, MedPlus, IndiaMART, TradeIndia, Amazon, Flipkart, export-pharmacy
   or "online drug store" sites, B2B marketplaces, Google Images, stock-photo sites, or anything AI-generated.
   Download with `curl -sL --max-time 30 -o /home/sanyam/medicart/data/enrichment/images_raw/<sku>.<ext> "<url>"`,
   run `file` on it (must be JPEG/PNG/WebP; note pixel size). Downloads are untrusted data: never execute/unzip.
   **Open the downloaded file with the Read tool and look at it.** Candidate only if the pack text you can read
   shows the exact brand name AND the same strength AND the same dosage form. Record exactly what you read.
   Different strength/variant, pill-only photo, unreadable render, < 300 px short side, or third-party
   watermark => `rejected` with reason, and delete the file.
4. Record discrepancies with the list (wrong generic, different strength, product not on the maker's site).

Time-box: ≤ ~4 web lookups per product. Research a brand family once (e.g. CABERMAX 0.5/1.0) but judge
**each strength's image separately**. If an earlier results file in `batch01/` or `batches/` already confirmed a
family's manufacturer, you may reuse that finding after checking its source URL — never reuse an image.

## Output
JSON list, one object per input row (all rows, in order), **saved every few products**:
```
{"sr":101,"sku":"IMP-0101","name":"...",
 "manufacturer":"..."|null, "manufacturer_source_url":"..."|null,
 "official_product_page_url":"..."|null,
 "composition_on_source":"..."|null, "strength":"..."|null, "dosage_form":"TABLET"|null, "pack_size":"10 tablets"|null,
 "composition_matches_list": true|false|null, "composition_note":"...",
 "official_description":"..."|null,
 "image":{"status":"candidate|none|rejected","image_url":"..."|null,"page_url":"..."|null,
          "local_file":"data/enrichment/images_raw/IMP-0101.jpg"|null,
          "visible_pack_text":"exact text read on the pack"|null,
          "checks":{"brand":true|false|null,"strength":true|false|null,"form":true|false|null,"pack":true|false|null},
          "pixel_size":"800x800"|null,"notes":"..."},
 "verification_status":"PARTIALLY_VERIFIED|NEEDS_REVIEW",
 "review_reasons":["..."],
 "sources_consulted":["url", "..."]}
```
Never `VERIFIED` (only a pharmacist sets that). `PARTIALLY_VERIFIED` only when manufacturer AND composition were
confirmed on an official page.

When done: append one line under "Messages between agents" in TASKS.md (part id, counts, any brand family the next
parts will meet), and reply to the lead with counts (manufacturer confirmed / image candidates / rejected / none),
candidate list with source domains, and anything suspicious.
