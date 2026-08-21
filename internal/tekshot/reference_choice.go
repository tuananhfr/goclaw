package tekshot

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// referenceChoiceMaxItems is the last-resort ceiling on the catalogue handed to
// the selection pass. Drupal caps the manifest first; this only guards against
// a studio whose library grew past what one prompt can usefully hold.
const referenceChoiceMaxItems = 60

// referenceChoiceTimeout bounds this pass on its own: it runs inside the job's
// 12-minute deadline, which the main image turn still needs after it.
const referenceChoiceTimeout = 90 * time.Second

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

// referenceChoiceRawID reports the id the reply actually carried, and whether
// it carried one at all. Only the logging needs the distinction: {"id": 0} is a
// judgement, an unreadable reply is a malfunction.
func referenceChoiceRawID(reply string) (int, bool) {
	match := referenceChoiceIDPattern.FindStringSubmatch(reply)
	if match == nil {
		return 0, false
	}
	id, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, false
	}
	return id, true
}

// parseReferenceChoice returns the chosen library id, or 0 for "no image fits".
// An id outside the manifest is refused: a hallucinated number must not decide
// which file gets downloaded.
func parseReferenceChoice(reply string, items []referenceLibraryItem) int {
	id, ok := referenceChoiceRawID(reply)
	if !ok || id <= 0 {
		return 0
	}
	for _, item := range items {
		if item.ID == id {
			return id
		}
	}
	return 0
}

// chooseReferenceImage runs one text-only turn that reads the catalogue and
// names a single library image. No media and no tools: this turn exists to
// decide WHICH file is worth downloading, so attaching the library here would
// defeat it. Any failure degrades to "no library image", never to a guess.
func (s *JobService) chooseReferenceImage(
	ctx context.Context,
	loop agent.Agent,
	job *store.TekshotJob,
	brief string,
	items []referenceLibraryItem,
) referenceLibraryItem {
	if len(items) == 0 {
		return referenceLibraryItem{}
	}
	shortlist := capReferenceItems(items, referenceChoiceMaxItems)
	userID := "tekshot-" + job.ExternalUserID
	runID := uuid.NewString()
	choiceCtx, cancel := context.WithTimeout(ctx, referenceChoiceTimeout)
	defer cancel()
	result, err := loop.Run(choiceCtx, agent.RunRequest{
		// Fresh session per run: a reused choice session would accumulate the
		// previous answers and bias (or bloat) every later pick.
		SessionKey:  job.SessionKey + ":ref-choice:" + runID,
		Message:     buildReferenceChoicePrompt(brief, shortlist),
		Channel:     "tekshot_job",
		ChannelType: "tekshot",
		ChatID:      userID,
		PeerKind:    "direct",
		Addressed:   true,
		RunID:       runID,
		UserID:      userID,
		SenderID:    userID,
		// An empty slice is NOT "no tools": policy.go's group-allow step only
		// gates on len(ToolAllow) > 0, so []string{} is indistinguishable from
		// nil and would grant the full toolset. "datetime" is the harmless
		// allowlist used for the same reason in knowledgeLabelToolAllow.
		ToolAllow:     []string{"datetime"},
		MaxIterations: 1,
		// Keep the pass cheap and literal: the agent's own image skill ("always
		// call create_image") pushes the model into prose instead of the JSON
		// this parser needs.
		SkillFilter:  []string{},
		LightContext: true,
		HistoryLimit: 1,
		TraceName:    "tekshot reference choice",
		TraceTags:    []string{"tekshot", "reference_choice"},
	})
	if err != nil || result == nil {
		reason := "agent returned no result"
		if err != nil {
			reason = err.Error()
		}
		slog.Warn("tekshot: reference choice failed, generating without a library image",
			"job", job.ID.String(), "external", job.ExternalJobUUID, "reason", reason)
		return referenceLibraryItem{}
	}
	chosenID := parseReferenceChoice(result.Content, shortlist)
	for _, item := range shortlist {
		if item.ID == chosenID {
			slog.Info("tekshot: reference image chosen",
				"job", job.ID.String(), "external", job.ExternalJobUUID,
				"reference_image_id", item.ID, "catalogue_size", len(items))
			return item
		}
	}
	// Three different outcomes land here; an operator must be able to tell the
	// model's judgement apart from the model failing to answer the question.
	rawID, parsed := referenceChoiceRawID(result.Content)
	switch {
	case !parsed:
		slog.Warn("tekshot: reference choice reply carried no id, generating without a library image",
			"job", job.ID.String(), "external", job.ExternalJobUUID, "catalogue_size", len(items))
	case rawID > 0:
		slog.Warn("tekshot: reference choice named an id outside the catalogue, generating without a library image",
			"job", job.ID.String(), "external", job.ExternalJobUUID,
			"reference_image_id", rawID, "catalogue_size", len(items))
	default:
		slog.Info("tekshot: no library image fits the brief",
			"job", job.ID.String(), "external", job.ExternalJobUUID, "catalogue_size", len(items))
	}
	return referenceLibraryItem{}
}
