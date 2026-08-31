package tekshot

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const autoImageMaxAttempts = 3

// runAutoImage deliberately reuses the battle-tested image_chat runner for
// media attachment, reference-library handling and create_image delivery, but
// adds a bounded visual QA loop. The image model owns the typography: Drupal
// never paints a post-generation text overlay.
func (s *JobService) runAutoImage(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
	basePrompt := strings.TrimSpace(stringFromMap(request, "prompt"))
	if basePrompt == "" {
		return nil, "", fmt.Errorf("automated image prompt is required")
	}

	imageJob := *job
	imageJob.JobType = TekshotJobTypeImageChat
	lastNotes := ""
	for attempt := 1; attempt <= autoImageMaxAttempts; attempt++ {
		attemptRequest := cloneMap(request)
		prompt := basePrompt
		if lastNotes != "" {
			prompt += "\n\nPREVIOUS QA FEEDBACK — keep the same concept and fix exactly this defect; do not switch to a different visual route:\n" + lastNotes
		}
		attemptRequest["prompt"] = prompt
		result, _, err := s.runChat(ctx, &imageJob, attemptRequest)
		if err != nil {
			lastNotes = err.Error()
			continue
		}

		resultMap, ok := result.(map[string]any)
		if !ok || len(anySlice(resultMap["media"])) == 0 {
			lastNotes = "No usable image was returned."
			continue
		}
		qa := parseAutoImageQA(stringFromMap(resultMap, "content"))
		// An image exists but the reply carried no QA JSON (typical after the
		// forced create_image rescue pass). Run one QA-only turn on the same
		// session instead of discarding a paid, possibly fine image.
		if !qa.Parsed {
			qa = s.runAutoImageQATurn(ctx, job)
		}
		if qa.Passed {
			resultMap["qa_passed"] = true
			resultMap["qa_notes"] = qa.Notes
			resultMap["creative_plan"] = qa.CreativePlan
			resultMap["final_prompt"] = qa.FinalPrompt
			return resultMap, fmt.Sprintf("Automated image passed visual QA (%d/%d)", attempt, autoImageMaxAttempts), nil
		}
		lastNotes = qa.Notes
		if lastNotes == "" {
			lastNotes = "The candidate did not satisfy the strict visual QA contract."
		}
	}

	return nil, "", fmt.Errorf("automated image failed visual QA after %d attempts: %s", autoImageMaxAttempts, lastNotes)
}

// runAutoImageQATurn asks for the missing QA verdict on the image just
// generated. It reuses the job's own session so the QA CONTRACT and the exact
// headline from the original request are still in history; read_image is the
// only tool on offer, so the turn cannot generate a second image. Any failure
// degrades to "not parsed" and the attempt loop treats it as a QA fail.
func (s *JobService) runAutoImageQATurn(ctx context.Context, job *store.TekshotJob) autoImageQA {
	loop, err := s.agents.Get(store.WithTenantID(ctx, store.MasterTenantID), job.AgentKey)
	if err != nil {
		return autoImageQA{Notes: "QA follow-up could not resolve the agent: " + err.Error()}
	}
	userID := "tekshot-" + job.ExternalUserID
	qaCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := loop.Run(qaCtx, agent.RunRequest{
		SessionKey: job.SessionKey,
		Message: "[System] The image was generated but you did not return the QA JSON. " +
			"Perform the QA CONTRACT from the original request now: call read_image on the image you just created, " +
			"then reply with ONLY the JSON object {\"qa_passed\":true|false,\"qa_notes\":\"...\",\"creative_plan\":{...},\"final_prompt\":\"...\"}.",
		Channel:       "tekshot_job",
		ChannelType:   "tekshot",
		ChatID:        userID,
		PeerKind:      "direct",
		Addressed:     true,
		RunID:         uuid.NewString(),
		UserID:        userID,
		SenderID:      userID,
		ToolAllow:     []string{"read_image"},
		MaxIterations: 3,
		TraceName:     "tekshot auto image qa",
		TraceTags:     []string{"tekshot", "auto_image_qa"},
	})
	if err != nil || result == nil {
		reason := "agent returned no result"
		if err != nil {
			reason = err.Error()
		}
		return autoImageQA{Notes: "QA follow-up turn failed: " + reason}
	}
	return parseAutoImageQA(result.Content)
}

type autoImageQA struct {
	// Parsed separates "QA genuinely failed" from "the reply carried no QA JSON
	// at all" — the second case gets a follow-up QA turn instead of burning the
	// attempt (the forced create_image pass returns media with no JSON).
	Parsed       bool
	Passed       bool
	Notes        string
	CreativePlan map[string]any
	FinalPrompt  string
}

func parseAutoImageQA(content string) autoImageQA {
	content = strings.TrimSpace(content)
	if start := strings.Index(content, "{"); start >= 0 {
		if end := strings.LastIndex(content, "}"); end >= start {
			content = content[start : end+1]
		}
	}
	var decoded map[string]any
	if json.Unmarshal([]byte(content), &decoded) != nil {
		return autoImageQA{Notes: "The agent did not return the required JSON QA result."}
	}
	// qa_passed must actually be present — an arbitrary JSON blob is not a QA
	// verdict, and reading it as one would skip the follow-up turn.
	if _, ok := decoded["qa_passed"]; !ok {
		return autoImageQA{Notes: "The agent did not return the required JSON QA result."}
	}
	passed, _ := decoded["qa_passed"].(bool)
	notes, _ := decoded["qa_notes"].(string)
	finalPrompt, _ := decoded["final_prompt"].(string)
	plan, _ := decoded["creative_plan"].(map[string]any)
	return autoImageQA{Parsed: true, Passed: passed, Notes: strings.TrimSpace(notes), CreativePlan: plan, FinalPrompt: strings.TrimSpace(finalPrompt)}
}

func cloneMap(source map[string]any) map[string]any {
	copy := make(map[string]any, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

func anySlice(value any) []any {
	if values, ok := value.([]any); ok {
		return values
	}
	reflected := reflect.ValueOf(value)
	if reflected.IsValid() && reflected.Kind() == reflect.Slice {
		values := make([]any, reflected.Len())
		for index := range values {
			values[index] = reflected.Index(index).Interface()
		}
		return values
	}
	return nil
}
