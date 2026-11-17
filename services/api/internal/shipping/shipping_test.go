package shipping

import (
	"testing"

	"github.com/sanyam/medicart/services/api/internal/platform/money"
)

func TestDeliveryFeeAppliesFreeThreshold(t *testing.T) {
	s := NewService(nil, 4900, 49900) // defaultFee=₹49, freeAbove=₹499

	cases := []struct {
		subtotal money.Paise
		area     *Area
		want     money.Paise
	}{
		{8400, &Area{DeliveryFeePaise: 4900}, 4900}, // below threshold: fee applies
		{49900, &Area{DeliveryFeePaise: 4900}, 0},   // exactly at threshold: free
		{60000, &Area{DeliveryFeePaise: 4900}, 0},   // above threshold: free
		{100, &Area{DeliveryFeePaise: 9900}, 9900},  // per-area override respected
	}
	for _, c := range cases {
		got := s.DeliveryFee(c.subtotal, c.area)
		if got != c.want {
			t.Errorf("DeliveryFee(%d, fee=%d) = %d, want %d", c.subtotal, c.area.DeliveryFeePaise, got, c.want)
		}
	}
}
