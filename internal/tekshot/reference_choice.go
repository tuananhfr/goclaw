package tekshot

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// referenceChoiceMaxItems is the last-resort ceiling on the catalogue handed to
// the selection pass. Drupal caps the manifest first; this only guards against
// a studio whose library grew past what one prompt can usefully hold.
const referenceChoiceMaxItems = 60

// referenceChoiceIDPattern reads the id out of the selection reply. A regex
// rather than json.Unmarshal because models wrap the object in prose or fences.
var referenceChoiceIDPattern = regexp.MustCompile(`"id"\s*:\s*(\d+)`)

func capReferenceItems(items []referenceLibraryItem, max int) []referenceLibraryItem {
	if max <= 0 || len(items) <= max {
		return items
	}
	return items[:max]
}

// buildReferenceChoicePrompt renders the descriptions-only catalogue. No URL and
// no attachment: the whole point of this pass is to decide what to download.
func buildReferenceChoicePrompt(brief string, items []referenceLibraryItem) string {
	var sb strings.Builder
	sb.WriteString("Pick at most ONE reference image for the image brief below.\n")
	sb.WriteString("Judge from the catalogue descriptions alone — the images are not attached.\n\n")
	sb.WriteString("## Image brief\n")
	sb.WriteString(strings.TrimSpace(brief))
	sb.WriteString("\n\n## Catalogue\n")
	for _, item := range items {
		sb.WriteString(fmt.Sprintf("- id %d: %s\n", item.ID, item.Description))
	}
	sb.WriteString("\n## Answer format\n")
	sb.WriteString("Reply with ONLY this JSON object and nothing else: {\"id\": <chosen id>}\n")
	sb.WriteString("Answer {\"id\": 0} when no entry genuinely fits. Never force a choice.\n")
	return sb.String()
}

// parseReferenceChoice returns the chosen library id, or 0 for "no image fits".
// An id outside the manifest is refused: a hallucinated number must not decide
// which file gets downloaded.
func parseReferenceChoice(reply string, items []referenceLibraryItem) int {
	match := referenceChoiceIDPattern.FindStringSubmatch(reply)
	if match == nil {
		return 0
	}
	id, err := strconv.Atoi(match[1])
	if err != nil || id <= 0 {
		return 0
	}
	for _, item := range items {
		if item.ID == id {
			return id
		}
	}
	return 0
}
