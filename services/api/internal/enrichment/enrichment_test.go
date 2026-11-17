package enrichment

import (
	"reflect"
	"testing"
)

func TestIngredients(t *testing.T) {
	cases := map[string][]string{
		"AMOXYCILIN AND POTASSIUM CLAVULANTE":           {"AMOXICILLIN", "CLAVULANATE"},
		"ACECLOFENAC + PARACETAMOL  + THIOCOLCHICOSIDE": {"ACECLOFENAC", "ACETAMINOPHEN", "THIOCOLCHICOSIDE"},
		"AMLODIPINE BESILATE":                           {"AMLODIPINE"},
		"TELMISARTAN HCL":                               {"TELMISARTAN"},
		"ACICLOVIR DISPERSIBLE":                         {"ACYCLOVIR"},
		"LEVOTHYROXINE SODIUM":                          {"LEVOTHYROXINE"},
		"BUDESONIDE NEBULISER":                          {"BUDESONIDE"},
		"SALBUTAMOL":                                    {"ALBUTEROL"},
		"PIPERACILLIN 4GM + TAZOBACTAM-500MG":           {"PIPERACILLIN", "TAZOBACTAM"},
		"DULOXETINE -":                                  {"DULOXETINE"},
		"SITAGLIPTIN ANDMETFORMIN":                      {"SITAGLIPTIN", "METFORMIN"},
		"DICLOFENAC DIETHLAMINE":                        {"DICLOFENAC"},
		"METHYL PREDNISOLONE":                           {"METHYLPREDNISOLONE"},
	}
	for in, want := range cases {
		if got := Ingredients(in); !reflect.DeepEqual(got, want) {
			t.Errorf("Ingredients(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSanitizeDropsUSOnlySentences(t *testing.T) {
	in := "Most common is edema. To report side effects, call Acme at 1-877-921-7873 or FDA at 1-800-FDA-1088. Keep away from children. DISTRIBUTED BY: Walmart Inc."
	want := "Most common is edema. Keep away from children."
	if got := sanitize(in); got != want {
		t.Errorf("sanitize() = %q, want %q", got, want)
	}
}

func TestRouteGroup(t *testing.T) {
	cases := []struct{ form, name, want string }{
		{"CREAM", "ACIVIR CREAM 5GM", "TOPICAL"},
		{"OINTMENT", "ACYLOVIR EYE OINT 5GM", "OPHTHALMIC"},
		{"DROPS", "CANDID EAR DROP", "ANY"},
		{"TABLET", "AMLIP 5MG", "ORAL"},
		{"INHALER", "ASTHALIN INHR", "INHALATION"},
	}
	for _, c := range cases {
		if got := routeGroup(c.form, c.name); got != c.want {
			t.Errorf("routeGroup(%s,%s) = %s, want %s", c.form, c.name, got, c.want)
		}
	}
}

func TestExcerptCleansBulletsAndBoilerplate(t *testing.T) {
	in := "6 ADVERSE REACTIONS Because clinical trials are conducted under widely varying conditions, adverse reaction rates observed in the clinical trials of a drug cannot be directly compared to rates in the clinical trials of another drug and may not reflect the rates observed in practice. Common reactions: o Headache o Nausea o"
	want := "Common reactions: - Headache - Nausea"
	if got := excerpt(in, 600); got != want {
		t.Errorf("excerpt() = %q, want %q", got, want)
	}
}

func TestReplaceBrands(t *testing.T) {
	in := "PULMICORT FLEXHALER is indicated for asthma. Do not use PULMICORT® to relieve acute bronchospasm."
	want := "Budesonide is indicated for asthma. Do not use Budesonide to relieve acute bronchospasm."
	if got := replaceBrands(in, []string{"Pulmicort Flexhaler"}, "BUDESONIDE"); got != want {
		t.Errorf("replaceBrands() = %q, want %q", got, want)
	}
	// A brand that is just the generic name is left alone.
	if got := replaceBrands("Amlodipine lowers blood pressure.", []string{"AMLODIPINE"}, "AMLODIPINE BESYLATE"); got != "Amlodipine lowers blood pressure." {
		t.Errorf("generic-named brand changed text: %q", got)
	}
}
