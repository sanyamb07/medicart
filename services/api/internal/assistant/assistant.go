// Package assistant implements the customer-support assistant.
//
// This is the "mock" AI driver: deterministic, rule-based intent matching
// that calls the same backend services as everything else (catalogue
// search, shipping, order tracking) rather than inventing answers. It never
// has unrestricted database or SQL access, never fabricates a price, stock
// level, delivery estimate, or order status, and never answers a clinical
// question — those are always escalated.
//
// A real LLM-backed driver (AI_DRIVER=anthropic) is not implemented: doing
// that safely needs the tool-authorization and prompt-injection safeguards
// described in docs/AI_ASSISTANT.md, which is future work. This package is
// structured so that driver could be added as a second implementation of
// the same Respond contract without changing the handler or the frontend.
package assistant

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/sanyam/medicart/services/api/internal/auth"
	"github.com/sanyam/medicart/services/api/internal/catalog"
	"github.com/sanyam/medicart/services/api/internal/crm"
	"github.com/sanyam/medicart/services/api/internal/enquiries"
	"github.com/sanyam/medicart/services/api/internal/orders"
	"github.com/sanyam/medicart/services/api/internal/platform/money"
	"github.com/sanyam/medicart/services/api/internal/shipping"
)

const Driver = "mock"

// Reply is what the assistant sends back. Suggestions are optional quick
// replies the frontend can render as buttons.
type Reply struct {
	Text        string   `json:"text"`
	Suggestions []string `json:"suggestions,omitempty"`
	Escalate    bool     `json:"escalate"`
	// Links are in-app pages the reply points to (e.g. a comparison).
	Links []Link `json:"links,omitempty"`
}

type Link struct {
	Label string `json:"label"`
	Href  string `json:"href"`
}

type Service struct {
	catalog   *catalog.Repo
	shipping  *shipping.Service
	orders    *orders.Service
	crm       *crm.Repo
	enquiries *enquiries.Repo
}

func NewService(catalogRepo *catalog.Repo, shipSvc *shipping.Service, orderSvc *orders.Service, crmRepo *crm.Repo, enquiryRepo *enquiries.Repo) *Service {
	return &Service{catalog: catalogRepo, shipping: shipSvc, orders: orderSvc, crm: crmRepo, enquiries: enquiryRepo}
}

// outcome carries what happened during one Respond call that's worth
// recording as a lead — populated only on the product/price/stock path. A
// zero-value outcome (Intent == "") means nothing enquiry-worthy happened.
type outcome struct {
	Intent   string
	Products []string
	Queries  []string
}

var (
	pinRe = regexp.MustCompile(`\b([1-9][0-9]{5})\b`)
	refRe = regexp.MustCompile(`\bMC-[A-Z0-9]+-[A-Z0-9]{4,}\b`)
	mobRe = regexp.MustCompile(`\b([6-9][0-9]{9})\b`)
)

// clinicalTerms trigger an escalation regardless of what else is asked —
// dosage, interactions, diagnosis, and side effects are pharmacist
// territory, not something a keyword matcher (or an LLM without a
// pharmacist in the loop) should ever answer.
var clinicalTerms = []string{
	"dose", "dosage", "overdose", "side effect", "side-effect", "interact", "interaction",
	"pregnan", "breastfeed", "allerg", "symptom", "diagnos", "should i take", "safe to take",
	"can i take", "how much should", "mg should", "combine with", "mix with",
	// Switching a prescribed medicine is a pharmacist/doctor decision.
	"can i substitute", "can i switch", "can i replace", "switch from", "switch to", "instead of my", "replace my",
}

// Respond answers one message, scoped to one business's own storefront
// (product/price/stock answers only ever reflect that business's own
// business_products — never another tenant's catalog or inventory).
// customerID is the signed-in customer's identity if any (empty for an
// anonymous visitor); when set, the conversation and any product/price/
// purchase intent it shows are logged as a lead for the business to follow
// up on (see internal/crm, internal/enquiries). An anonymous visitor's chat
// is still answered, just not logged as a lead — there is no contactable
// identity to attach one to.
func (s *Service) Respond(ctx context.Context, businessID, customerID, message string) (Reply, error) {
	reply, out := s.respondCore(ctx, businessID, message)
	if reply.err != nil {
		return Reply{}, reply.err
	}

	if customerID != "" {
		if err := s.crm.EnsureCustomer(ctx, businessID, customerID); err != nil {
			return Reply{}, err
		}
		sessionID, err := s.enquiries.StartOrContinueSession(ctx, businessID, customerID)
		if err != nil {
			return Reply{}, err
		}
		if err := s.enquiries.LogMessage(ctx, sessionID, "user", message); err != nil {
			return Reply{}, err
		}
		if err := s.enquiries.LogMessage(ctx, sessionID, "assistant", reply.Text); err != nil {
			return Reply{}, err
		}
		if reply.Escalate {
			if err := s.enquiries.MarkEscalated(ctx, sessionID); err != nil {
				return Reply{}, err
			}
		}
		if out.Intent != "" {
			if err := s.enquiries.RecordIntent(ctx, businessID, customerID, sessionID, out.Intent, out.Products, out.Queries); err != nil {
				return Reply{}, err
			}
		}
	}
	return reply.Reply, nil
}

// replyOrErr lets respondCore return either a reply or an error through one
// value without every call site juggling two return values just to detect
// "nothing enquiry-worthy happened."
type replyOrErr struct {
	Reply
	err error
}

func (s *Service) respondCore(ctx context.Context, businessID, message string) (replyOrErr, outcome) {
	text := strings.ToLower(strings.TrimSpace(message))
	if text == "" {
		return replyOrErr{Reply: Reply{Text: "Ask me about product availability, pricing, delivery to your PIN code, or an order's status."}}, outcome{}
	}

	for _, term := range clinicalTerms {
		if strings.Contains(text, term) {
			return replyOrErr{Reply: Reply{
				Text: "I can't advise on dosage, side effects, interactions, or whether a medicine is right for you — " +
					"that needs a pharmacist's judgement, not an automated answer. Please reach out on WhatsApp and " +
					"a pharmacist will help, or consult your doctor for anything urgent.",
				Escalate: true,
			}}, outcome{}
		}
	}

	if ref := refRe.FindString(strings.ToUpper(message)); ref != "" {
		reply, err := s.orderStatus(ctx, ref, mobRe.FindString(text))
		return replyOrErr{Reply: reply, err: err}, outcome{}
	}
	if containsAny(text, "order status", "track my order", "where is my order", "where's my order") {
		return replyOrErr{Reply: Reply{
			Text:        "Share your order reference (looks like MC-XXXXXXXX) and the mobile number it was placed with, and I'll look it up. Or use the Track Order page.",
			Suggestions: []string{"Track order"},
		}}, outcome{}
	}

	if pin := pinRe.FindString(text); pin != "" && containsAny(text, "deliver", "ship", "pin", "available in", "service") {
		reply, err := s.serviceability(ctx, pin)
		return replyOrErr{Reply: reply, err: err}, outcome{}
	}
	if containsAny(text, "delivery time", "how long", "when will it arrive", "shipping time") {
		return replyOrErr{Reply: Reply{Text: "Delivery time depends on your PIN code — tell me your 6-digit PIN and I'll check."}}, outcome{}
	}

	if containsAny(text, "compare", "alternative", "substitute", "cheaper", "other brand", "generic of", "same salt", "similar to") {
		query := extractCompareQuery(text)
		if query == "" {
			return replyOrErr{Reply: Reply{Text: "Which medicine would you like to compare? Tell me its name (e.g. \"metformin\" or \"Januvia 100\")."}}, outcome{}
		}
		reply, err := s.compareLookup(ctx, businessID, query)
		return replyOrErr{Reply: reply, err: err}, outcome{Intent: "compare", Queries: []string{query}}
	}

	if containsAny(text, "in stock", "available", "do you have", "stock", "price", "cost", "how much") {
		query := extractProductQuery(text)
		if query == "" {
			return replyOrErr{Reply: Reply{Text: "Which product? Tell me its name (e.g. \"paracetamol 500mg\") and I'll check current price and stock."}}, outcome{}
		}
		reply, products, err := s.productLookup(ctx, businessID, query)
		return replyOrErr{Reply: reply, err: err}, outcome{Intent: "product_or_price", Products: products, Queries: []string{query}}
	}

	return replyOrErr{Reply: Reply{
		Text: "I can help with: product availability and pricing, delivery to a PIN code, and order status (with your " +
			"order reference and mobile number). For anything else, message us on WhatsApp.",
		Suggestions: []string{"Check stock", "Delivery to my PIN", "Track my order"},
	}}, outcome{}
}

func (s *Service) productLookup(ctx context.Context, businessID, query string) (Reply, []string, error) {
	products, _, err := s.catalog.ListForBusiness(ctx, businessID, catalog.ListFilter{Query: query, Page: 1, PageSize: 3, Sort: "relevance"})
	if err != nil {
		return Reply{}, nil, fmt.Errorf("search products: %w", err)
	}
	if len(products) == 0 {
		return Reply{Text: fmt.Sprintf("I couldn't find anything matching %q in the catalogue.", query)}, nil, nil
	}
	var b strings.Builder
	names := make([]string, 0, len(products))
	for _, p := range products {
		stock := "in stock"
		if !p.InStock {
			stock = "out of stock"
		}
		rx := ""
		if p.RequiresPrescription {
			rx = " (prescription required)"
		}
		fmt.Fprintf(&b, "%s%s — ₹%s, %s%s\n", p.Name, rx, money.Paise(p.PricePaise).Format(), stock, rxNote(p.RequiresPrescription))
		names = append(names, p.Name)
	}
	return Reply{Text: strings.TrimSpace(b.String())}, names, nil
}

func rxNote(requiresRx bool) string {
	if requiresRx {
		return ". A valid prescription is required and will be verified before this is dispatched."
	}
	return "."
}

func (s *Service) serviceability(ctx context.Context, pin string) (Reply, error) {
	area, err := s.shipping.Check(ctx, pin)
	if err != nil {
		return Reply{}, fmt.Errorf("check serviceability: %w", err)
	}
	if !area.IsServiceable {
		return Reply{Text: fmt.Sprintf("We don't currently deliver to PIN %s.", pin)}, nil
	}
	eta := "a few days"
	if area.ETAMinDays != nil && area.ETAMaxDays != nil {
		eta = fmt.Sprintf("%d–%d days", *area.ETAMinDays, *area.ETAMaxDays)
	}
	return Reply{Text: fmt.Sprintf("Yes — we deliver to %s, %s (PIN %s). Estimated delivery: %s.", area.City, area.State, pin, eta)}, nil
}

func (s *Service) orderStatus(ctx context.Context, reference, mobile string) (Reply, error) {
	if mobile == "" {
		return Reply{Text: fmt.Sprintf("To look up %s, I also need the mobile number the order was placed with.", reference)}, nil
	}
	order, err := s.orders.GetByReferenceForCustomer(ctx, reference, auth.NormaliseMobile(mobile))
	if err != nil {
		if err == orders.ErrOrderNotFound {
			return Reply{Text: "I couldn't find an order matching that reference and mobile number."}, nil
		}
		return Reply{}, fmt.Errorf("look up order: %w", err)
	}
	return Reply{Text: fmt.Sprintf("Order %s is currently: %s.", order.Reference, humaniseStatus(order.Status))}, nil
}

func humaniseStatus(status string) string {
	s := strings.ToLower(strings.ReplaceAll(status, "_", " "))
	return s
}

func containsAny(text string, substrs ...string) bool {
	for _, s := range substrs {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// extractProductQuery strips the common question scaffolding off a stock/
// price question so the remainder is a reasonable search query — "do you
// have paracetamol in stock" -> "paracetamol".
func extractProductQuery(text string) string {
	stop := []string{
		"do you have", "is there", "how much is", "how much does", "what's the price of",
		"what is the price of", "price of", "cost of", "in stock", "available", "stock of", "?",
	}
	q := text
	for _, s := range stop {
		q = strings.ReplaceAll(q, s, " ")
	}
	return strings.TrimSpace(strings.Join(strings.Fields(q), " "))
}

// compareLookup points the shopper to the side-by-side comparison for a
// medicine's exact listed composition. It states facts only: it never names
// a "substitute" or suggests switching a prescribed medicine.
func (s *Service) compareLookup(ctx context.Context, businessID, query string) (Reply, error) {
	products, _, err := s.catalog.ListForBusiness(ctx, businessID, catalog.ListFilter{Query: query, Page: 1, PageSize: 40, Sort: "relevance"})
	if err != nil {
		return Reply{}, fmt.Errorf("search products: %w", err)
	}
	// When the query is itself a salt ("metformin"), anchor on the listed
	// composition most products share that contains it; otherwise (a brand
	// name) anchor on the best-matching product's own composition.
	counts := map[string]int{}
	byGeneric := map[string]*catalog.Product{}
	for _, p := range products {
		if p.GenericName == nil || strings.TrimSpace(*p.GenericName) == "" {
			continue
		}
		g := strings.TrimSpace(*p.GenericName)
		if strings.Contains(strings.ToLower(g), strings.ToLower(query)) {
			counts[g]++
			if byGeneric[g] == nil {
				byGeneric[g] = p
			}
		}
	}
	var anchor *catalog.Product
	best := 0
	for g, n := range counts {
		if n > best {
			best, anchor = n, byGeneric[g]
		}
	}
	if anchor == nil {
		for _, p := range products {
			if p.GenericName != nil && strings.TrimSpace(*p.GenericName) != "" {
				anchor = p
				break
			}
		}
	}
	if anchor == nil {
		return Reply{Text: fmt.Sprintf("I couldn't find a medicine with a listed composition matching %q.", query)}, nil
	}
	salt := strings.TrimSpace(*anchor.GenericName)
	text := fmt.Sprintf("%s contains %s. The comparison page lists every product with this exact composition, grouped "+
		"by strength, dosage form and release type, so prices are only compared like for like.\n\n"+
		"A lower price doesn't make a brand a suitable switch for a prescribed medicine — please confirm any change with your "+
		"doctor or pharmacist.", anchor.Name, salt)
	href := "/compare?salt=" + url.QueryEscape(salt) + "&from=" + url.QueryEscape(anchor.Slug)
	return Reply{Text: text, Links: []Link{{Label: "Compare " + salt + " brands", Href: href}}}, nil
}

// extractCompareQuery strips comparison phrasing: "cheaper alternative to
// januvia" -> "januvia", "compare brands of metformin" -> "metformin".
func extractCompareQuery(text string) string {
	stop := []string{
		"compare", "comparison", "brands of", "brands for", "brands", "other brand of", "other brands of", "other brand",
		"cheaper alternative to", "cheaper alternative for", "alternative to", "alternative for", "alternatives to",
		"alternatives for", "alternatives", "alternative", "substitute for", "substitute of", "substitute", "cheaper",
		"generic of", "same salt as", "same salt", "similar to", "show me", "is there a", "any", "?",
	}
	q := text
	for _, s := range stop {
		q = strings.ReplaceAll(q, s, " ")
	}
	return strings.TrimSpace(strings.Join(strings.Fields(q), " "))
}
