// Package enrichment fills in medicine information for catalogue products
// from an authoritative public source, in resumable batches.
//
// Source: the US FDA's openFDA drug-label API (public-domain prescribing
// information). Lookups are by ACTIVE INGREDIENT and route, not by brand —
// the brands in this catalogue are Indian generics with no label of their
// own in that database — so everything stored here is general information
// about the ingredient, quoted from the label, and is presented that way.
//
// Rules this package keeps:
//   - Nothing is invented. A field with no source text stays NULL and the
//     product is flagged (missing_fields / needs_manual_review).
//   - Existing product data is never overwritten; products.* columns are
//     only filled when NULL.
//   - No dosage instructions are stored or shown.
//   - "Verified" is only ever set by a human in the admin screen.
package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sanyam/medicart/services/api/internal/db"
)

const sourceName = "US FDA drug label (openFDA)"

// RecomputeVerificationSQL derives product_enrichment.verification_status
// ($1 = product id). VERIFIED needs a person to have checked both the
// information and a real image; a person's check of the information alone,
// or a cited source without any hold, is PARTIALLY_VERIFIED; anything held
// (suspect composition, regulatory review) or without source is NEEDS_REVIEW.
const RecomputeVerificationSQL = `
	UPDATE product_enrichment SET verification_status = CASE
		WHEN last_verified_at IS NOT NULL AND image_status = 'verified' THEN 'VERIFIED'
		WHEN last_verified_at IS NOT NULL THEN 'PARTIALLY_VERIFIED'
		WHEN hold_generated_text OR regulatory_group = 'not_approved_for_human_use' THEN 'NEEDS_REVIEW'
		WHEN status IN ('completed','partially_completed') THEN 'PARTIALLY_VERIFIED'
		ELSE 'NEEDS_REVIEW' END
	 WHERE product_id = $1`

// ErrRateLimited stops a run cleanly; unprocessed products stay pending.
var ErrRateLimited = errors.New("enrichment: source rate limit reached")

type Service struct {
	pool   *db.Pool
	http   *http.Client
	apiKey string        // optional OPENFDA_API_KEY: raises the daily quota
	pause  time.Duration // between source requests
}

func NewService(pool *db.Pool, apiKey string) *Service {
	return &Service{pool: pool, http: &http.Client{Timeout: 25 * time.Second}, apiKey: apiKey, pause: 350 * time.Millisecond}
}

// ---------------------------------------------------------------- parsing

// synonyms maps the names used in Indian/British practice (and common
// misspellings in the source price list) to the US adopted name the label
// database indexes by.
var synonyms = map[string]string{
	"ACICLOVIR": "ACYCLOVIR", "AMOXYCILLIN": "AMOXICILLIN", "AMOXYCILIN": "AMOXICILLIN", "AMOXICILIN": "AMOXICILLIN",
	"PARACETAMOL": "ACETAMINOPHEN", "SALBUTAMOL": "ALBUTEROL", "LEVOSALBUTAMOL": "LEVALBUTEROL",
	"FRUSEMIDE": "FUROSEMIDE", "LIGNOCAINE": "LIDOCAINE", "LIGNOCAIN": "LIDOCAINE", "ADRENALINE": "EPINEPHRINE",
	"THYROXINE": "LEVOTHYROXINE", "RIFAMPICIN": "RIFAMPIN", "GLYCERYL TRINITRATE": "NITROGLYCERIN",
	"CLOMIFENE": "CLOMIPHENE", "CYCLOSPORINE": "CYCLOSPORINE", "CLAVULANTE": "CLAVULANATE",
	"POTASSIUM CLAVULANTE": "CLAVULANATE", "POTASSIUM CLAVULANATE": "CLAVULANATE", "CLAVULANIC ACID": "CLAVULANATE",
	"BECLOMETASONE": "BECLOMETHASONE", "SULPHAMETHOXAZOLE": "SULFAMETHOXAZOLE", "SULPHMETHOXAZOLE": "SULFAMETHOXAZOLE",
	"METHYLCOBALAMIN": "CYANOCOBALAMIN", "CYANOCOBALAMINE": "CYANOCOBALAMIN", "CETRIZINE": "CETIRIZINE",
	"MOTELUKAST": "MONTELUKAST", "MIRTAZPINE": "MIRTAZAPINE", "ARIPIRAZOLE": "ARIPIPRAZOLE", "ENALAPRI": "ENALAPRIL",
	"NITROFURANTION": "NITROFURANTOIN", "TORASEMIDE": "TORSEMIDE", "CHOLECALCIFEROL": "CHOLECALCIFEROL",
	"VITAMIN D3": "CHOLECALCIFEROL", "VITAMIN C": "ASCORBIC ACID", "CARBAMEZAPINE": "CARBAMAZEPINE",
	"SODIUM VALPROATE": "VALPROIC ACID", "DIVALPROEX": "DIVALPROEX", "MESALAZINE": "MESALAMINE",
	"ISOSORBIDE MONONITRATE": "ISOSORBIDE MONONITRATE", "TRIHEXYPHENIDYL": "TRIHEXYPHENIDYL",
	"HYOSCINE BUTYLBROMIDE": "SCOPOLAMINE", "CEPHALEXIN": "CEPHALEXIN", "LEVETIRACETAM": "LEVETIRACETAM",
	"URSODEOXYCHOLIC": "URSODIOL", "SODIUM ALENDRONATE": "ALENDRONATE", "CICLOPIROX": "CICLOPIROX",
	"PHYTOMENADIONE": "PHYTONADIONE", "BENZATHINE PENICILLIN": "PENICILLIN G BENZATHINE", "FLUOXAMINE": "FLUVOXAMINE",
	"IVABRIDINE": "IVABRADINE", "ATOMEXITINE": "ATOMOXETINE", "CIPROPRXACIN": "CIPROFLOXACIN", "MINICYCLINE": "MINOCYCLINE",
	"FAMCYCLOVIR": "FAMCICLOVIR", "VALACYCLOVIR": "VALACYCLOVIR", "SUMATROPTAN": "SUMATRIPTAN", "PRAMIPEXOL": "PRAMIPEXOLE",
	"LIVETIRACETAM": "LEVETIRACETAM", "LEVONOGESTREL": "LEVONORGESTREL", "METHOCARMABOL": "METHOCARBAMOL",
	"HYROQUINONE": "HYDROQUINONE", "ROPRANOLOL": "PROPRANOLOL", "NIMUSULIDE": "NIMESULIDE", "MEBENDAZOL": "MEBENDAZOLE",
	"MECOBALAMIN": "CYANOCOBALAMIN", "MECOBALAMIN VIT B12": "CYANOCOBALAMIN", "CYNOCOBALAMIN": "CYANOCOBALAMIN",
	"VITAMIN B12": "CYANOCOBALAMIN", "COMPLEX VITAMIN B12": "CYANOCOBALAMIN", "SILVER SULFADIZINE": "SILVER SULFADIAZINE",
	"METHYL PREDNISOLONE": "METHYLPREDNISOLONE", "TENOFOVIR DISOP": "TENOFOVIR", "EMTRICIABIN": "EMTRICITABINE",
	"CHLORTALIDONE": "CHLORTHALIDONE", "VALPROATE VALPROIC ACID": "VALPROIC ACID", "OF VALPROATE": "VALPROIC ACID",
	"VALPROATE": "VALPROIC ACID", "THIAMINE VITAMIN B1": "THIAMINE", "ETHINYLESTRADIOL": "ETHINYL ESTRADIOL",
	"NORETHISTERONE": "NORETHINDRONE", "CALCIPOTRIOL": "CALCIPOTRIENE",
}

// nonDrugRe marks catalogue items that are devices, kits or brand-only
// herbal/cosmetic lines: a drug label does not describe them, so they are
// sent to a person instead of being looked up.
var nonDrugRe = regexp.MustCompile(`(?i)\b(?:SYRINGE|SYRING|NEEDLE|KIT|MONITOR\w*|OXY?METER|OXIMETER|THERMOMETER|HIMALAYA|` +
	`SUPPLEMENTS?|STERILE WATER|GLUCOMETER|STRIPS?|LANCETS?|BANDAGE|DRESSING)\b`)

var (
	splitRe = regexp.MustCompile(`\s*(?:\+|&|,|/|\bAND\b|\bWITH\b)\s*`)
	// Salt, form and release-profile words that are not part of the ingredient name.
	noiseRe = regexp.MustCompile(`\b(?:DIETHYLAMINE|DIETHLAMINE|DISOP|HCL|HCI|HYDROCHLORIDE|HYDROCLORIDE|DIHCL|DIHYDROCHLORIDE|SODIUM|POTASSIUM|CALCIUM|BESILATE|BESYLATE|` +
		`MALEATE|CITRATE|PHOSPHATE|SULPHATE|SULFATE|ACETATE|FUMARATE|FUMERATE|SUCCINATE|TARTRATE|BITARTATE|MESYLATE|OXALATE|TRIHYDRATE|` +
		`FUROATE|FURATE|PROPIONATE|DIPROPIONATE|VALERATE|ACETONIDE|ETABONATE|AXETIL|PROXETIL|MEDOXOMIL|CILEXETIL|ETEXILATE|DISOPROXIL|` +
		`ALAFENAMIDE|TROMETHAMINE|TROMETAMOL|HBR|` +
		`TAB|TABS|TABLETS?|CAP|CAPS|CAPSULES?|INJ|INJECTION|GEL|CREAM|OINTMENT|SUSPENSION|SUSP|SYRUP|SOLUTION|DROPS?|SPRAY|NEBULISER|` +
		`OPHTHALMIC|OPTHALMIC|OPTH|TOPICAL|DISPERSIBLE|CHEWABLE|SUBLINGUAL|EFFERVESCENT|SOFT|MICRONISED|PLUS|` +
		`SUSTAINED|RELEASE|EXTENDED|PROLONGED|CONTROLLED|GASTRO|RESISTANT|RETARD|ER|SR|CR|XL|XR|DT|IU|MG|MCG|GM|ML)\b`)
	junkRe   = regexp.MustCompile(`[^A-Z0-9 \-]+|\d+(?:\.\d+)?\s*(?:MG|MCG|GM|G|ML|IU|NO|%)\b|\b\d+(\.\d+)?%?\b`)
	hyphenRe = regexp.MustCompile(`(?:^|\s)-+|-+(?:\s|$)`)
	spaceRe  = regexp.MustCompile(`\s+`)
	strengRe = regexp.MustCompile(`(?i)\b(\d+(?:\.\d+)?(?:\s?/\s?\d+(?:\.\d+)?)?\s?(?:MG|MCG|IU)|\d+(?:\.\d+)?\s?%)`)
	packRe   = regexp.MustCompile(`(?i)\b(\d+(?:\.\d+)?\s?(?:GM|G|ML|MD))\b`)
)

// Ingredients splits a "GENERIC/CONTAINS" string into normalised ingredient keys.
func Ingredients(generic string) []string {
	seen := map[string]bool{}
	var out []string
	up := strings.ReplaceAll(strings.ToUpper(generic), "ANDMETFORMIN", "AND METFORMIN")
	for _, part := range splitRe.Split(up, -1) {
		k := normalise(part)
		if len(k) < 4 || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}

func normalise(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if v, ok := synonyms[spaceRe.ReplaceAllString(s, " ")]; ok {
		return v
	}
	s = junkRe.ReplaceAllString(s, " ")
	s = noiseRe.ReplaceAllString(s, " ")
	s = hyphenRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
	if v, ok := synonyms[s]; ok {
		return v
	}
	return s
}

// routeGroup picks which kind of label applies to a dosage form, so a cream
// is not described using the label for the same ingredient's tablets.
func routeGroup(form, name string) string {
	switch form {
	case "CREAM", "GEL", "OINTMENT", "LOTION":
		if strings.Contains(strings.ToUpper(name), "EYE") {
			return "OPHTHALMIC"
		}
		return "TOPICAL"
	case "DROPS":
		if strings.Contains(strings.ToUpper(name), "EAR") && !strings.Contains(strings.ToUpper(name), "EYE") {
			return "ANY"
		}
		return "OPHTHALMIC"
	case "INHALER":
		return "INHALATION"
	case "INJECTION":
		return "INJECTION"
	case "TABLET", "CAPSULE", "SYRUP", "SUSPENSION", "SACHET", "POWDER":
		return "ORAL"
	}
	return "ANY"
}

var routeQuery = map[string]string{
	"ORAL": `"ORAL"`, "TOPICAL": `"TOPICAL"`, "OPHTHALMIC": `"OPHTHALMIC"`,
	"INHALATION": `"RESPIRATORY (INHALATION)"`, "INJECTION": `(INTRAVENOUS INTRAMUSCULAR SUBCUTANEOUS)`,
}

// ---------------------------------------------------------------- source

type Monograph struct {
	Status, MatchedName, PharmClass                                     string
	Indications, Mechanism, Warnings, Boxed, Adverse, Contraindications string
	Storage, SourceURL                                                  string
}

type fdaResp struct {
	Error   *struct{ Code, Message string } `json:"error"`
	Results []map[string]json.RawMessage    `json:"results"`
}

func first(r map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		var arr []string
		if raw, ok := r[k]; ok && json.Unmarshal(raw, &arr) == nil && len(arr) > 0 && strings.TrimSpace(arr[0]) != "" {
			return arr[0]
		}
	}
	return ""
}

var (
	headRe = regexp.MustCompile(`(?i)^\s*(?:\d+(?:\.\d+)?\s*)?(?:indications?\s*(?:and|&)\s*usage|uses?|warnings?\s*(?:and|&)\s*precautions|warnings?|precautions|` +
		`contraindications|adverse reactions|mechanism of action|clinical pharmacology|storage and handling|boxed warning|how supplied)\s*:?\s+`)
	// US-only reporting/contact lines: correct in a US label, wrong on an
	// Indian storefront.
	usContactRe = regexp.MustCompile(`(?i)\s*To report (?:SUSPECTED ADVERSE REACTIONS|side effects).*?(?:1-800-FDA-1088|www\.fda\.gov/medwatch)[^.]*\.?`)
	oBulletRe   = regexp.MustCompile(`(\s)o\s+([A-Z(0-9])`)
	oTailRe     = regexp.MustCompile(`\s+o\s*$`)
	refRe       = regexp.MustCompile(`\s*[\(\[]\s*(?:see\s+)?(?:\d+(?:\.\d+)?\s*,?\s*)+[\)\]]|\[\s*see [^\]]*\]|\(\s*see [^\)]*\)`)
)

// usJunkRe matches sentences that only make sense on US packaging: US
// phone numbers, distributor lines, recycling and origin notes.
var usJunkRe = regexp.MustCompile(`(?i)\b1-8\d\d-[A-Z0-9]{3}-[A-Z0-9]{4}\b|\(?\b1-8\d\d-\d{3}-\d{4}\b\)?|fda\.gov|medwatch|walmart|` +
	`distributed by|manufactured for|made in the usa|how2recycle|do not recycle|factory certified|www\.[a-z0-9.-]+\.(?:com|net)`)

// boilerRe matches label boilerplate that tells a shopper nothing about
// the medicine (it is about how trial data was gathered).
var boilerRe = regexp.MustCompile(`(?i)because clinical trials are conducted under widely varying conditions|` +
	`may not reflect the rates observed in (?:clinical )?practice|` +
	`because these reactions are reported voluntarily from a population of uncertain size`)

var sentenceRe = regexp.MustCompile(`(?:[^.!?]|\.\d)+[.!?]*`)

// dropSentences removes every sentence matching re, keeping paragraphs.
func dropSentences(s string, re *regexp.Regexp) string {
	if s == "" || !re.MatchString(s) {
		return s
	}
	var keep []string
	for _, para := range strings.Split(s, "\n\n") {
		var sent []string
		for _, x := range sentenceRe.FindAllString(para, -1) {
			if !re.MatchString(x) {
				sent = append(sent, strings.TrimSpace(x))
			}
		}
		if p := strings.TrimSpace(strings.Join(sent, " ")); p != "" {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, "\n\n")
}

// sanitize drops US-only sentences from already-excerpted text.
func sanitize(s string) string {
	return dropSentences(s, usJunkRe)
}

// excerpt cleans one label section and cuts it at a sentence boundary. The
// text is quoted, never paraphrased, so nothing is added to what the label says.
func excerpt(s string, max int) string {
	s = spaceRe.ReplaceAllString(strings.TrimSpace(s), " ")
	for i := 0; i < 3; i++ {
		loc := headRe.FindStringIndex(s)
		if loc == nil {
			break
		}
		s = s[loc[1]:]
	}
	s = strings.TrimSpace(refRe.ReplaceAllString(usContactRe.ReplaceAllString(s, ""), ""))
	s = strings.NewReplacer(" .", ".", " ,", ",", "•", "- ", "▪", "- ", "●", "- ", "■", "- ").Replace(s)
	// Labels use a bare "o" as a sub-bullet: "Hypertension o Heart failure".
	s = oBulletRe.ReplaceAllString(s, "$1- $2")
	s = strings.TrimSpace(oTailRe.ReplaceAllString(s, ""))
	s = dropSentences(dropSentences(s, boilerRe), usJunkRe)
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	if i := strings.LastIndex(cut, ". "); i > max/3 {
		return cut[:i+1]
	}
	return strings.TrimSpace(cut[:strings.LastIndex(cut, " ")]) + "…"
}

// brandStop lists words that appear in OTC brand names but are also
// ordinary label words, so they are never treated as a brand on their own.
var brandStop = map[string]bool{"ALLERGY": true, "RELIEF": true, "CHILDRENS": true, "CHILDREN'S": true, "INFANTS": true,
	"EXTRA": true, "STRENGTH": true, "MAXIMUM": true, "REGULAR": true, "SEVERE": true, "NASAL": true, "COUGH": true,
	"COLD": true, "PAIN": true, "SINUS": true, "ORIGINAL": true, "ADVANCED": true, "TRIPLE": true, "DAILY": true}

// replaceBrands swaps the US brand names a label uses for the generic name:
// the catalogue product is a different (Indian) brand, so the US one would
// only confuse. Nothing else in the quoted text changes.
func replaceBrands(text string, brands []string, generic string) string {
	if text == "" {
		return text
	}
	g := strings.ToUpper(generic)
	for _, b := range brands {
		b = strings.TrimSpace(b)
		ub := strings.ToUpper(b)
		if len(b) < 3 || strings.Contains(g, ub) || strings.Contains(ub, g) {
			continue
		}
		re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(b) + `\b\s*[®™]?`)
		text = re.ReplaceAllString(text, title(generic)+" ")
		// The bare first word of a multi-word brand ("PULMICORT" from
		// "PULMICORT FLEXHALER"), only where the label writes it in capitals.
		if f := strings.Fields(ub); len(f) > 1 && len(f[0]) >= 5 && !brandStop[f[0]] && !strings.Contains(g, f[0]) {
			re := regexp.MustCompile(`\b` + regexp.QuoteMeta(f[0]) + `\b\s*[®™]?`)
			text = re.ReplaceAllString(text, title(generic)+" ")
		}
	}
	return strings.TrimSpace(spaceRe.ReplaceAllString(strings.NewReplacer(" .", ".", " ,", ",", " )", ")").Replace(text), " "))
}

// componentRe splits a label's generic name into its active ingredients.
var componentRe = regexp.MustCompile(`\s*(?:,|/|\bAND\b)\s*`)

// fetch looks up one label. With one ingredient it accepts only a
// single-ingredient label (a combination label would attribute the other
// ingredient's uses and warnings to this one). With several it accepts only
// a label for exactly that combination.
func (s *Service) fetch(ctx context.Context, ings []string, route string) (*Monograph, error) {
	var terms []string
	for _, ing := range ings {
		terms = append(terms, fmt.Sprintf(`openfda.generic_name:"%s"`, ing))
	}
	search := strings.Join(terms, " AND ")
	if rq, ok := routeQuery[route]; ok {
		search += fmt.Sprintf(` AND openfda.route:%s`, rq)
	}
	q := url.Values{"search": {search}, "limit": {"25"}}
	if s.apiKey != "" {
		q.Set("api_key", s.apiKey)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.fda.gov/drug/label.json?"+q.Encode(), nil)
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, ErrRateLimited
	case resp.StatusCode == http.StatusNotFound:
		return &Monograph{Status: "not_found"}, nil
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("source returned HTTP %d", resp.StatusCode)
	}
	var body fdaResp
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	type openfda struct {
		GenericName []string `json:"generic_name"`
		BrandName   []string `json:"brand_name"`
		EPC         []string `json:"pharm_class_epc"`
		SetID       []string `json:"spl_set_id"`
	}
	type cand struct {
		r   map[string]json.RawMessage
		of  openfda
		g   string
		len int
	}
	var best *cand
	for _, r := range body.Results {
		var of openfda
		_ = json.Unmarshal(r["openfda"], &of)
		if len(of.GenericName) == 0 || first(r, "indications_and_usage") == "" {
			continue
		}
		g := strings.ToUpper(of.GenericName[0])
		ok := true
		for _, ing := range ings {
			if !strings.Contains(g, ing) {
				ok = false
			}
		}
		if !ok || len(componentRe.Split(g, -1)) != len(ings) {
			continue
		}
		// Shortest generic name wins (the plainest label for exactly these
		// ingredients); among equals, prefer a label whose text kept its
		// bullet points: some copies of the same OTC label lose them, which
		// runs the list items together ("headache the common cold backache").
		score := len(g) * 2
		if !strings.ContainsAny(first(r, "indications_and_usage"), "•●▪") {
			score++
		}
		if best == nil || score < best.len {
			best = &cand{r, of, g, score}
		}
	}
	if best == nil {
		return &Monograph{Status: "not_found"}, nil
	}
	c := best
	sec := func(max int, keys ...string) string {
		return excerpt(replaceBrands(first(c.r, keys...), c.of.BrandName, c.g), max)
	}
	m := &Monograph{
		Status: "found", MatchedName: c.g,
		Indications:       sec(600, "indications_and_usage"),
		Mechanism:         sec(500, "mechanism_of_action", "clinical_pharmacology"),
		Warnings:          sec(700, "warnings_and_cautions", "warnings", "precautions"),
		Boxed:             sec(500, "boxed_warning"),
		Adverse:           sec(600, "adverse_reactions"),
		Contraindications: sec(500, "contraindications"),
		Storage:           sec(300, "storage_and_handling"),
	}
	var classes []string
	for _, e := range c.of.EPC {
		classes = append(classes, strings.TrimSuffix(e, " [EPC]"))
	}
	m.PharmClass = strings.Join(classes, "; ")
	if len(ings) == 1 && len(classes) > 0 {
		m.PharmClass = classes[0]
	}
	if len(c.of.SetID) > 0 {
		m.SourceURL = "https://dailymed.nlm.nih.gov/dailymed/drugInfo.cfm?setid=" + c.of.SetID[0]
	}
	return m, nil
}

// monograph returns the cached monograph, fetching it once if absent. A
// 'not_found' result is cached too, so re-runs do not re-query the source.
func (s *Service) monograph(ctx context.Context, ings []string, route string) (*Monograph, error) {
	ingredient := strings.Join(ings, " + ")
	var m Monograph
	err := s.pool.QueryRow(ctx, `
		SELECT status, COALESCE(matched_name,''), COALESCE(pharm_class,''), COALESCE(indications,''), COALESCE(mechanism,''),
		       COALESCE(warnings,''), COALESCE(boxed_warning,''), COALESCE(adverse_reactions,''), COALESCE(contraindications,''),
		       COALESCE(storage,''), COALESCE(source_url,'')
		  FROM drug_monographs WHERE ingredient_key = $1 AND route_group = $2 AND (status <> 'error' OR attempts >= 3)`,
		ingredient, route).Scan(&m.Status, &m.MatchedName, &m.PharmClass, &m.Indications, &m.Mechanism, &m.Warnings,
		&m.Boxed, &m.Adverse, &m.Contraindications, &m.Storage, &m.SourceURL)
	if err == nil {
		return &m, nil
	}
	if !db.IsNoRows(err) {
		return nil, err
	}

	time.Sleep(s.pause)
	got, ferr := s.fetch(ctx, ings, route)
	if errors.Is(ferr, ErrRateLimited) {
		return nil, ferr
	}
	return s.storeAndReturn(ctx, ingredient, route, got, ferr)
}

func (s *Service) storeAndReturn(ctx context.Context, ingredient, route string, m *Monograph, ferr error) (*Monograph, error) {
	if ferr != nil {
		m = &Monograph{Status: "error"}
	}
	var lastErr any
	if ferr != nil {
		lastErr = ferr.Error()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO drug_monographs (ingredient_key, route_group, status, matched_name, pharm_class, indications, mechanism,
			warnings, boxed_warning, adverse_reactions, contraindications, storage, source_name, source_url, attempts, last_error)
		VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),NULLIF($10,''),
			NULLIF($11,''),NULLIF($12,''),$13,NULLIF($14,''),1,$15)
		ON CONFLICT (ingredient_key, route_group) DO UPDATE SET status = EXCLUDED.status, attempts = drug_monographs.attempts + 1,
			last_error = EXCLUDED.last_error, fetched_at = now()`,
		ingredient, route, m.Status, m.MatchedName, m.PharmClass, m.Indications, m.Mechanism, m.Warnings, m.Boxed,
		m.Adverse, m.Contraindications, m.Storage, sourceName, m.SourceURL, lastErr)
	if err != nil {
		return nil, fmt.Errorf("cache monograph: %w", err)
	}
	return m, ferr
}

// ---------------------------------------------------------------- pipeline

type RunResult struct {
	Processed, Completed, Partial, NeedsReview, Failed int
	RateLimited                                        bool
}

type row struct {
	id, name, generic, form string
	rx, hold                bool
}

var formWords = map[string]string{
	"TABLET": "tablet", "CAPSULE": "capsule", "SYRUP": "syrup", "SUSPENSION": "oral suspension", "INJECTION": "injection",
	"CREAM": "cream", "OINTMENT": "ointment", "GEL": "gel", "DROPS": "drops", "INHALER": "inhaler", "SPRAY": "spray",
	"POWDER": "powder", "SACHET": "sachet", "SOLUTION": "solution", "LOTION": "lotion", "DEVICE": "medical device",
}

// Run processes up to limit products that still need work. It is safe to
// stop and restart at any point: each product is claimed ('processing'),
// then written with its final status in one statement; a product left in
// 'processing' by a crash is picked up again after 10 minutes.
func (s *Service) Run(ctx context.Context, limit, batchSize int, progress func(RunResult)) (RunResult, error) {
	var res RunResult
	for res.Processed < limit {
		n := batchSize
		if limit-res.Processed < n {
			n = limit - res.Processed
		}
		rows, err := s.pool.Query(ctx, `
			UPDATE product_enrichment e SET status = 'processing', attempts = attempts + 1, updated_at = now()
			  FROM products p
			 WHERE p.id = e.product_id AND e.product_id IN (
				SELECT product_id FROM product_enrichment
				 WHERE status = 'pending' OR (status = 'failed' AND attempts < 3)
				    OR (status = 'processing' AND updated_at < now() - INTERVAL '10 minutes')
				 ORDER BY created_at, product_id LIMIT $1 FOR UPDATE SKIP LOCKED)
			RETURNING p.id::text, p.name, COALESCE(p.generic_name,''), COALESCE(p.dosage_form,''), p.requires_prescription, e.hold_generated_text`, n)
		if err != nil {
			return res, fmt.Errorf("claim batch: %w", err)
		}
		var batch []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.name, &r.generic, &r.form, &r.rx, &r.hold); err != nil {
				rows.Close()
				return res, err
			}
			batch = append(batch, r)
		}
		rows.Close()
		if len(batch) == 0 {
			return res, nil
		}
		for i, r := range batch {
			status, err := s.enrichOne(ctx, r)
			if errors.Is(err, ErrRateLimited) {
				// Hand this and the rest of the batch back untouched.
				for _, rest := range batch[i:] {
					_, _ = s.pool.Exec(ctx, `UPDATE product_enrichment SET status='pending', attempts = GREATEST(attempts-1,0) WHERE product_id=$1`, rest.id)
				}
				res.RateLimited = true
				return res, nil
			}
			if err != nil {
				status = "failed"
				_, _ = s.pool.Exec(ctx, `UPDATE product_enrichment SET status='failed', last_error=$2, processed_at=now() WHERE product_id=$1`, r.id, err.Error())
			}
			res.Processed++
			switch status {
			case "completed":
				res.Completed++
			case "partially_completed":
				res.Partial++
			case "needs_manual_review":
				res.NeedsReview++
			default:
				res.Failed++
			}
		}
		if progress != nil {
			progress(res)
		}
	}
	return res, nil
}

func title(s string) string {
	w := strings.Fields(strings.ToLower(s))
	for i := range w {
		w[i] = strings.ToUpper(w[i][:1]) + w[i][1:]
	}
	return strings.Join(w, " ")
}

func (s *Service) enrichOne(ctx context.Context, r row) (string, error) {
	if r.hold {
		return s.holdOne(ctx, r)
	}
	ings := Ingredients(r.generic)
	route := routeGroup(r.form, r.name)

	nonDrug := r.form == "DEVICE" || nonDrugRe.MatchString(r.generic) || len(ings) == 0

	var found []*Monograph
	var foundNames, unmatched []string
	if len(ings) > 1 && !nonDrug {
		// A label for exactly this combination describes it best (e.g.
		// amoxicillin with clavulanate); fall back to one label per ingredient.
		key := append([]string{}, ings...)
		sort.Strings(key)
		m, err := s.monograph(ctx, key, route)
		if errors.Is(err, ErrRateLimited) {
			return "", err
		}
		if err == nil && m.Status == "found" {
			found = append(found, m)
			foundNames = append(foundNames, "")
		}
	}
	if len(found) == 0 && !nonDrug {
		for _, ing := range ings {
			m, err := s.monograph(ctx, []string{ing}, route)
			if errors.Is(err, ErrRateLimited) {
				return "", err
			}
			if err != nil || m.Status != "found" {
				unmatched = append(unmatched, title(ing))
				continue
			}
			found = append(found, m)
			foundNames = append(foundNames, title(ing))
		}
	}

	strength := strings.ToUpper(strings.Join(strengRe.FindAllString(r.name, 2), " / "))
	pack := strings.ToUpper(packRe.FindString(r.name))
	form := formWords[r.form]
	if form == "" {
		form = "product"
	}
	kind := "an over-the-counter"
	if r.rx {
		kind = "a prescription"
	}
	generic := title(r.generic)
	short := fmt.Sprintf("%s is %s %s", r.name, kind, form)
	if generic != "" && !nonDrug {
		short += " containing " + generic
	}
	if strength != "" {
		short += " (" + strength + ")"
	}
	short += "."

	join := func(get func(*Monograph) string) string {
		var parts []string
		for i, m := range found {
			if t := sanitize(get(m)); t != "" {
				if len(found) > 1 && foundNames[i] != "" {
					t = foundNames[i] + ": " + t
				}
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "\n\n")
	}
	uses := join(func(m *Monograph) string { return m.Indications })
	mech := join(func(m *Monograph) string { return m.Mechanism })
	prec := join(func(m *Monograph) string { return m.Warnings })
	side := join(func(m *Monograph) string { return m.Adverse })
	contra := join(func(m *Monograph) string { return m.Contraindications })
	boxed := join(func(m *Monograph) string { return m.Boxed })
	storage := join(func(m *Monograph) string { return m.Storage })
	class := join(func(m *Monograph) string { return m.PharmClass })
	class = strings.ReplaceAll(class, "\n\n", "; ")

	desc := short
	if class != "" {
		desc += " Pharmacological class: " + class + "."
	}
	if uses != "" {
		desc += "\n\nWhat it is used for (from the US FDA label for the active ingredient):\n" + uses
	}
	if mech != "" {
		desc += "\n\nHow it works:\n" + mech
	}

	var missing []string
	for k, v := range map[string]string{"uses": uses, "mechanism_of_action": mech, "precautions": prec,
		"side_effects": side, "contraindications": contra, "storage": storage, "strength": strength} {
		if v == "" {
			missing = append(missing, k)
		}
	}
	missing = append(missing, "manufacturer", "verified_image")
	if nonDrug {
		// Devices, kits and brand-only herbal lines: no drug label applies.
		missing = append(missing, "product_information_from_supplier")
	}
	sort.Strings(missing)

	status := "completed"
	switch {
	case len(found) == 0:
		status = "needs_manual_review"
	case len(unmatched) > 0 || uses == "" || prec == "" || side == "":
		status = "partially_completed"
	}

	var srcURL string
	if len(found) > 0 {
		srcURL = found[0].SourceURL
	}
	seoTitle := r.name
	if generic != "" {
		seoTitle += " – " + generic
	}
	meta := short
	if len(meta) > 155 {
		meta = meta[:152] + "…"
	}

	err := s.pool.InTx(ctx, func(q db.Querier) error {
		// Fill a products.* text column only when it is empty or still holds
		// exactly what this pipeline wrote last time; text a person wrote or
		// edited never matches, so it is never overwritten.
		if _, err := q.Exec(ctx, `
			UPDATE products p SET
				description  = CASE WHEN p.description  IS NULL OR p.description  = e.generated_description THEN NULLIF($2,'') ELSE p.description END,
				key_uses     = CASE WHEN p.key_uses     IS NULL OR p.key_uses     = e.generated_uses        THEN NULLIF($3,'') ELSE p.key_uses END,
				storage_info = CASE WHEN p.storage_info IS NULL OR p.storage_info = e.generated_storage     THEN NULLIF($4,'') ELSE p.storage_info END,
				strength     = COALESCE(p.strength,  NULLIF($5,'')),
				pack_size    = COALESCE(p.pack_size, NULLIF($6,''))
			  FROM product_enrichment e
			 WHERE p.id = $1 AND e.product_id = p.id`, r.id, desc, uses, storage, strength, pack); err != nil {
			return fmt.Errorf("update product: %w", err)
		}
		if _, err := q.Exec(ctx, `
			UPDATE product_enrichment e SET
				generated_description = CASE WHEN p.description  IS NOT DISTINCT FROM NULLIF($2,'') THEN p.description END,
				generated_uses        = CASE WHEN p.key_uses     IS NOT DISTINCT FROM NULLIF($3,'') THEN p.key_uses END,
				generated_storage     = CASE WHEN p.storage_info IS NOT DISTINCT FROM NULLIF($4,'') THEN p.storage_info END
			  FROM products p
			 WHERE e.product_id = $1 AND p.id = e.product_id`, r.id, desc, uses, storage); err != nil {
			return fmt.Errorf("record generated text: %w", err)
		}
		_, err := q.Exec(ctx, `
			UPDATE product_enrichment SET status=$2, therapeutic_category=NULLIF($3,''), short_description=$4,
				mechanism_of_action=NULLIF($5,''), precautions=NULLIF($6,''), side_effects=NULLIF($7,''), warnings=NULLIF($8,''),
				contraindications=NULLIF($9,''), seo_title=$10, meta_description=$11,
				info_source = CASE WHEN $12 <> '' THEN $13 END, info_source_url=NULLIF($12,''),
				missing_fields=$14, unmatched_ingredients=$15, last_error=NULL, processed_at=now()
			 WHERE product_id=$1`,
			r.id, status, class, short, mech, prec, side, boxed, contra, seoTitle, meta, srcURL, sourceName,
			missing, append([]string{}, unmatched...))
		if err != nil {
			return err
		}
		_, err = q.Exec(ctx, RecomputeVerificationSQL, r.id)
		return err
	})
	if err != nil {
		return "", err
	}
	return status, nil
}

// holdOne handles a product whose listed composition is suspected wrong: a
// description built from that composition would describe a different
// medicine, so any text this pipeline wrote earlier is withdrawn (text a
// person wrote is kept) and the product goes to a pharmacist.
func (s *Service) holdOne(ctx context.Context, r row) (string, error) {
	err := s.pool.InTx(ctx, func(q db.Querier) error {
		if _, err := q.Exec(ctx, `
			UPDATE products p SET
				description  = CASE WHEN p.description  = e.generated_description THEN NULL ELSE p.description END,
				key_uses     = CASE WHEN p.key_uses     = e.generated_uses        THEN NULL ELSE p.key_uses END,
				storage_info = CASE WHEN p.storage_info = e.generated_storage     THEN NULL ELSE p.storage_info END
			  FROM product_enrichment e WHERE p.id = $1 AND e.product_id = p.id AND e.edited_at IS NULL`, r.id); err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `
			UPDATE product_enrichment SET status = 'needs_manual_review',
				generated_description = NULL, generated_uses = NULL, generated_storage = NULL,
				therapeutic_category = NULL, short_description = NULL, mechanism_of_action = NULL, precautions = NULL,
				side_effects = NULL, warnings = NULL, contraindications = NULL, info_source = NULL, info_source_url = NULL,
				last_error = NULL, processed_at = now()
			 WHERE product_id = $1 AND edited_at IS NULL`, r.id); err != nil {
			return err
		}
		_, err := q.Exec(ctx, RecomputeVerificationSQL, r.id)
		return err
	})
	if err != nil {
		return "", err
	}
	return "needs_manual_review", nil
}
