package tekshot

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const learnStyleMaxIterations = 4

// learnStyleToolAllow keeps the style run closed-book: the job DISTILLS the
// sample posts it was handed. Letting it browse or search would invite brand
// "facts" the samples do not support. datetime stays so date references in
// samples can be reasoned about, mirroring tekshotChecklistToolAllow.
func learnStyleToolAllow() []string {
	return []string{"datetime"}
}

// learnStyleSample is one sample post from the Drupal request. Drupal
// normalises both sources (recent published posts and hand-pasted text) into
// this same sample_posts shape before enqueueing, so this is the only shape
// the runner has to parse.
type learnStyleSample struct {
	Title    string
	Content  string
	Hashtags string
}

func learnStyleSamples(request map[string]any) []learnStyleSample {
	raw, ok := request["sample_posts"].([]any)
	if !ok {
		return nil
	}
	samples := make([]learnStyleSample, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		content := strings.TrimSpace(stringFromMap(entry, "content"))
		if content == "" {
			continue
		}
		samples = append(samples, learnStyleSample{
			Title:    strings.TrimSpace(stringFromMap(entry, "title")),
			Content:  content,
			Hashtags: strings.TrimSpace(stringFromMap(entry, "hashtags")),
		})
	}
	return samples
}

func (s *JobService) runLearnStyle(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
	if s.agents == nil {
		return nil, "", fmt.Errorf("agent router is not configured")
	}
	samples := learnStyleSamples(request)
	if len(samples) == 0 {
		return nil, "", fmt.Errorf("learn_style requires at least one sample post with content")
	}

	loop, err := s.agents.Get(store.WithTenantID(ctx, store.MasterTenantID), job.AgentKey)
	if err != nil {
		return nil, "", err
	}

	userID := "tekshot-" + job.ExternalUserID
	runCtx := store.WithTenantID(ctx, store.MasterTenantID)
	runCtx = store.WithUserID(runCtx, userID)
	runCtx = store.WithAgentKey(runCtx, job.AgentKey)

	runReq := agent.RunRequest{
		SessionKey:    job.SessionKey,
		Message:       buildLearnStylePrompt(request, samples),
		Channel:       "tekshot_job",
		ChannelType:   "tekshot",
		ChatID:        userID,
		PeerKind:      "direct",
		Addressed:     true,
		RunID:         uuid.NewString(),
		UserID:        userID,
		SenderID:      userID,
		ToolAllow:     learnStyleToolAllow(),
		MaxIterations: learnStyleMaxIterations,
		TraceName:     "tekshot learn style",
		TraceTags:     []string{"tekshot", "learn_style"},
	}

	result, err := loop.Run(runCtx, runReq)
	if err != nil {
		return nil, "", err
	}
	if result == nil {
		return nil, "", fmt.Errorf("agent returned no result")
	}
	styleGuide := strings.TrimSpace(result.Content)
	if styleGuide == "" {
		return nil, "", fmt.Errorf("agent returned an empty style guide")
	}
	return map[string]any{"style_guide": styleGuide}, "Style guide generated", nil
}

func buildLearnStylePrompt(request map[string]any, samples []learnStyleSample) string {
	pageName := strings.TrimSpace(stringFromMap(request, "facebook_page_name"))
	pageDesc := strings.TrimSpace(stringFromMap(request, "facebook_page_description"))
	styleSource := strings.TrimSpace(stringFromMap(request, "style_source"))

	var sb strings.Builder
	sb.WriteString("You are analysing a Facebook page's posts to distill the page's writing style.\n")
	sb.WriteString("Study the sample posts below and produce a practical style guide that a copywriter (or another AI) can follow to write NEW posts in exactly the same voice.\n\n")

	sb.WriteString("## Page\n")
	if pageName != "" {
		sb.WriteString("- Name: " + pageName + "\n")
	}
	if pageDesc != "" {
		sb.WriteString("- Description: " + pageDesc + "\n")
	}
	if styleSource != "" {
		sb.WriteString("- Sample source: " + styleSource + "\n")
	}

	sb.WriteString(fmt.Sprintf("\n## Sample posts (%d)\n", len(samples)))
	for i, sample := range samples {
		sb.WriteString(fmt.Sprintf("\n### Sample %d\n", i+1))
		if sample.Title != "" {
			sb.WriteString("Title: " + sample.Title + "\n")
		}
		sb.WriteString("Content:\n" + sample.Content + "\n")
		if sample.Hashtags != "" {
			sb.WriteString("Hashtags: " + sample.Hashtags + "\n")
		}
	}

	sb.WriteString("\n## Hard rules\n")
	sb.WriteString("- Write the ENTIRE style guide in Vietnamese — store staff read and edit it by hand.\n")
	sb.WriteString("- Cover, in this order: giọng văn và thái độ; cách xưng hô với khách; cấu trúc bài (mở - thân - kết); cách dùng emoji (mật độ, vị trí, bộ emoji quen thuộc); độ dài điển hình; phong cách hashtag; kiểu call-to-action đặc trưng; kết thúc bằng 1-2 trích đoạn mẫu ngắn lấy từ chính các bài trên.\n")
	sb.WriteString("- Ground EVERY observation in the samples above. Never invent products, prices, promotions or brand facts the samples do not show.\n")
	sb.WriteString("- Reply with ONLY the style guide text: no preamble, no meta commentary, no code fences.\n")
	return sb.String()
}
