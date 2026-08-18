package tekshot

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

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
			prompt += "\n\nPREVIOUS QA FEEDBACK (fix this and choose a noticeably different visual route):\n" + lastNotes
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

type autoImageQA struct {
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
	passed, _ := decoded["qa_passed"].(bool)
	notes, _ := decoded["qa_notes"].(string)
	finalPrompt, _ := decoded["final_prompt"].(string)
	plan, _ := decoded["creative_plan"].(map[string]any)
	return autoImageQA{Passed: passed, Notes: strings.TrimSpace(notes), CreativePlan: plan, FinalPrompt: strings.TrimSpace(finalPrompt)}
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
