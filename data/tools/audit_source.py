"""T1 audit of the supplier price list (medicine_list_2026A.json) against the DB.

Run:  python3 -I data/tools/audit_source.py
Reads:  data/source/medicine_list_2026A.json
        data/enrichment/audit/manual_findings.json   (auditor's judgement flags)
        DB via `docker exec medicart-db psql` -- SELECT only, never writes.
Writes: data/enrichment/audit/audit_rows.json   (one entry per source row)
        data/enrichment/audit/audit_summary.md

Flag codes (severity in brackets is the default):
  exact_duplicate [medium]           same normalised name + generic + price as other sr(s)
  same_name_different_price [medium] same normalised name, different price
  same_name_different_generic [high] same normalised name, different generic
  near_duplicate_name [low]          names equal after removing punctuation/TAB/CAP words
  whitespace_or_case_variant [low]   double/trailing spaces or lower-case in name (DB stores cleaned name)
  generic_formatting [low]           stray spaces/dashes/commas in generic text
  generic_misspelling [low]          generic misspelt; correct INN in detail
  suspected_wrong_generic [*]        generic does not match the brand (manual; see manual_findings.json)
  incomplete_generic [medium]        combination product whose generic lists only part (manual)
  strength_mismatch [medium]         strength in name vs generic text differ (manual)
  strength_unit_suspect [medium]     unit probably wrong (mg for mcg products, inhalers in mg, ...)
  missing_generic [high/medium]      blank generic, or generic is not an ingredient
  generic_truncated [low]            generic text cut off (manual)
  missing_strength [low]             a drug with no strength in the name
  strength_unit_missing [low]        a bare number in the name with no unit (e.g. 'ATORBEST 10')
  form_inferred [low]                dosage form inferred without a form word
  form_ambiguous [low]               e.g. ORAL JELLY / oral gel / ODS strip -> OTHER
  form_unknown [low]                 no form could be inferred
  form_generic_conflict [high]       form word contradicts generic (tablet vs 'GEL', etc.)
  placeholder_name [high]            name is not a product name (e.g. 'F')
  name_typo [low]                    obvious typo in name (manual)
  price_outlier [medium]             price far from same ingredient+form+strength group (or manual)
  db_mismatch [medium]               DB name/generic/price differs from source
  combination_product [low]          2+ ingredients (informational)
  parse_override [low]               parsed strength/form/pack set manually (manual_findings.json)
  needs_pharmacist_check [low]       anything else worth a look (manual)
  regulatory_review [low]            regulatory group could not be decided mechanically
"""
import json
import os
import re
import statistics
import subprocess
from collections import defaultdict

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SRC = os.path.join(ROOT, "source", "medicine_list_2026A.json")
AUDIT_DIR = os.path.join(ROOT, "enrichment", "audit")
MANUAL = os.path.join(AUDIT_DIR, "manual_findings.json")
OUT_ROWS = os.path.join(AUDIT_DIR, "audit_rows.json")
OUT_MD = os.path.join(AUDIT_DIR, "audit_summary.md")

# ---------------------------------------------------------------- DB (read only)
DB_SQL = (
    "SELECT p.sku, p.name, coalesce(p.generic_name,''), coalesce(p.strength,''), "
    "coalesce(p.dosage_form,''), coalesce(p.pack_size,''), p.price_paise, "
    "p.requires_prescription, p.is_active, coalesce(e.status,''), "
    "coalesce(e.therapeutic_category,''), coalesce(e.info_source_url,'') "
    "FROM products p LEFT JOIN product_enrichment e ON e.product_id = p.id "
    "WHERE p.sku LIKE 'IMP-%' ORDER BY p.sku"
)


def load_db():
    out = subprocess.run(
        ["docker", "exec", "-i", "medicart-db", "psql", "-U", "medicart", "-d", "medicart",
         "-At", "-F", "\t", "-c", DB_SQL],
        check=True, capture_output=True, text=True).stdout
    db = {}
    for line in out.splitlines():
        f = line.split("\t")
        if len(f) < 12:
            continue
        cols = ["sku", "name", "generic", "strength", "form", "pack", "price_paise",
                "rx", "active", "enrich_status", "category", "info_url"]
        db[f[0]] = dict(zip(cols, f))
    return db


# ---------------------------------------------------------------- vocab
MISSPELLINGS = {
    "AMOXYCILIN": "AMOXICILLIN", "AMOXICILIN": "AMOXICILLIN", "CLAVULANTE": "CLAVULANATE",
    "ARIPIRAZOLE": "ARIPIPRAZOLE", "FEBENDAZOLE": "FENBENDAZOLE", "MESTERONUM": "MESTEROLONE",
    "MINICYCLINE": "MINOCYCLINE", "MIRTAZPINE": "MIRTAZAPINE", "MOTELUKAST": "MONTELUKAST",
    "NIMUSULIDE": "NIMESULIDE", "OXYMETHALONE": "OXYMETHOLONE", "OXYMETHELONE": "OXYMETHOLONE",
    "ROPRANOLOL": "PROPRANOLOL", "ABIRATERON": "ABIRATERONE", "VERDENAFIL": "VARDENAFIL",
    "NITROFURANTION": "NITROFURANTOIN", "BITARTATE": "BITARTRATE", "FLUOXAMINE": "FLUVOXAMINE",
    "SUMATROPTAN": "SUMATRIPTAN", "EMTRICIABIN": "EMTRICITABINE", "ATOMEXITINE": "ATOMOXETINE",
    "BICALUMUTIDE": "BICALUTAMIDE", "HYSOCINE": "HYOSCINE", "CIPROPRXACIN": "CIPROFLOXACIN",
    "UNDERCANOATE": "UNDECANOATE", "DAPOXITINE": "DAPOXETINE", "DEPOXETINE": "DAPOXETINE",
    "DAPXETINE": "DAPOXETINE", "ARBUTAMINE": "ARBUTIN (arbutamine is a different, cardiac drug)",
    "ENALAPRI": "ENALAPRIL", "CARBAMEZAPINE": "CARBAMAZEPINE", "FAMCYCLOVIR": "FAMCICLOVIR",
    "IVABRIDINE": "IVABRADINE", "LIVETIRACETAM": "LEVETIRACETAM", "LIGNOCAIN": "LIGNOCAINE (LIDOCAINE)",
    "HYROQUINONE": "HYDROQUINONE", "MEBENDAZOL": "MEBENDAZOLE", "METHOCARMABOL": "METHOCARBAMOL",
    "SULFADIZINE": "SULFADIAZINE", "TAZOBACTUM": "TAZOBACTAM", "CYNOCOBALAMIN": "CYANOCOBALAMIN",
    "CYANOCOBALAMINE": "CYANOCOBALAMIN", "LEVONOGESTREL": "LEVONORGESTREL",
    "DIETHLAMINE": "DIETHYLAMINE", "FOLICLE": "FOLLICLE", "HYDROCLORIDE": "HYDROCHLORIDE",
    "GLARGIN": "GLARGINE", "FURATE": "FUROATE", "PRAMIPEXOL": "PRAMIPEXOLE", "CETRIZINE": "CETIRIZINE",
    "SULPHMETHOXAZOLE": "SULFAMETHOXAZOLE", "MIC": "MIX", "COUMPOUND": "COMPOUND",
    "OPTHALMIC": "OPHTHALMIC", "OPTH": "OPHTHALMIC", "HCLAND": "HCL AND", "ANDMETFORMIN": "AND METFORMIN",
    "TRENBOL": "TRENBOLONE",
}

# ingredient synonyms -> one normalised (INN-style) name
SYNONYMS = {
    "AMOXYCILLIN": "AMOXICILLIN", "AMOXYCILIN": "AMOXICILLIN", "AMOXICILIN": "AMOXICILLIN",
    "POTASSIUM CLAVULANATE": "CLAVULANIC ACID", "POTASSIUM CLAVULANTE": "CLAVULANIC ACID",
    "CLAVULANATE": "CLAVULANIC ACID", "ACYCLOVIR": "ACICLOVIR", "FRUSEMIDE": "FUROSEMIDE",
    "LIGNOCAINE": "LIDOCAINE", "LIGNOCAIN": "LIDOCAINE", "THYROXINE": "LEVOTHYROXINE",
    "CLOMIPHENE": "CLOMIFENE", "BECLOMETHASONE": "BECLOMETASONE", "CHLORTHALIDONE": "CHLORTALIDONE",
    "METHANDIENONE": "METANDIENONE", "CYCLOSPORINE": "CICLOSPORIN", "MESALAMINE": "MESALAZINE",
    "SULPHAMETHOXAZOLE": "SULFAMETHOXAZOLE", "SULPHMETHOXAZOLE": "SULFAMETHOXAZOLE",
    "CO-TRIMETHOPRIM": "TRIMETHOPRIM", "CYANOCOBALAMINE": "CYANOCOBALAMIN", "CYNOCOBALAMIN": "CYANOCOBALAMIN",
    "MECOBALAMIN": "METHYLCOBALAMIN", "NIMUSULIDE": "NIMESULIDE", "CHLOROMYCETIN": "CHLORAMPHENICOL",
    "HUMAN CHORIONIC GONADOTROPHIN": "CHORIONIC GONADOTROPIN", "CHORIONIC GONADOTROPHIN": "CHORIONIC GONADOTROPIN",
    "MENOTROPHIN": "MENOTROPIN", "FOLICLE STIMULATING HORMONE": "FOLLITROPIN",
    "HUMAN GROWTH HORMONE": "SOMATROPIN", "NITROGLYCERIN": "GLYCERYL TRINITRATE",
    "VITAMIN D3": "CHOLECALCIFEROL", "VITAMIN C": "ASCORBIC ACID", "ASCORBIC ACID VITAMIN C": "ASCORBIC ACID",
    "INSULIN GLARGIN": "INSULIN GLARGINE", "TRANEXAMIC": "TRANEXAMIC ACID",
    "URSODEOXYCHOLIC": "URSODEOXYCHOLIC ACID", "BEMPEDOIC": "BEMPEDOIC ACID", "ENALAPRI": "ENALAPRIL",
    "LEVONOGESTREL": "LEVONORGESTREL", "PRAMIPEXOL": "PRAMIPEXOLE", "EMTRICIABIN": "EMTRICITABINE",
    "SODIUM ALENDRONATE": "ALENDRONATE", "ALENDRONATE SODIUM": "ALENDRONATE", "MEBENDAZOL": "MEBENDAZOLE",
    "METANDIENONE": "METANDIENONE", "CETRIZINE": "CETIRIZINE", "N-ACETYLCYSTEINE": "ACETYLCYSTEINE",
    "DIVALPROEX SODIUM ER": "DIVALPROEX", "L CARNITINE": "L-CARNITINE", "LEVOCARNITINE": "L-CARNITINE",
    "SILVER SULFADIZINE": "SILVER SULFADIAZINE", "ACICLOVIR CREAM": "ACICLOVIR",
}
# misspellings also normalise to the INN
for _bad, _good in MISSPELLINGS.items():
    if _bad.isalpha() and " " not in _good and _bad not in ("MIC", "OPTH", "FURATE", "FOLICLE",
                                                             "HYDROCLORIDE", "BITARTATE", "DIETHLAMINE",
                                                             "COUMPOUND", "TRENBOL"):
        SYNONYMS.setdefault(_bad, _good)

FORM_WORDS = [
    "OPHTHALMIC SOLUTION", "TOPICAL SOLUTION", "GASTRO -RESISTANT", "GASTRO RESISTANT", "PROLONGED RELEASE",
    "SUSTAINED RELEASE", "EXTENDED RELEASE", "CONTROLLED-RELEASE TABLETS OF", "INJECTION SUSPENSION",
    "ORAL POWDER", "INJECTION", "SUSPENSION", "OINTMENT", "OPHTHALMIC", "OPTHALMIC", "OPTH", "NEBULISER",
    "DISPERSIBLE", "CHEWABLE", "SUBLINGUAL", "EFFERVESCENT", "EXTENDED", "SUSTAINED", "RETARD",
    "LYOPHILIZED", "MICRONISED", "MICRONIZED", "SYRUP", "CREAM", "TABLETS", "TABLET", "TAB", "GEL", "INJ",
    "SOFT", "CAP", "SOLUTION",
]
SALTS = ["HYDROCHLORIDE", "HYDROCLORIDE", "DIHYDROCHLORIDE", "DIHCL", "HCL", "HCI", "MALEATE", "CITRATE",
         "MESYLATE", "MESILATE", "BESILATE", "OXALATE", "TROMETHAMINE", "TROMETAMOL", "TRIHYDRATE",
         "SODIUM", "POTASSIUM", "CALCIUM", "MAGNESIUM", "SULPHATE", "SULFATE"]
SALT_BASES = {"SODIUM", "POTASSIUM", "CALCIUM", "MAGNESIUM", "ZINC", "FERROUS", "SILVER", "IRON",
              "BENZYL", "ALUMINIUM", "SELENIUM", "BENZOYL", "SALICYLIC", "AZELAIC", "KOJIC", "GLYCOLIC"}

NOT_INGREDIENT = {"HERBAL", "HIMALAYA", "FACE SERUM", "HAIR SUPPLEMENTS", "FABISPRAY", "SKIN LIGHTENING CREAM",
                  "DIETARY SUPPLEMENT", "HERBAL TABLETS", "HERBAL CAPSULES"}

DEVICE_GENERIC = re.compile(r"\b(SYRING|SYRINGE|NEEDLE|PREGNANCY TEST KIT|FIRST AID KIT|BLOOD PRESSURE|"
                            r"OXYMETER|OXIMETER|FACE MASK|ISOPROPYL ALCOHOL|LUBRICATING JELLY|MOUTHWASH)\b")
DEVICE_NAME = re.compile(r"\b(SYRINGE|SYRING|NEEDLE|NEEDL|KIT SET|GLUCOSE MONITOR|NEBULIZER KIT|I\.V\. SET|"
                         r"OMRON|PULS OXY|FACE MASK|PREP PADS|HUMA PEN|HICK|HICKSCREPE|TRIAL PACK)\b")

OTC_GENERIC = re.compile(
    r"\b(HERBAL|HIMALAYA|SHILAJIT|ASHWAGAND|MULTIVITAMIN|VITAMIN|CHOLECALCIFEROL|ZINC|CALCIUM|OMEGA-3|"
    r"GLUTATHIONE|BIOTIN|HAIR SUPPLEMENTS|FACE SERUM|SKIN LIGHTENING|MOUTHWASH|LUBRICATING|DIETARY|PROBIOTIC|"
    r"L-CARNITINE|L CARNITINE|LEVOCARNITINE|L-THEANINE|QUERCETIN|PLACENTA|KOJIC|ORAL REHYDRATION|"
    r"METHYLCOBALAMIN|CYANOCOBALAMIN|CYANOCOBALAMINE|THIAMINE|ASCORBIC|SACCHAROMYCES|STERILE WATER|"
    r"GLYCERIN SUPPOSITORIES|ISOPROPYL|ALPHA LIPOIC|FERROUS|ELEMENTAL IRON|NATTOKINASE|CASTOR)\b")
OTC_NAME = re.compile(r"\b(CETAPHIL|PHYSIOGEL|CASTOR OIL|SHAMPOO|INTALON|ASHWAGANDHA|NASOMIST|"
                      r"DIVYA|LIV 52|LIVERHEAL|BONTRESS|AYUR)\b")

PED_INGR = re.compile(r"TRENBOLONE|TRENBOL|BOLDENONE|DROSTANOLONE|METHENOLONE|NANDROLONE|STANOZOLOL|"
                      r"OXANDROLONE|OXYMETHOLONE|OXYMETHALONE|OXYMETHELONE|METHANDIENONE|METANDIENONE|"
                      r"MESTEROLONE|MESTERONUM|FLUOXYMESTERONE|CHLORODEHYDROMETHYLTESTOSTERONE|CLENBUTEROL|"
                      r"MK-677|IBUTAMOREN|CUT MIX")
PED_NAME = re.compile(r"\bMK-677\b")
CONTROLLED = re.compile(r"TAPENTADOL|ARMODAFINIL|MODAFINIL|ESZOPICLONE|ZOPICLONE|ZALEPLON|CARISOPRODOL|TIANEPTINE")
VET_INGR = re.compile(r"FENBENDAZOLE|FEBENDAZOLE|OCLACITINIB|FIROCOXIB|SELAMECTIN|PIMOBENDAN")
VET_NAME = re.compile(r"\bVET\b|KOMEC|PETMECTIN|HITEK|PANACUR|EAZYPET|RADICATE|KETOCHLOR|BOLUS|PETMEDIN|OCLAPET|"
                      r"APPOQUIL|FIROTAIL|DICHRYSTICIN")
MAINSTREAM_TESTO = re.compile(r"\b(SUSTANON|TESTOVIRON|CERNOS|AQUAVIRON|ANDROFAST)\b")

TOPICAL_FORMS = {"CREAM", "OINTMENT", "GEL", "LOTION", "POWDER", "SOLUTION", "SPRAY", "DROPS", "OTHER"}
SOLID_FORMS = {"TABLET", "CAPSULE"}


def norm_ws(s):
    return " ".join(s.split())


def norm_name(s):
    return norm_ws(s.upper()).rstrip(". ")


def norm_generic_text(s):
    s = norm_ws(s.upper())
    s = re.sub(r"\s*([+&,/])\s*", r" \1 ", s)
    s = re.sub(r"\s*-\s*(?=[+&,]|$)", " ", s)
    return norm_ws(s).strip(" ,.-")


def loose_name(s):
    s = re.sub(r"[^A-Z0-9.%]+", " ", s.upper())
    s = re.sub(r"\b(TAB|TABS|TABLET|TABLETS|CAP|CAPS|CAPSULE)\b", " ", s)
    return re.sub(r"\s+", "", s).rstrip(".")


# ---------------------------------------------------------------- parsing
UNIT_RE = re.compile(r"(?<![\d.A-Z])(\d+(?:\.\d+)?(?:\s*/\s*\d+(?:\.\d+)?)?)\s*(MCG|MG|IU|%|GM|G|ML|MD|K|L\s*IU)(?![A-Z])")
BARE_NUM_RE = re.compile(r"(?<![\d.])\b(\d+(?:\.\d+)?)\b(?!\s*(?:MCG|MG|IU|%|GM|G|ML|MD|K|L\s*IU|\*|CM|NO|DAYS|CAP|TAB|S\b|X\b))")


def tokens(name):
    return set(re.findall(r"[A-Z]+", name.upper()))


def parse_form(name, generic):
    """Return (form, extra_flags)."""
    n = name.upper()
    g = generic.upper()
    t = tokens(n)
    flags = []
    if DEVICE_GENERIC.search(g) or DEVICE_NAME.search(n):
        return "DEVICE", flags
    if "ORAL JELLY" in n or ("JELLY" in t and re.search(r"SILDENAFIL|TADALAFIL|VARDENAFIL", g)) \
            or ("SHOT" in t and "GEL" in t):
        flags.append(("form_ambiguous", "low", "oral jelly / oral gel ED product -> OTHER"))
        return "OTHER", flags
    if "ODS" in t:
        flags.append(("form_ambiguous", "low", "ODS = oral dissolving strip -> OTHER"))
        return "OTHER", flags
    if t & {"ROTACAPS"}:
        return "CAPSULE", flags
    if t & {"INHALER", "INHR", "CIPHALER", "TURBUHALER", "ACCUHALER", "ELLIPTA", "INHALANT"}:
        return "INHALER", flags
    if t & {"RES", "RESP", "RESPULES", "RESPIRATOR"}:
        return "SOLUTION", flags
    if "SPRAY" in t or re.search(r"\bNAS(AL)?\b", n):
        return "SPRAY", flags
    if t & {"DROP", "DROPS"}:
        return "DROPS", flags
    if "EYE" in t and t & {"OINT", "OINTMENT"}:
        return "OINTMENT", flags
    if t & {"INJ", "INJECTION", "AMP", "AMPS", "VIAL", "FLEXPEN", "SOLOSTAR", "NORDIFLEX", "CART",
            "CARTRIDGE", "PFS", "KWIK", "IV"} or re.search(r"\bPEN\b", n) or "INJ" in n:
        return "INJECTION", flags
    if t & {"CREAM"}:
        return "CREAM", flags
    if t & {"OINT", "OINTMENT", "ONT", "OIT"}:
        return "OINTMENT", flags
    if t & {"JELLY", "GEL"}:
        return "GEL", flags
    if t & {"LOTION"}:
        return "LOTION", flags
    if t & {"SHAMPOO", "SERUM", "SOAP", "SUPPOSITORY", "SUPPO", "SUPPOOSIT", "PASTE", "PAD", "PADS",
            "KIT", "COMBIPACK", "BOX", "OIL"}:
        return "OTHER", flags
    if t & {"SYP", "SYRUP", "SYRP", "SYR"}:
        return "SYRUP", flags
    if t & {"SUSP", "SUSPENSION", "SUS", "SYS", "GRANULES"} and "DRY" not in t:
        return "SUSPENSION", flags
    if "DRY" in t:
        return "SUSPENSION", flags
    if t & {"SOL", "SOLUTION", "LIQ", "VISCOUS", "MOUTHWASH"}:
        return "SOLUTION", flags
    if t & {"SACHET"}:
        return "SACHET", flags
    if t & {"POWDER"}:
        return "POWDER", flags
    if t & {"CAP", "CAPS", "CAPSULE", "SOFTGEL"}:
        return "CAPSULE", flags
    if t & {"TAB", "TABS", "TABLET", "TABLETS", "MELTAB", "PILL"}:
        return "TABLET", flags
    if t & {"DT", "MD"}:
        return "TABLET", flags
    # inference
    if re.search(r"\d\s*%", n) and re.search(r"\d\s*ML\b", n):
        flags.append(("form_inferred", "low", "percent strength + ML pack -> topical SOLUTION inferred"))
        return "SOLUTION", flags
    if "ML" in n and re.search(r"\d\s*ML\b", n):
        flags.append(("form_inferred", "low", "ML volume with no form word -> SOLUTION inferred"))
        return "SOLUTION", flags
    if re.search(r"\d\s*(MG|MCG)\b", n) or t & {"SR", "ER", "XL", "XR", "CR", "OD", "PR", "SX"}:
        flags.append(("form_inferred", "low", "no form word; oral strength/release code -> TABLET inferred"))
        return "TABLET", flags
    return None, flags


def parse_strength_pack(name, form):
    n = name.upper()
    strength, pack, packs = None, None, []
    for m in UNIT_RE.finditer(n):
        num, unit = re.sub(r"\s+", "", m.group(1)), re.sub(r"\s+", " ", m.group(2))
        val = num + unit if unit != "L IU" else num + "L IU"
        if unit in ("MCG", "MG", "IU", "%", "K", "L IU"):
            if strength is None:
                strength = val
        elif unit in ("GM", "G"):
            if form in TOPICAL_FORMS or form is None or form in ("GEL",):
                packs.append(num + "GM")
            elif strength is None and float(num.split("/")[0]) <= 5:
                strength = num + "GM"
            else:
                packs.append(num + "GM")
        elif unit == "ML":
            packs.append(num + "ML")
        elif unit == "MD":
            packs.append(num + "MD")
    if packs:
        pack = packs[-1]
    m = re.search(r"\b(1\*10|21CAP|10TAB|1BOX)\b", n)
    if m and not pack:
        pack = m.group(1)
    # "MG/ML" style e.g. 600MG/4ML -> strength 600MG, pack 4ML (already handled)
    return strength, pack


def split_ingredients(generic):
    g = generic.upper()
    g = g.replace("ANDMETFORMIN", "AND METFORMIN").replace("HCLAND", "HCL AND")
    g = re.sub(r"\([^)]*\)", " ", g)
    g = re.sub(r"\d+(\.\d+)?\s*MG\s*/\s*ML\)?", " ", g).replace(")", " ")
    g = re.sub(r"^DARDFLAM\s*-", "", g)
    parts = re.split(r"\s*(?:\+|&|,|/|\bAND\b|\bWITH\b|\bPLUS\b)\s*", g)
    out = []
    for p in parts:
        p = norm_ws(p)
        p = re.sub(r"[-\s]*\d+(\.\d+)?\s*(MG|GM|G|MCG|IU|%|ML)\b.*$", "", p)  # strengths
        p = re.sub(r"^\d+(\.\d+)?\s*", "", p)
        p = p.strip(" -.,")
        changed = True
        while changed and p:
            changed = False
            for w in FORM_WORDS:
                if p.endswith(" " + w) or p == w:
                    p = p[: -len(w)].strip(" -")
                    changed = True
        if not p:
            continue
        words = p.split()
        while len(words) >= 2 and words[-1] in SALTS and words[0] not in SALT_BASES:
            words = words[:-1]
        p = " ".join(words)
        if p in ("HCL", "HCI", "COMPOUND", "MIX", "MIC COMPOUND", "COMPONENT", "SUBLINGUAL"):
            continue
        p = SYNONYMS.get(p, p)
        # single-word synonyms inside multiword (e.g. AMOXYCILLIN TRIHYDRATE handled above)
        p = " ".join(SYNONYMS.get(w, w) if w in SYNONYMS and len(words) > 1 else w for w in p.split()) \
            if len(p.split()) > 1 else p
        if p and p not in out:
            out.append(p)
    return out


# ---------------------------------------------------------------- regulatory
def regulatory_group(row, ingr, form, testo_ped_stems):
    n = row["name"].upper()
    g = row["generic"].upper()
    allg = g + " " + " ".join(ingr)
    if not g.strip() and not DEVICE_NAME.search(n) and not OTC_NAME.search(n):
        if PED_NAME.search(n):
            return "anabolic_steroid_or_ped", None
        return "unknown", "no generic; group unknown"
    if VET_NAME.search(n) or VET_INGR.search(allg):
        return "veterinary", None
    if "IVERMECTIN" in allg and ("50ML" in n.replace(" ", "") or "PASTE" in n):
        return "veterinary", None
    if PED_INGR.search(allg) or PED_NAME.search(n):
        return "anabolic_steroid_or_ped", None
    if "TESTOSTERONE" in allg:
        if MAINSTREAM_TESTO.search(n):
            return "approved_rx", None
        if re.search(r"MIX|COMPOUND|MIC COMPOUND", g) or re.search(r"\b(MAXX|PRO|MIX)\b", n) \
                or re.search(r"\b(10|20)\s*ML\b", n):
            return "anabolic_steroid_or_ped", None
        if form in ("CAPSULE", "TABLET", "GEL", "CREAM"):
            return "approved_rx", None
        stem = n.split()[0].split("-")[0]
        if stem in testo_ped_stems:
            return "anabolic_steroid_or_ped", None
        return "unknown", "testosterone injection from a non-mainstream brand without multi-dose/blend markers"
    if CONTROLLED.search(allg):
        return "controlled_abuse_prone", None
    if form == "DEVICE" or DEVICE_GENERIC.search(g) or DEVICE_NAME.search(n):
        return "otc_or_device", None
    if OTC_NAME.search(n) and not re.search(r"KETOCONAZOLE|CLOBETASOL|SELENIUM|IVERMECTIN", allg):
        return "otc_or_device", None
    if ingr and all(OTC_GENERIC.search(i) for i in ingr):
        return "otc_or_device", None
    if g.strip().upper() in NOT_INGREDIENT or OTC_GENERIC.search(g) and not ingr:
        return "otc_or_device", None
    return "approved_rx", None


# ---------------------------------------------------------------- main
def main():
    rows = json.load(open(SRC, encoding="utf-8"))
    manual = json.load(open(MANUAL, encoding="utf-8"))
    db = load_db()
    by_sr = {r["sr"]: r for r in rows}

    flags = defaultdict(list)

    def add(sr, code, sev, detail, url=None):
        flags[sr].append({"code": code, "severity": sev, "detail": detail, "evidence_url": url})

    parsed, ingr_map = {}, {}
    pov = manual.get("parsed_overrides", {})
    for r in rows:
        form, fflags = parse_form(r["name"], r["generic"])
        strength, pack = parse_strength_pack(r["name"], form)
        parsed[r["sr"]] = {"strength": strength, "dosage_form": form, "pack": pack}
        if str(r["sr"]) in pov:
            ov = dict(pov[str(r["sr"])])
            reason = ov.pop("reason", "")
            parsed[r["sr"]].update(ov)
            fflags = [f for f in fflags if f[0] != "form_inferred"]
            add(r["sr"], "parse_override", "low", f"parsed fields set manually ({reason})")
        for code, sev, det in fflags:
            add(r["sr"], code, sev, det)
        ingr_map[r["sr"]] = split_ingredients(r["generic"])

    # testosterone brand stems that have multi-dose / blend variants
    testo_ped_stems = set()
    for r in rows:
        n, g = r["name"].upper(), r["generic"].upper()
        if "TESTOSTERONE" in g and (re.search(r"\b(10|20)\s*ML\b", n) or re.search(r"\b(MAXX|PRO|MIX)\b", n)
                                    or re.search(r"MIX|COMPOUND", g)):
            testo_ped_stems.add(n.split()[0].split("-")[0])
    testo_ped_stems -= {"SUSTANON", "TESTOVIRON", "CERNOS", "AQUAVIRON"}

    # ---- duplicates
    by_name = defaultdict(list)
    by_loose = defaultdict(list)
    for r in rows:
        by_name[norm_name(r["name"])].append(r)
        by_loose[loose_name(r["name"])].append(r)
    dup_groups = []
    for key, grp in by_name.items():
        if len(grp) < 2:
            continue
        dup_groups.append((key, [g["sr"] for g in grp]))
        for r in grp:
            others = [o for o in grp if o["sr"] != r["sr"]]
            exact = [o["sr"] for o in others if norm_generic_text(o["generic"]) == norm_generic_text(r["generic"])
                     and float(o["price"]) == float(r["price"])]
            if exact:
                add(r["sr"], "exact_duplicate", "medium", f"same name+generic+price as sr {exact}")
            dp = sorted({o["price"] for o in others if float(o["price"]) != float(r["price"])}, key=float)
            if dp:
                add(r["sr"], "same_name_different_price", "medium",
                    f"price {r['price']} vs {dp} (sr {[o['sr'] for o in others if float(o['price']) != float(r['price'])]})")
            dg = [o for o in others if split_ingredients(o["generic"]) != ingr_map[r["sr"]]]
            if dg:
                add(r["sr"], "same_name_different_generic", "high",
                    "; ".join(f"sr {o['sr']}: '{o['generic']}'" for o in dg) + f" vs this '{r['generic']}'")
    near_groups = []
    for key, grp in by_loose.items():
        names = {norm_name(g["name"]) for g in grp}
        if len(grp) >= 2 and len(names) >= 2:
            near_groups.append([g["sr"] for g in grp])
            for r in grp:
                others = [o["sr"] for o in grp if norm_name(o["name"]) != norm_name(r["name"])]
                if others:
                    add(r["sr"], "near_duplicate_name", "low",
                        f"same product name apart from punctuation/form word as sr {others}")

    # ---- per-row checks
    for r in rows:
        sr, name, gen = r["sr"], r["name"], r["generic"]
        p = parsed[sr]
        ingr = ingr_map[sr]
        if "  " in name or name != name.strip():
            add(sr, "whitespace_or_case_variant", "low", f"name has extra whitespace: {name!r}; DB stores cleaned name")
        if name != name.upper():
            add(sr, "whitespace_or_case_variant", "low", f"name not upper-case: {name!r}")
        if gen and ("  " in gen or re.search(r"-\s*(\+|$)|,\s*,|,\s*$|-\s*,", gen)):
            add(sr, "generic_formatting", "low", f"generic has stray spaces/dashes/commas: {gen!r}")
        # misspellings
        seen = set()
        for w in re.findall(r"[A-Z]+", gen.upper()):
            if w in MISSPELLINGS and w not in seen and w not in ("MIC", "HCLAND", "ANDMETFORMIN"):
                seen.add(w)
                add(sr, "generic_misspelling", "low", f"'{w}' -> {MISSPELLINGS[w]}")
        if re.search(r"\bMIC COMPOUND\b", gen.upper()):
            add(sr, "generic_misspelling", "low", "'MIC COMPOUND' -> MIX COMPOUND (testosterone blend)")
        # placeholder
        if len(re.sub(r"[^A-Z]", "", name.upper())) <= 1:
            add(sr, "placeholder_name", "high",
                f"name {name!r} is not a product name (generic {gen!r}, price {r['price']})")
        # missing generic
        if not gen.strip():
            add(sr, "missing_generic", "high", "generic column blank")
        elif norm_ws(gen.upper()) == norm_ws(name.upper()):
            add(sr, "missing_generic", "medium", "generic repeats the product name")
        # form vs generic conflicts
        gu = gen.upper()
        if p["dosage_form"] in SOLID_FORMS | {"INJECTION"} and re.search(r"\b(GEL|CREAM|OINTMENT)\b", gu):
            add(sr, "form_generic_conflict", "high", f"{p['dosage_form']} but generic says {gen!r}")
        if p["dosage_form"] in ("CREAM", "OINTMENT", "GEL", "LOTION") and re.search(r"\bTAB\b|INJ\b", gu):
            add(sr, "form_generic_conflict", "high", f"{p['dosage_form']} but generic says {gen!r}")
        if p["dosage_form"] is None:
            add(sr, "form_unknown", "low", "no dosage form inferable from name")
        # strength unit suspicion
        st = p["strength"] or ""
        allg = gu + " " + " ".join(ingr)
        if re.search(r"THYROXINE|LIOTHYRONINE", allg) and st.endswith("MG") and not st.startswith("0."):
            add(sr, "strength_unit_suspect", "medium", f"{st} for a thyroid hormone — almost certainly MCG")
        if p["dosage_form"] in ("INHALER",) or re.search(r"ROTACAPS|RES\b|RESP", name.upper()):
            if st.endswith("MG") and float(re.sub(r"[^\d.]", "", st.split("/")[0]) or 0) >= 50:
                add(sr, "strength_unit_suspect", "medium", f"{st} for an inhaled product — MCG expected")
        if "CLENBUTEROL" in allg and st.endswith("MG"):
            add(sr, "strength_unit_suspect", "medium", f"{st} clenbuterol — MCG expected")
        if re.search(r"METHYLCOBALAMIN|CYANOCOBALAMIN", allg) and st.endswith("MG") \
                and float(re.sub(r"[^\d.]", "", st) or 0) >= 500:
            add(sr, "strength_unit_suspect", "medium", f"{st} vitamin B12 — MCG expected")
        if re.search(r"GONADOTROPIN|FOLLITROPIN|MENOTROPIN|UROFOLLITROPIN", allg) and st.endswith("MG"):
            add(sr, "strength_unit_suspect", "medium", f"{st} gonadotrophin — IU expected")
        if "LIOTHYRONINE" in allg and st.endswith("MG"):
            pass  # covered above
        # strength
        group_hint = None
        is_drug = bool(gen.strip()) and p["dosage_form"] != "DEVICE" and gu.strip() not in NOT_INGREDIENT \
            and not OTC_GENERIC.search(gu)
        if not p["strength"] and is_drug:
            nm = re.sub(r"\b(1\*10|21CAP|10TAB|1BOX)\b", " ", name.upper())
            nm = UNIT_RE.sub(" ", nm)
            bare = [b for b in BARE_NUM_RE.findall(nm)]
            if bare:
                add(sr, "strength_unit_missing", "low", f"bare number(s) {bare} in name without a unit")
            else:
                add(sr, "missing_strength", "low", "no strength in product name")
        # combination
        if len(ingr) >= 2:
            add(sr, "combination_product", "low", f"{len(ingr)} ingredients: {ingr}")

    # ---- price outliers (same ingredients + form + strength, group >= 3)
    grp = defaultdict(list)
    for r in rows:
        p = parsed[r["sr"]]
        if ingr_map[r["sr"]] and p["strength"]:
            grp[(tuple(sorted(ingr_map[r["sr"]])), p["dosage_form"], p["strength"])].append(r)
    for key, members in grp.items():
        if len(members) < 3:
            continue
        med = statistics.median(float(m["price"]) for m in members)
        for m in members:
            pr = float(m["price"])
            if med > 0 and pr > 15 * med and pr - med > 50:
                add(m["sr"], "price_outlier", "low",
                    f"price {m['price']} vs median {med:g} of {len(members)} rows with same ingredients/form/strength "
                    f"{list(key[0])} {key[1]} {key[2]} — may be a legitimate originator/brand premium")
            elif med > 0 and pr < med / 15:
                add(m["sr"], "price_outlier", "medium",
                    f"price {m['price']} vs median {med:g} of {len(members)} rows with same ingredients/form/strength "
                    f"{list(key[0])} {key[1]} {key[2]}")
    # ---- price outliers (same ingredients + form ignoring strength, very large deviation)
    grp2 = defaultdict(list)
    for r in rows:
        if ingr_map[r["sr"]]:
            grp2[(tuple(sorted(ingr_map[r["sr"]])), parsed[r["sr"]]["dosage_form"])].append(r)
    for key, members in grp2.items():
        if len(members) < 3:
            continue
        med = statistics.median(float(m["price"]) for m in members)
        for m in members:
            pr = float(m["price"])
            if pr > 100 * med and pr > 1000 and not any(f["code"] == "price_outlier" for f in flags[m["sr"]]):
                add(m["sr"], "price_outlier", "high",
                    f"price {m['price']} is >100x the median {med:g} of {len(members)} {list(key[0])} {key[1]} rows")

    # ---- DB comparison
    in_db_count = 0
    db_extra = []
    for r in rows:
        sku = "IMP-%04d" % r["sr"]
        d = db.get(sku)
        if not d:
            continue
        in_db_count += 1
        diffs = []
        if d["name"] != r["name"]:
            diffs.append(f"name DB {d['name']!r} vs source {r['name']!r}")
        if norm_ws(d["generic"].upper()) != norm_ws(r["generic"].upper()):
            diffs.append(f"generic DB {d['generic']!r} vs source {r['generic']!r}")
        if int(d["price_paise"]) != round(float(r["price"]) * 100):
            diffs.append(f"price DB {d['price_paise']} paise vs source {r['price']}")
        if diffs:
            add(r["sr"], "db_mismatch", "medium", "; ".join(diffs))
    src_skus = {"IMP-%04d" % r["sr"] for r in rows}
    db_extra = sorted(set(db) - src_skus)

    # ---- manual flags
    for sr_s, fl in manual.get("flags", {}).items():
        sr = int(sr_s)
        for f in fl:
            # manual missing_generic replaces mechanical duplicate of same code
            if f["code"] in ("missing_generic", "price_outlier"):
                flags[sr] = [x for x in flags[sr] if x["code"] != f["code"]]
            flags[sr].append(dict(f))

    # ---- assemble
    overrides = manual.get("regulatory_overrides", {})
    out = []
    for r in rows:
        sr = r["sr"]
        p = parsed[sr]
        grp_name, why = regulatory_group(r, ingr_map[sr], p["dosage_form"], testo_ped_stems)
        if str(sr) in overrides:
            grp_name, why = overrides[str(sr)]["group"], None
            add(sr, "regulatory_review", "low", "regulatory group set manually: " + overrides[str(sr)]["reason"])
        elif why:
            add(sr, "regulatory_review", "low", why)
        out.append({
            "sr": sr, "sku": "IMP-%04d" % sr, "name": r["name"], "generic": r["generic"], "price": r["price"],
            "in_db": ("IMP-%04d" % sr) in db,
            "parsed": p,
            "ingredients_normalised": ingr_map[sr],
            "flags": flags[sr],
            "regulatory_group": grp_name,
        })
    assert len(out) == 3300 and len({o["sr"] for o in out}) == 3300
    with open(OUT_ROWS, "w", encoding="utf-8") as fh:
        json.dump(out, fh, indent=1, ensure_ascii=False)

    write_summary(out, db, dup_groups, near_groups, in_db_count, db_extra)
    print(f"wrote {OUT_ROWS} ({len(out)} rows, {in_db_count} in DB) and {OUT_MD}")


def write_summary(out, db, dup_groups, near_groups, in_db_count, db_extra):
    from collections import Counter
    sev_order = {"high": 0, "medium": 1, "low": 2}
    code_rows = Counter()
    code_sev = defaultdict(Counter)
    for o in out:
        for c in {f["code"] for f in o["flags"]}:
            code_rows[c] += 1
        for f in o["flags"]:
            code_sev[f["code"]][f["severity"]] += 1
    by_sr = {o["sr"]: o for o in out}
    L = []
    L.append("# T1 audit — supplier list 2026A vs DB\n")
    L.append("Generated by `data/tools/audit_source.py` (mechanical checks) + "
             "`enrichment/audit/manual_findings.json` (auditor judgement). Full per-row output: `audit_rows.json`.\n")
    L.append(f"- Source rows: **{len(out)}** (SR 1–3300, all accounted for)")
    L.append(f"- In DB (sku IMP-xxxx): **{in_db_count}**; not imported: **{len(out) - in_db_count}**")
    L.append(f"- DB IMP- skus not in source: {db_extra if db_extra else 'none'}")
    L.append(f"- Rows with ≥1 flag: {sum(1 for o in out if o['flags'])}; rows with a high flag: "
             f"{sum(1 for o in out if any(f['severity']=='high' for f in o['flags']))}\n")

    L.append("## Counts per flag (rows flagged; severity split counts flag instances)\n")
    L.append("| flag | rows | high | medium | low |")
    L.append("|---|---|---|---|---|")
    for c, n in sorted(code_rows.items(), key=lambda x: -x[1]):
        s = code_sev[c]
        L.append(f"| `{c}` | {n} | {s['high']} | {s['medium']} | {s['low']} |")
    L.append("")

    L.append("## Regulatory groups (guidance only, not a legal opinion)\n")
    rg = Counter(o["regulatory_group"] for o in out)
    rg_db = Counter(o["regulatory_group"] for o in out if o["in_db"])
    L.append("| group | all rows | already in DB | not yet imported |")
    L.append("|---|---|---|---|")
    for g, n in rg.most_common():
        L.append(f"| {g} | {n} | {rg_db[g]} | {n - rg_db[g]} |")
    L.append("")
    for g in ("anabolic_steroid_or_ped", "veterinary", "controlled_abuse_prone"):
        act = [o for o in out if o["regulatory_group"] == g and o["in_db"]
               and db["IMP-%04d" % o["sr"]]["active"] == "t"]
        L.append(f"- **{g}** rows already in DB and `is_active = true`: {len(act)}"
                 + (f" — sr {[o['sr'] for o in act]}" if act else ""))
    unk = [o for o in out if o["regulatory_group"] == "unknown"]
    L.append(f"- **unknown** ({len(unk)}): " + ", ".join(f"{o['sr']} {o['name']}" for o in unk))
    L.append("")

    swg = [o for o in out if any(f["code"] == "suspected_wrong_generic" for f in o["flags"])]
    L.append(f"## suspected_wrong_generic — {len(swg)} rows\n")
    L.append("`confirmed` = an official manufacturer page was checked; everything else is `suspected` "
             "(inference from name/strength/form/sibling rows). DB columns show what the enrichment run "
             "already wrote from the (wrong) generic — these need quarantine in T6.\n")
    L.append("| sr | name | listed generic | sev | status / detail | evidence | in DB | DB enrichment / category |")
    L.append("|---|---|---|---|---|---|---|---|")
    swg.sort(key=lambda o: (0 if any(f["code"] == "suspected_wrong_generic" and f["detail"].startswith("confirmed")
                                     for f in o["flags"]) else 1,
                            min(sev_order[f["severity"]] for f in o["flags"] if f["code"] == "suspected_wrong_generic"),
                            o["sr"]))
    for o in swg:
        f = next(f for f in o["flags"] if f["code"] == "suspected_wrong_generic")
        d = db.get(o["sku"])
        dbi = f"{d['enrich_status']} / {d['category']}" if d else "—"
        L.append(f"| {o['sr']} | {o['name']} | {o['generic']} | {f['severity']} | {f['detail']} | "
                 f"{f['evidence_url'] or '—'} | {'yes' if o['in_db'] else 'no'} | {dbi} |")
    L.append("")

    other_manual = ["incomplete_generic", "strength_mismatch", "form_generic_conflict", "placeholder_name",
                    "price_outlier", "db_mismatch", "missing_generic", "generic_truncated", "needs_pharmacist_check"]
    for code in other_manual:
        rs = [o for o in out if any(f["code"] == code for f in o["flags"])]
        if not rs:
            continue
        L.append(f"## {code} — {len(rs)} rows\n")
        for o in rs:
            for f in o["flags"]:
                if f["code"] == code:
                    L.append(f"- sr {o['sr']} `{o['name']}` (generic `{o['generic']}`, price {o['price']}, "
                             f"in DB: {'yes' if o['in_db'] else 'no'}) — {f['detail']}"
                             + (f" [{f['evidence_url']}]" if f["evidence_url"] else ""))
        L.append("")

    L.append(f"## Duplicate groups (same normalised name) — {len(dup_groups)} groups\n")
    for key, srs in sorted(dup_groups, key=lambda x: x[1][0]):
        parts = []
        for s in srs:
            o = by_sr[s]
            parts.append(f"sr {s}: '{o['generic']}' @ {o['price']}")
        exact = all(any(f["code"] == "exact_duplicate" for f in by_sr[s]["flags"]) for s in srs)
        L.append(f"- **{key}** {'(exact)' if exact else ''} — " + "; ".join(parts))
    L.append("")
    L.append(f"## Near-duplicate names (differ only by punctuation / TAB / CAP) — {len(near_groups)} groups\n")
    for srs in sorted(near_groups, key=lambda x: x[0]):
        L.append("- " + "; ".join(f"sr {s} '{by_sr[s]['name']}' @ {by_sr[s]['price']}" for s in srs))
    L.append("")

    misp = Counter()
    for o in out:
        for f in o["flags"]:
            if f["code"] == "generic_misspelling":
                misp[f["detail"]] += 1
    L.append("## Generic misspellings (rows)\n")
    for d, n in misp.most_common():
        L.append(f"- {d}: {n}")
    L.append("")
    su = [o for o in out if any(f["code"] == "strength_unit_suspect" for f in o["flags"])]
    L.append(f"## strength_unit_suspect — {len(su)} rows\n")
    L.append(", ".join(f"{o['sr']} {o['name']}" for o in su))
    L.append("")
    L.append("## Flag code reference\n")
    L.append("See the module docstring of `data/tools/audit_source.py`.")
    with open(OUT_MD, "w", encoding="utf-8") as fh:
        fh.write("\n".join(L) + "\n")


if __name__ == "__main__":
    main()
