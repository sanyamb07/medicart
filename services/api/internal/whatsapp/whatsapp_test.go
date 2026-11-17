package whatsapp

import (
	"net/url"
	"strings"
	"testing"
)

func TestBuildHandoffLinkContainsRequiredFields(t *testing.T) {
	link := BuildHandoffLink("919999999999", OrderSummary{
		Reference: "MC-26A-TEST1234",
		PINCode:   "400001",
		Items: []LineSummary{
			{Name: "Paracetamol 500mg Tablets", PackSize: "Strip of 15 tablets", Qty: 3},
		},
		TotalPaise: 13300,
	})

	if !strings.HasPrefix(link, "https://wa.me/919999999999?") {
		t.Fatalf("unexpected link prefix: %s", link)
	}

	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	text := u.Query().Get("text")

	for _, want := range []string{
		"MC-26A-TEST1234",
		"Paracetamol 500mg Tablets",
		"Strip of 15 tablets",
		"x3",
		"133.00",
		"400001",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("message missing %q:\n%s", want, text)
		}
	}
}

// The message must never carry anything beyond order ref, items, amount and
// PIN — it travels in a URL, which can land in browser history or referrer
// headers.
func TestBuildHandoffLinkExcludesSensitiveData(t *testing.T) {
	link := BuildHandoffLink("919999999999", OrderSummary{
		Reference:  "MC-26A-TEST1234",
		PINCode:    "400001",
		TotalPaise: 100,
	})
	u, _ := url.Parse(link)
	text := u.Query().Get("text")

	for _, excluded := range []string{"9876543210", "MG Road", "@"} {
		if strings.Contains(text, excluded) {
			t.Errorf("message should not contain %q:\n%s", excluded, text)
		}
	}
}
