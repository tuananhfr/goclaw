package tekshot

import (
	"fmt"
	"sort"
	"strings"
)

// Page goals Drupal may declare. They change what a "competitor" even is:
// a recruiting page competes for applicants, a community page competes for
// attention, and only a selling page competes for buyers.
const (
	goalSell      = "sell"
	goalRecruit   = "recruit"
	goalLeads     = "leads"
	goalBrand     = "brand"
	goalCommunity = "community"
)

// businessProfile is the declared identity of ONE research subject: a store
// or one of its pages. Drupal owns the vocabulary; everything here is
// optional, because a subject nobody has declared yet must still run.
type businessProfile struct {
	name        string
	goal        string
	kind        string
	description string
	notes       string
	offerings   []string
	channels    []string
	geoMode     string
	geoArea     string
	geoLat      float64
	geoLng      float64
	geoRadiusKm float64
	priceMin    float64
	priceMax    float64
	pos         map[string]any
	present     bool
}

// readBusinessProfile pulls the block Drupal sends alongside every market job.
// An absent block is not an error — callers fall back to their old wording.
func readBusinessProfile(request map[string]any) businessProfile {
	raw, ok := request["business_profile"].(map[string]any)
	if !ok || len(raw) == 0 {
		return businessProfile{}
	}

	profile := businessProfile{
		name:        strings.TrimSpace(stringFromMap(raw, "name")),
		goal:        strings.TrimSpace(stringFromMap(raw, "goal")),
		kind:        strings.TrimSpace(stringFromMap(raw, "kind")),
		description: strings.TrimSpace(stringFromMap(raw, "description")),
		notes:       strings.TrimSpace(stringFromMap(raw, "notes")),
		offerings:   stringsFromAny(raw["offerings"]),
		channels:    stringsFromAny(raw["channels"]),
		present:     true,
	}
	if profile.goal == "" {
		profile.goal = goalSell
	}

	if geo, ok := raw["geography"].(map[string]any); ok {
		profile.geoMode = strings.TrimSpace(stringFromMap(geo, "mode"))
		profile.geoArea = strings.TrimSpace(stringFromMap(geo, "area"))
		profile.geoLat = numberFromMap(geo, "lat")
		profile.geoLng = numberFromMap(geo, "lng")
		profile.geoRadiusKm = numberFromMap(geo, "radius_km")
	}
	if band, ok := raw["price_band"].(map[string]any); ok {
		profile.priceMin = numberFromMap(band, "min")
		profile.priceMax = numberFromMap(band, "max")
	}
	if pos, ok := raw["pos_snapshot"].(map[string]any); ok && len(pos) > 0 {
		profile.pos = pos
	}

	return profile
}

// subjectName is the identity to research. Falls back to the store name,
// which for stores grouped by category ("Xây dựng") is exactly why the
// declared profile exists.
func (p businessProfile) subjectName(storeName string) string {
	if p.name != "" {
		return p.name
	}
	return storeName
}

// writeProfile renders the declared subject into the prompt's store context.
// Reports whether anything was written, so the caller knows if it still needs
// its "infer the business from what you were given" line.
func (p businessProfile) writeProfile(sb *strings.Builder) bool {
	if !p.present {
		return false
	}

	sb.WriteString("- Business: " + p.description + "\n")
	if p.kind != "" {
		sb.WriteString("- Business type: " + p.kind + "\n")
	}
	sb.WriteString("- This subject exists to: " + goalSentence(p.goal) + "\n")

	if len(p.offerings) > 0 {
		sb.WriteString("- " + offeringsLabel(p.goal) + ": " + strings.Join(p.offerings, "; ") + "\n")
	}
	if band := p.priceSentence(); band != "" {
		sb.WriteString("- " + band + "\n")
	}
	if len(p.channels) > 0 {
		sb.WriteString("- Channels: " + strings.Join(p.channels, ", ") + "\n")
	}
	sb.WriteString("- Operating area: " + p.geoSentence() + "\n")
	if p.notes != "" {
		sb.WriteString("- Team notes: " + p.notes + "\n")
	}
	p.writePosSnapshot(sb)

	return true
}

// writePosSnapshot adds live POS figures when the subject is allowed to carry
// them. Sales belong to the store, so Drupal only sends this for a subject
// that IS the outlet.
func (p businessProfile) writePosSnapshot(sb *strings.Builder) {
	if p.pos == nil {
		return
	}

	var parts []string
	if orders := numberFromMap(p.pos, "orders"); orders > 0 {
		days := numberFromMap(p.pos, "window_days")
		parts = append(parts, fmt.Sprintf("%.0f orders in the last %.0f days", orders, days))
	}
	if aov := numberFromMap(p.pos, "aov"); aov > 0 {
		parts = append(parts, fmt.Sprintf("average order %.0f VND", aov))
	}
	if hours := stringsFromAny(p.pos["peak_hours"]); len(hours) > 0 {
		parts = append(parts, "busiest hours "+strings.Join(hours, "h, ")+"h")
	}
	if len(parts) > 0 {
		sb.WriteString("- Actual sales: " + strings.Join(parts, "; ") + "\n")
	}

	if mix, ok := p.pos["receive_mix"].(map[string]any); ok && len(mix) > 0 {
		// Sorted, because Go randomises map order and a prompt that differs
		// run to run cannot be diffed or tested.
		labels := make([]string, 0, len(mix))
		for label := range mix {
			labels = append(labels, label)
		}
		sort.Strings(labels)

		shares := make([]string, 0, len(labels))
		for _, label := range labels {
			shares = append(shares, fmt.Sprintf("%s %.0f%%", label, numberFromMap(mix, label)))
		}
		sb.WriteString("- Order mix: " + strings.Join(shares, ", ") + "\n")
	}

	if top := topProductTitles(p.pos["top_products"]); len(top) > 0 {
		sb.WriteString("- Best sellers right now: " + strings.Join(top, "; ") + "\n")
	}
}

// geoSentence turns the declared geography into one readable line. The mode
// matters more than the numbers: it decides whether "nearby" means anything.
func (p businessProfile) geoSentence() string {
	switch p.geoMode {
	case "local":
		radius := p.geoRadiusKm
		if radius <= 0 {
			radius = 3
		}
		line := fmt.Sprintf("serves customers within about %.1f km of %.5f,%.5f", radius, p.geoLat, p.geoLng)
		if p.geoArea != "" {
			line += " (" + p.geoArea + ")"
		}
		return line
	case "area":
		if p.geoArea != "" {
			return "operates across " + p.geoArea + ", with no single walk-in catchment"
		}
		return "operates across a region rather than one neighbourhood"
	case "nationwide":
		return "operates nationwide / online, with no physical catchment"
	default:
		return "not declared"
	}
}

// priceSentence describes the money band, whose meaning follows the goal.
func (p businessProfile) priceSentence() string {
	if p.priceMin <= 0 && p.priceMax <= 0 {
		return ""
	}
	label := "Typical order value"
	if p.goal == goalRecruit {
		label = "Salary range offered"
	}
	switch {
	case p.priceMin > 0 && p.priceMax > 0:
		return fmt.Sprintf("%s: %.0f–%.0f VND", label, p.priceMin, p.priceMax)
	case p.priceMin > 0:
		return fmt.Sprintf("%s: from %.0f VND", label, p.priceMin)
	default:
		return fmt.Sprintf("%s: up to %.0f VND", label, p.priceMax)
	}
}

// goalSentence spells out what the subject is for, in the agent's own terms.
func goalSentence(goal string) string {
	switch goal {
	case goalRecruit:
		return "hire people — it competes for APPLICANTS, not for buyers"
	case goalLeads:
		return "collect leads and enquiries (partners, franchisees, sign-ups) — it competes for the same people filling in forms"
	case goalBrand:
		return "tell the brand's story — it competes for attention, not for orders"
	case goalCommunity:
		return "serve a professional community — it competes for attention within that niche"
	default:
		return "sell products or services"
	}
}

// offeringsLabel keeps the offerings list honest: the same column means
// different things depending on what the subject is for.
func offeringsLabel(goal string) string {
	switch goal {
	case goalRecruit:
		return "Roles being hired"
	case goalLeads:
		return "What people sign up for"
	case goalBrand, goalCommunity:
		return "Content topics"
	default:
		return "Main products/services"
	}
}

// stringsFromAny reads a JSON array of scalars into trimmed strings.
func stringsFromAny(value any) []string {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, item := range list {
		text := strings.TrimSpace(fmt.Sprintf("%v", item))
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

// topProductTitles reads the POS snapshot's product list, titles only.
func topProductTitles(value any) []string {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, item := range list {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if title := strings.TrimSpace(stringFromMap(row, "title")); title != "" {
			out = append(out, title)
		}
	}
	return out
}
