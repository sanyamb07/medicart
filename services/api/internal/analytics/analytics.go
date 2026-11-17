// Package analytics is a minimal, privacy-conscious event log — not a
// general-purpose analytics platform. It exists to answer "do people find
// products, add them to cart, and complete checkout", not "what did this
// named person do": events carry an anonymous per-browser id, never a
// customer id, and property values are restricted to primitives so a caller
// cannot smuggle free-text (a typed search query, a name, anything that
// could be personal or medical) into the properties blob.
package analytics

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sanyam/medicart/services/api/internal/db"
)

// allowedEvents is a closed vocabulary. An unrecognised event name is
// rejected rather than silently stored, so the event list in docs stays the
// actual list of what the product measures.
var allowedEvents = map[string]bool{
	"search_performed":        true,
	"search_no_results":       true,
	"product_viewed":          true,
	"add_to_cart":             true,
	"cart_viewed":             true,
	"checkout_started":        true,
	"payment_initiated":       true,
	"payment_succeeded":       true,
	"payment_failed":          true,
	"order_completed":         true,
	"whatsapp_handoff_opened": true,
}

const (
	maxPropsPerEvent = 10
	maxPropValueLen  = 200
	maxBatchSize     = 20
)

type Event struct {
	Name       string         `json:"event"`
	Properties map[string]any `json:"properties"`
}

type Service struct{ pool *db.Pool }

func NewService(pool *db.Pool) *Service { return &Service{pool: pool} }

// Record validates and stores a batch of events in one insert. Invalid
// individual events are dropped (not an error for the whole batch) — a typo'd
// client-side event name should never surface as a user-visible failure.
func (s *Service) Record(ctx context.Context, anonID string, events []Event) (accepted int, err error) {
	if len(events) > maxBatchSize {
		events = events[:maxBatchSize]
	}
	if len(anonID) > 100 {
		anonID = anonID[:100]
	}

	for _, e := range events {
		if !allowedEvents[e.Name] {
			continue
		}
		props := sanitiseProps(e.Properties)
		raw, merr := json.Marshal(props)
		if merr != nil {
			continue
		}
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO analytics_events (event_name, anon_id, properties) VALUES ($1, $2, $3)`,
			e.Name, nullIfEmpty(anonID), raw); err != nil {
			return accepted, fmt.Errorf("insert event: %w", err)
		}
		accepted++
	}
	return accepted, nil
}

// sanitiseProps keeps only primitive-valued keys, bounded in count and
// length — the mechanism that makes "no free text" an enforced property
// rather than a convention callers might forget.
func sanitiseProps(in map[string]any) map[string]any {
	out := make(map[string]any, maxPropsPerEvent)
	n := 0
	for k, v := range in {
		if n >= maxPropsPerEvent {
			break
		}
		switch val := v.(type) {
		case string:
			if len(val) > maxPropValueLen {
				val = val[:maxPropValueLen]
			}
			out[k] = val
		case float64, bool, nil:
			out[k] = val
		default:
			continue // objects/arrays are not accepted
		}
		n++
	}
	return out
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
