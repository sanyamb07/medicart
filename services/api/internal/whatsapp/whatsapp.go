// Package whatsapp builds the customer-initiated WhatsApp handoff.
//
// Driver scope in this iteration: only "link" (a wa.me deep link with a
// prefilled message) is implemented and tested. The "cloud" driver
// (WhatsApp Business Cloud API, for automated business-initiated messages —
// payment links, status updates) requires a Meta Business/WhatsApp Business
// Platform account and is NOT implemented here; selecting WHATSAPP_DRIVER=cloud
// is rejected by config validation precisely so this gap cannot be silently
// relied on. See docs/WHATSAPP_INTEGRATION.md.
//
// Important and deliberate: opening the wa.me link is NOT evidence the
// customer sent the message, and this package never claims otherwise — the
// order stays in AWAITING_CUSTOMER_CONFIRMATION until a staff member records
// an explicit confirmation (internal/orders.Service.ConfirmWhatsApp).
package whatsapp

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/sanyam/medicart/services/api/internal/platform/money"
)

type OrderSummary struct {
	Reference  string
	PINCode    string
	Items      []LineSummary
	TotalPaise int64
}

type LineSummary struct {
	Name     string
	PackSize string
	Qty      int
}

// BuildHandoffLink returns a wa.me URL whose prefilled text contains only
// what is needed to confirm the order: reference, line items, amount and
// delivery PIN code. No address, no phone number, no medical detail — the
// message travels in a URL, which can end up in browser history or referrer
// headers, so it must not carry anything sensitive.
func BuildHandoffLink(businessNumberE164NoPlus string, o OrderSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Hi MediCart, I'd like to confirm order %s:\n", o.Reference)
	for _, it := range o.Items {
		pack := it.PackSize
		if pack != "" {
			pack = " (" + pack + ")"
		}
		fmt.Fprintf(&b, "- %s%s x%d\n", it.Name, pack, it.Qty)
	}
	fmt.Fprintf(&b, "Amount: Rs. %s\n", money.Paise(o.TotalPaise).Format())
	fmt.Fprintf(&b, "Delivery PIN: %s\n", o.PINCode)
	b.WriteString("Please confirm availability. Thank you!")

	q := url.Values{}
	q.Set("text", b.String())
	return fmt.Sprintf("https://wa.me/%s?%s", businessNumberE164NoPlus, q.Encode())
}
