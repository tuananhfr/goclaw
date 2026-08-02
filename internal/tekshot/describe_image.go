package tekshot

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const describeImageMaxIterations = 4

// describeImageToolAllow: the run only has to LOOK at one attached image.
// read_image must stay available because in file-ref vision mode the main
// provider never sees the image inline and reaches it through that tool
// (agent/loop_input_media.go). Everything else would be a distraction.
func describeImageToolAllow() []string {
	return []string{"read_image"}
}

// runDescribeImage produces the one-time Vietnamese description of a
// reference-library image. The image arrives as a Drupal public URL; the
// agent loop's persistMedia downloads URL media into the run workspace, so no
// download code lives here.
func (s *JobService) runDescribeImage(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
	if s.agents == nil {
		return nil, "", fmt.Errorf("agent router is not configured")
	}
	imageURL := strings.TrimSpace(stringFromMap(request, "image_url"))
	if imageURL == "" {
		return nil, "", fmt.Errorf("describe_image requires image_url")
	}
	if !strings.HasPrefix(imageURL, "http://") && !strings.HasPrefix(imageURL, "https://") {
		return nil, "", fmt.Errorf("describe_image image_url must be an http(s) URL")
	}
	referenceImageID := int(numberFromMap(request, "reference_image_id"))

	loop, err := s.agents.Get(store.WithTenantID(ctx, store.MasterTenantID), job.AgentKey)
	if err != nil {
		return nil, "", err
	}

	userID := "tekshot-" + job.ExternalUserID
	runCtx := store.WithTenantID(ctx, store.MasterTenantID)
	runCtx = store.WithUserID(runCtx, userID)
	runCtx = store.WithAgentKey(runCtx, job.AgentKey)

	runReq := agent.RunRequest{
		SessionKey: job.SessionKey,
		Message:    buildDescribeImagePrompt(request),
		Media: []bus.MediaFile{{
			Path:     imageURL,
			Filename: fmt.Sprintf("ref-%d", referenceImageID),
		}},
		Channel:       "tekshot_job",
		ChannelType:   "tekshot",
		ChatID:        userID,
		PeerKind:      "direct",
		Addressed:     true,
		RunID:         uuid.NewString(),
		UserID:        userID,
		SenderID:      userID,
		ToolAllow:     describeImageToolAllow(),
		MaxIterations: describeImageMaxIterations,
		TraceName:     "tekshot describe image",
		TraceTags:     []string{"tekshot", "describe_image"},
	}

	result, err := loop.Run(runCtx, runReq)
	if err != nil {
		return nil, "", err
	}
	if result == nil {
		return nil, "", fmt.Errorf("agent returned no result")
	}
	description := strings.TrimSpace(result.Content)
	if description == "" {
		return nil, "", fmt.Errorf("agent returned an empty image description")
	}
	return map[string]any{
		"reference_image_id": referenceImageID,
		"description":        description,
	}, "Image described", nil
}

func buildDescribeImagePrompt(request map[string]any) string {
	pageName := strings.TrimSpace(stringFromMap(request, "facebook_page_name"))
	pageDesc := strings.TrimSpace(stringFromMap(request, "facebook_page_description"))

	var sb strings.Builder
	sb.WriteString("Look at the image attached to this message and write its catalogue description for a reference image library.\n")
	sb.WriteString("The description is later read by an AI to decide when this image fits a social post brief and to guide image generation that uses it as a visual reference.\n")
	sb.WriteString("If the image is not visible inline, read it with the read_image tool — it is attached to this message.\n\n")

	if pageName != "" || pageDesc != "" {
		sb.WriteString("## Page context\n")
		if pageName != "" {
			sb.WriteString("- Name: " + pageName + "\n")
		}
		if pageDesc != "" {
			sb.WriteString("- Description: " + pageDesc + "\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Hard rules\n")
	sb.WriteString("- Write in Vietnamese, 3-6 câu, một đoạn văn liền mạch.\n")
	sb.WriteString("- Cover: chủ thể chính; bối cảnh/không gian; màu sắc và ánh sáng chủ đạo; phong cách (ảnh chụp thật / đồ hoạ / minh hoạ); ảnh phù hợp cho loại bài đăng nào.\n")
	sb.WriteString("- Describe only what is actually visible. No hashtags, no marketing fluff, no assumptions about the brand.\n")
	sb.WriteString("- Reply with ONLY the description text: no preamble, no meta commentary, no code fences.\n")
	return sb.String()
}
