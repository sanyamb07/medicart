# MediCart enrichment progress
_Generated 2026-11-17 06:38 UTC from the live database._

| Measure | Count |
|---|---|
| Medicines in source list | 3300 |
| Source rows present in DB (one product each, SKU IMP-<SR>) | 3300 |
| Other (pre-existing demo) products in DB | 12 |
| Prices identical to source | 3300 |
| Active (listed) | 3220 |
| Inactive pending regulatory / data review | 80 |
| VERIFIED (pharmacist checked info + image) | 0 |
| PARTIALLY_VERIFIED | 2743 |
| NEEDS_REVIEW | 557 |
| With description from a cited source | 2740 |
| Description text withheld (suspected wrong composition) | 44 |
| With manufacturer (from official source) | 415 |
| Product photos awaiting pharmacist review (hidden from customers) | 4 |
| Product photos verified (shown to customers) | 303 |
| Showing neutral placeholder | 2997 |
| Enrichment failed | 0 |

## Images (whole database, all products)
| Measure | Count |
|---|---|
| Products in database | 3312 |
| Showing a dummy illustration (placeholder) to customers | 3007 |
| With no image at all | 2 |
| Genuine images held for manual verification / pharmacist decision | 4 |
| Genuine images approved and shown to customers | 303 |
|   of which approved by Claude on the owner's instruction (not a pharmacist) | 303 |
|   of which approved by a named staff account | 0 |
| Image rows rejected by a pharmacist in admin (QA rejections are recorded as review reasons instead) | 0 |

## By regulatory group
| Group | Active | Inactive |
|---|---|---|
| approved_rx | 2992 | 1 |
| controlled_abuse_prone | 129 | 0 |
| not_approved_for_human_use | 0 | 79 |
| otc_or_device | 46 | 0 |
| veterinary | 53 | 0 |

## By pipeline status
| Status | Count |
|---|---|
| completed | 2406 |
| needs_manual_review | 556 |
| partially_completed | 338 |

## Products needing manual review
Full list with reasons: `data/enrichment/manual_review.tsv`.

1119 products listed.
