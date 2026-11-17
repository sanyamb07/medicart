# Local visual QA brief (catalogue images)

These images were matched by name from the manufacturer's own catalogue (healingpharma.in store API,
saved locally in `data/enrichment/catalogues/healingpharma/page_*.json`). Your job is the visual check.

**Do not use the web at all** (no WebSearch, no WebFetch, no curl). Local files only. Never write to the database
or edit code.

For each row in your input file:
1. Open `/home/sanyam/medicart/<image.local_file>` with the Read tool and look at it.
2. Read the pack text. Approve ONLY if all of these hold:
   - the brand name on the pack matches the row `name` (ignore hyphens/case; a list typo like ANGIOTESIN vs
     Angiotensin is OK but add an extra_reason);
   - the strength on the pack matches the row `name` (10MG ≠ 20MG, 0.025% ≠ 0.1%). If the row name has no
     strength, the pack must still be the plain variant (not "Forte", "Plus", "DS", "XL", "CR" unless the row says so);
   - the dosage form on the pack matches (tablet ≠ capsule, cream ≠ gel ≠ ointment, solution ≠ tablet);
   - it is a real product pack/carton/bottle photo or render with readable text — not a placeholder,
     "coming soon", logo, pill-only photo, or a different product.
   If the pack shows no strength at all, reject (note "strength not visible").
3. If the pack's composition clearly differs from `list_generic`, still judge the image on brand/strength/form,
   but add an extra_reason "Pack composition: <what you read>; list says <list_generic>".

Write a JSON list (one object per input row, same order), saving every ~20 rows:
```
{"sku":"IMP-0383","image_verdict":"approve|reject","manufacturer_confirmed":true,"pack_size_confirmed":false,
 "source_name":"Healing Pharma India Pvt. Ltd.","visible_pack_text":"exact text you read (brand, strength, form, pack)",
 "note":"why approved/rejected","extra_reasons":[]}
```
`manufacturer_confirmed` is true for every row (the product is on the manufacturer's own catalogue).
Reply to the lead with: approved / rejected counts, and the rejected SKUs with reasons.
