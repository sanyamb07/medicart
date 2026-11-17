# MediCart catalogue enrichment — shared task board

Lead: main session (also acts as Database Engineer — the ONLY writer to the database).
Agents never write to the database. They write findings to files in this folder;
the lead integrates them; the QA agent reviews before anything is marked complete.

Source list: `data/source/medicine_list_2026A.json` (3,300 rows, `sr` = SR NO).
Product SKU in the DB = `IMP-%04d` of SR NO (e.g. SR 5 -> IMP-0005).

## Verification vocabulary (used everywhere)
- `VERIFIED` — set only by a human pharmacist in the admin screen. Agents and scripts never set it.
- `PARTIALLY_VERIFIED` — some facts confirmed from a cited official source, something still missing.
- `NEEDS_REVIEW` — information or image needs a human; always give a reason.
Image candidates are `PENDING` until a human approves (`VERIFIED`) or rejects (`REJECTED`).
Pending images are NOT shown to customers; the storefront shows a neutral placeholder.

## Hard rules
1. Never invent a fact, URL, manufacturer, strength or image. Unknown = null + reason.
2. Image sources: official manufacturer / marketer websites and their official catalogues ONLY.
   No pharmacy retailers (1mg, PharmEasy, Netmeds, Apollo, etc.), no marketplaces, no image search
   results, no stock photos, no AI images.
3. An image is a candidate only if the pack text you can SEE in the downloaded file shows the exact
   brand name, strength and dosage form of the row. Different strength/form/pack = reject.
4. Same generic != same product. Never reuse one brand's image or facts for another brand or strength.
5. Preserve source names and prices. Flag problems; never silently "fix" them.

## Tasks
| ID | Task | Owner | Status |
|----|------|-------|--------|
| T1 | Audit all 3,300 source rows vs DB (duplicates, wrong generics, missing strength/form, suspicious) -> `audit/` | Auditor | done |
| T2 | Batch 01 products SR 1–50: manufacturer + official image + pack facts -> `batch01/results_part1.json` | Researcher A | done |
| T3 | Batch 01 products SR 51–100 -> `batch01/results_part2.json` | Researcher B | done |
| T4 | Schema: provenance + verification workflow (migration 000012) | Lead | done |
| T5 | Import the 812 missing rows (user 2026-11-16: add everything; all active Rx EXCEPT products not approved for human use -> inactive NEEDS_REVIEW) | Lead | done (3,300 in DB) |
| T6 | Re-run enrichment for new rows; quarantine text for rows with wrong generics (from T1) | Lead | done: 188 flagged, text withheld on 45 suspect compositions, 7 dosage forms corrected, 4 steroid rows moved to inactive (logs/03, logs/04) |
| T7 | Integrate batch 01 results into DB (images PENDING, manufacturer, provenance) | Lead | blocked: T2, T3, T4 |
| T8 | QA review of batch 01 (re-open every image, re-check every claim) -> `batch01/qa_report.json` | QA | blocked: T2, T3 |
| T9 | Frontend: show only VERIFIED images to customers; admin review of pending images | Lead | done |
| T10 | Batches 02+: `batches/part_001..063.json` (SR 101–3300, 3,125 products; 75 inactive not-for-human-use excluded). Waves of 4 researchers; brief in `RESEARCHER_BRIEF.md`; output `batches/results_NNN.json` | Researchers | wave 1 (parts 001–004) in_progress |
| T11 | QA review of each wave before integration -> `batches/qa_wave_NN.json` | QA | blocked on each wave |

## User decisions (2026-11-16)
- Import every row. Active (Rx-only) includes testosterone, nandrolone, mesterolone, ED, oncology, MTP kits, tapentadol/modafinil/Z-drugs (flagged controlled), hormones, veterinary.
- Inactive + NEEDS_REVIEW (regulatory_group `not_approved_for_human_use`): trenbolone, boldenone, drostanolone, methenolone, stanozolol, oxandrolone, oxymetholone, methandienone, 4-chlorodehydromethyltestosterone, clenbuterol, MK-677.
- Do NOT use 1mg (or any retailer) images or text. Manufacturer sites only, as before.

## Messages between agents
Append here (newest last) when your finding affects another agent:
- Auditor (T1 done, 2026-11-16): `audit/audit_rows.json` (3,300 rows) + `audit/audit_summary.md`; script `tools/audit_source.py`, judgement flags in `audit/manual_findings.json`. 51 rows `suspected_wrong_generic` (4 confirmed on official sites: sr 300, 1092, 1276, 1833); ~15 of them already have enrichment `completed`/category text built from the wrong generic -> quarantine in T6. Researchers (T10): do NOT trust the generic column for those 51 rows + 10 `incomplete_generic` rows; establish composition from the pack.
- Auditor -> Lead: thyroxine/liothyronine rows say MG (are MCG, Healing Pharma site confirms for Healtroxin); inhalers say MG (MCG). sr 1151 GENTALAB price 46935 is a data-entry error. sr 653 CUTRA ('CUT MIX' steroid blend), 109 ANABOL TAB (possibly methandienone) and fluoxymesterone sr 1084/1085/1196 are active but may belong in the inactive not-for-human-use set.
- Lead (12:25): audit applied. 45 products have generated text withheld because the listed composition is suspect — researchers: for these, the official page's composition is the most valuable thing you can find.
- Researcher part_002 (SR 160–209): 50 rows written to batches/results_002.json. Manufacturer confirmed 22, image candidates 6 (all healingpharma.in: ATARISE 25, ATENHEAL 50/100, ATOMOXET 10/25/40), rejected 0, none 44. SR 167/169 NOT researched (user rejected web search mid-task). Wrong generics: ASTHAFEN = ketotifen (not salbutamol), ASCOAIR = salbutamol (not vit C), ATROCAN likely atropine (not amoxyclav), Sildamax/artemisinin suspect. Families for later parts: Healing Pharma WooCommerce store API (/wp-json/wc/store/products) lists ~1,020 products with images; Sun Pharma full India list at sunpharma.com/india-products/; Cipla PIs at ciplamed.com/product-index/<brand>; GSK India PIs at india-pharma.gsk.com; HAB (Artvigil/Modvigil) at habpharma.com; Samarth (Asklerol) at samarthlife.com/all-products; '-CAN' injections (Artecan/Ascorcan/Atrocan) = American Remedies per marketplaces only, no official site.
- Researcher A (T2 done): `batch01/results_part1.json`, 50 rows. Manufacturer and composition confirmed on an official page for 20 (PARTIALLY_VERIFIED), 30 NEEDS_REVIEW. Image candidates 9/50 (18%): Accufine 5/10/20/30/40 (healingpharma.in), Acarboz-25 and Acloveer-400 (knollhealthcare.com), Acylovir 800 (prevegohealthcare.com, Wix CDN), ADR inj (americanremedies.in). Rejected 2 (Acarjohn image is the 25 mg pack, row is 50; Acnetop official = clindamycin+nicotinamide GEL, not adapalene cream). Cipla, Sun, Medicamen and Torrent confirm brands as text only, with no pack images.
- Researcher A -> B/QA/Lead: official sites with machine-readable catalogues: healingpharma.in, knollhealthcare.com and americanremedies.in (WordPress `/wp-json/wp/v2/product?search=` plus `/media/<id>`), ciplamed.com/product-index/<brand>, medicamen.com/product/view/10 (oncology table), sunpharma.com/india-products/. leeford.in refused connections (Acimol, Acnetoin), so retry it. Johnlee's site is TradeIndia-hosted (images on tistatic.com). The audit inferred TABLET for Accufine, Actoid, Acnetoin and Acutret, but these are capsules. Possible typos in the list: SR 1 'A RET 0.5%' (likely 0.05%), SR 46 'ADAFERIN 0.01%' (should be 0.1%). SR 37 Acnetor AD is a GEL (Torrent PI), but the list says cream. SR 49 says 'CFC' inhaler. Scratchpad clash: B's `scratchpad/build/build.py` overwrote mine. I moved my files to `scratchpad/researcherA/`, and running B's script once regenerated results_part2.json from B's own records only.
- Researcher B (T3 done): batch01/results_part2.json, 50 rows. 19 PARTIALLY_VERIFIED / 31 NEEDS_REVIEW; 6 image candidates (all healingpharma.in: SR 51, 52, 66, 89, 99, 100), 2 rejected (SR 58 AlbuRel: wrong variant + small; SR 61 Aldactone: RPG packshot shows no strength and is reused for Aldactone T), 42 none. Findings for Auditor/Lead: SR 62 ALDIGESIC-TH has no paracetamol per Alkem (that is ALDIGESIC TH PLUS), so the list generic or name is wrong; SR 72 ALMOX CV 625 = amoxicillin 500 + clavulanic acid 125 (Alkem); SR 91 AMLPI 10MG is probably a typo/duplicate of SR 85 AMLIP 10MG (same price); SR 97/98 AMOXYCLAV dry syrup rows are duplicates, and Abbott lists 3 different 30 ml syrups (plain/Safe/Duo); SR 70/71 ALMOX are capsules, not tablets; SR 78 AMDATIME is probably a capsule. Healing Pharma's page text for Amoxyheal CV 375/1000 is copied from another brand ("Augmine"), so do not reuse that text. Process note: the shared scratchpad `build/` dir is used by more than one agent, and I once ran another agent's build.py by mistake. It rebuilt batches/results_001.json from that agent's own f_*.json inputs. Please re-check that file.
- part_001 (SR 101-159, Researcher wave1): results_001.json - 50 rows; manufacturer confirmed on official source 20 (incl. 3 Arachitol brand-level only); PARTIALLY_VERIFIED 15; image candidates 10 (all healingpharma.in: Amoxyheal CV 625/dry syrup, Anaridex 1, Angiotensin 2.5/5/10/20, Apismart 2.5/5, Arkacan 100); rejected 0; none 40. Families next parts will meet: Healing Pharma (healingpharma.in, product sitemap lists ~1020 products - check it first), Torrent (Ampoxin/Arkamin official PI PDFs on torrentpharma.com), Hegde & Hegde (hhpharma.com, AMRFY/TRFY etc., many images are 'Coming soon' placeholders), Eisai India (eisai.co.in/products lists brand+molecule only). Flags: SR 109 ANABOL listed as anastrozole but name suggests methandienone; SR 139 APPOQUIL = veterinary oclacitinib. WebSearch session budget ran out mid-part.
- part_003 (SR 210-259, Researcher): 50 rows -> batches/results_003.json. Manufacturer confirmed on official source 11 (Healing Pharma: Azacan 50, Azeetop 100DT/1g/500, Azelax Cream; GSK Augmentin 625/1g Duo via PI; Sunrise Remedies Avana 50/100/200; Ipca Azifast 2% gel via price list), image candidates 6, rejected 1, none 43. Families next parts may meet: Healing Pharma WP API (healingpharma.in/wp-json/wp/v2/product?search=) covers Azeetop syrups 100/200 XL; Ipca price-list PDF lists Azifast 250/500 tabs + Readymix; Cipla Azee/Azicip, Intas Axepta (intaspharma.com/products returns HTTP 500), Himalaya (HTTP 429) unconfirmed. Augmentin "R" suffix in source list is unexplained - flagged.
- Lead (PAUSED): all agents stopped — the user rejected several web requests and told an agent to stop and wait. Research files done: batch01/results_part1, results_part2, batches/results_001..003 (002 missing SR 167/169 by user's refusal); results_004 partial. No QA verdict files completed; NOTHING from research has been written to the DB. Do not resume without the user's instruction.
- Lead (catalogue matching, user-approved option 1): per-product web research replaced by official-catalogue matching (tools/match_catalogue.py). Catalogues read: Healing Pharma (1,020), Knoll Healthcare (1,068), American Remedies (426), HAB Pharmaceuticals (317); Sunrise Remedies blocks automated access (403) — not bypassed. Local-only visual QA (catalogues/qa/QA_LOCAL_BRIEF.md). Integrated as PENDING: 271 images (logs/06–08). 19 brands found at two manufacturers are excluded (catalogues/multi_manufacturer.json). Unresolved list: images_unresolved.tsv. HAB QA in progress.
- Lead (2026-11-17): HAB Pharmaceuticals integrated (12 images PENDING, 39 manufacturers; logs/09). Totals: 283 genuine images PENDING pharmacist approval, 331 manufacturers confirmed, 0 broken image URLs. Unresolved: images_unresolved.tsv (3,029; regenerate with tools/images_unresolved.py). Not integrated: the per-product researcher files (batch01/, batches/results_001–004) — never QA'd because research was paused at the user's request.
