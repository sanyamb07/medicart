package money

import "testing"

func TestDiscountPercent(t *testing.T) {
	cases := []struct {
		mrp, price Paise
		want       int
	}{
		{3500, 2800, 20},
		{100, 100, 0},
		{100, 110, 0}, // selling above MRP never reports a discount
		{0, 0, 0},
		{1000, 0, 100},
	}
	for _, c := range cases {
		got := DiscountPercent(c.mrp, c.price)
		if got != c.want {
			t.Errorf("DiscountPercent(%d,%d) = %d, want %d", c.mrp, c.price, got, c.want)
		}
	}
}

func TestSubClampsAtZero(t *testing.T) {
	got := Paise(100).Sub(Paise(150))
	if got != 0 {
		t.Errorf("Sub underflow: got %d, want 0", got)
	}
}

func TestFormatIndianGrouping(t *testing.T) {
	cases := []struct {
		p    Paise
		want string
	}{
		{13300, "133.00"},
		{100, "1.00"},
		{123456789, "12,34,567.89"},
		{0, "0.00"},
	}
	for _, c := range cases {
		if got := c.p.Format(); got != c.want {
			t.Errorf("Format(%d) = %q, want %q", c.p, got, c.want)
		}
	}
}

func TestParseRupeesRoundTrip(t *testing.T) {
	cases := map[string]Paise{
		"133.00":  13300,
		"133":     13300,
		"1,234.5": 123450,
		"0.01":    1,
	}
	for in, want := range cases {
		got, err := ParseRupees(in)
		if err != nil {
			t.Fatalf("ParseRupees(%q) error: %v", in, err)
		}
		if got != want {
			t.Errorf("ParseRupees(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseRupeesRejectsNegative(t *testing.T) {
	if _, err := ParseRupees("-5.00"); err == nil {
		t.Error("expected error for negative amount")
	}
}
