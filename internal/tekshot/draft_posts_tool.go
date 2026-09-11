package tekshot

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	toolName           = "tekshot_generate_draft_posts"
	finalToolName      = "submit_draft_batch"
	defaultTimezone    = "Asia/Ho_Chi_Minh"
	maxStructuredPosts = 200
)

var (
	isoDateTimePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}$`)
	isoDatePattern     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	hourMinutePattern  = regexp.MustCompile(`^\d{2}:\d{2}$`)
)

type DraftPostsTool struct {
	router *agent.Router
}

type SourceItem struct {
	SourceIndex   int
	ChecklistItem string
	SourceTitle   string
	SourceBrief   string
	SourceText    string
}

func tekshotDraftResearchToolAllow() []string {
	return []string{
		"skill_search",
		"vault_search",
		"vault_read",
		"web_search",
		"web_fetch",
		"memory_search",
		"memory_get",
		"memory_expand",
		"knowledge_graph_search",
		"read_document",
		"read_image",
		"datetime",
	}
}
func NewDraftPostsTool(router *agent.Router) *DraftPostsTool {
	return &DraftPostsTool{router: router}
}

func (t *DraftPostsTool) Name() string { return toolName }

func (t *DraftPostsTool) Description() string {
	return "Generate a strict structured Tekshot draft post batch by orchestrating an agent and returning validated JSON."
}

func (t *DraftPostsTool) HiddenFromLLM() bool { return true }

func (t *DraftPostsTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent_key": map[string]any{
				"type":        "string",
				"description": "Target GoClaw agent key that should plan the batch.",
			},
			"session_key": map[string]any{
				"type":        "string",
				"description": "Stable session key for the delegated planning run.",
			},
			"source_type": map[string]any{
				"type":        "string",
				"description": "Checklist source type: text, link, or file.",
			},
			"source_text": map[string]any{
				"type":        "string",
				"description": "Resolved source text when available.",
			},
			"source_items": map[string]any{
				"type":        "array",
				"description": "Exact checklist source items for this chunk. The final batch must return one post per source_index.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"source_index":   map[string]any{"type": "integer"},
						"checklist_item": map[string]any{"type": "string"},
						"source_title":   map[string]any{"type": "string"},
						"source_brief":   map[string]any{"type": "string"},
						"source_text":    map[string]any{"type": "string"},
					},
				},
			},
			"expected_count": map[string]any{
				"type":        "number",
				"description": "Expected number of posts for this request.",
			},
			"chunk_index": map[string]any{
				"type":        "number",
				"description": "1-based chunk number for chunked Tekshot generation.",
			},
			"chunk_count": map[string]any{
				"type":        "number",
				"description": "Total chunk count for chunked Tekshot generation.",
			},
			"parent_job_uuid": map[string]any{
				"type":        "string",
				"description": "Drupal parent job UUID for chunked Tekshot generation.",
			},
			"source_url": map[string]any{
				"type":        "string",
				"description": "Original cloud document URL when the source came from a link.",
			},
			"source_media": map[string]any{
				"type":        "array",
				"description": "Uploaded GoClaw media references for file-based source material.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":      map[string]any{"type": "string"},
						"mime_type": map[string]any{"type": "string"},
						"filename":  map[string]any{"type": "string"},
					},
				},
			},
			"file_name": map[string]any{
				"type":        "string",
				"description": "Human-readable file name for file-based checklist source.",
			},
			"instructions": map[string]any{
				"type":        "string",
				"description": "Full Tekshot instructions telling the agent how to plan the posts.",
			},
			"timezone": map[string]any{
				"type":        "string",
				"description": "IANA timezone used when the agent schedules posts.",
			},
		},
		"required":             []string{"agent_key", "instructions"},
		"additionalProperties": false,
	}
}

func (t *DraftPostsTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	agentKey := strings.TrimSpace(stringArg(args, "agent_key"))
	if agentKey == "" {
		return tools.ErrorResult("agent_key is required")
	}
	if t.router == nil {
		return tools.ErrorResult("agent router is not configured")
	}

	ag, err := t.router.Get(ctx, agentKey)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to resolve agent %q: %v", agentKey, err))
	}

	sessionKey := strings.TrimSpace(stringArg(args, "session_key"))
	if sessionKey == "" {
		sessionKey = "tekshot:draft:" + uuid.NewString()
	}
	timezone := strings.TrimSpace(stringArg(args, "timezone"))
	if timezone == "" {
		timezone = defaultTimezone
	}

	profile := pageProfileFromRequest(args)
	collector := NewDraftBatchCollectorTool(sourceItemsArg(args["source_items"])).withProfile(profile)
	userID := store.UserIDFromContext(ctx)
	writerArgs := maps.Clone(args)
	writerArgs["researched_facts"] = renderFactSheet(researchDraftFacts(ctx, ag, args, userID, sessionKey))
	runReq := draftRunRequest(writerArgs, timezone, userID, sessionKey, collector)

	if _, err := ag.Run(ctx, runReq); err != nil && collector.Batch() == nil {
		return tools.ErrorResult(fmt.Sprintf("draft generation run failed: %v", err))
	}

	if collector.Batch() == nil {
		finalReq := runReq
		finalReq.RunID = uuid.NewString()
		finalReq.Message = fmt.Sprintf("Submit the final Tekshot batch now by calling %s with the complete structured result. Do not answer with plain text.", finalToolName)
		// ToolChoice is applied on every LLM iteration. One forced iteration is
		// enough to collect the final batch and prevents later calls overwriting a
		// previously valid submission.
		finalReq.MaxIterations = 1
		finalReq.ToolChoice = &providers.ToolChoice{
			Mode: "function",
			Name: finalToolName,
		}
		if _, err := ag.Run(ctx, finalReq); err != nil && collector.Batch() == nil {
			return tools.ErrorResult(fmt.Sprintf("final structured submission failed: %v", err))
		}
	}

	if collector.Batch() == nil {
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: agent did not submit a valid structured draft batch")
	}
	runDraftReview(ctx, ag, runReq, collector)
	batch := collector.Batch()

	// Prompt D: lop kiem tra THU HAI, tach khoi luot viet. Chi chay khi trang
	// da bat luat va bai khong phai thuan thong tin — bai THONG_TIN khong mang
	// nghia vu quang cao nen khong co gi de soat.
	applyComplianceOverlay(ctx, ag, sessionKey, store.UserIDFromContext(ctx), profile, batch)

	encoded, err := json.Marshal(batch)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to encode structured draft batch: %v", err))
	}

	return &tools.Result{
		ForLLM:            string(encoded),
		StructuredContent: batch,
		Metadata: map[string]any{
			"agent_key":   agentKey,
			"session_key": sessionKey,
			"source_type": stringArg(args, "source_type"),
			"timezone":    timezone,
		},
	}
}

// draftRunRequest builds the writing run. LightContext drops the agent's chat
// persona files (SOUL/AGENTS/USER.md, NO_REPLY and cron rules): in a caption
// run they were most of the prompt and the source row a sliver of it. The
// writer gets no research tools — facts come from the research pass — and one
// forced submission, since ToolChoice re-applies every iteration and a second
// one could overwrite a valid post.
func draftRunRequest(args map[string]any, timezone, userID, sessionKey string, collector *DraftBatchCollectorTool) agent.RunRequest {
	return agent.RunRequest{
		SessionKey:     sessionKey,
		Message:        draftWriterPersona(args) + "\n\n" + buildPrompt(args, timezone) + buildGovernancePrompt(pageProfileFromRequest(args)),
		Media:          mediaFilesArg(args["source_media"]),
		Channel:        "tekshot_job",
		ChannelType:    "tekshot",
		ChatID:         userID,
		PeerKind:       "direct",
		Addressed:      true,
		RunID:          uuid.NewString(),
		UserID:         userID,
		SenderID:       userID,
		ToolAllow:      []string{draftWriteNoTools},
		EphemeralTools: []tools.Tool{collector},
		ToolChoice:     &providers.ToolChoice{Mode: "function", Name: finalToolName},
		MaxIterations:  1,
		SkillFilter:    []string{},
		LightContext:   true,
		TraceName:      "tekshot draft posts",
		TraceTags:      []string{"tekshot", "draft_posts"},
	}
}

// draftWriterPersona replaces the chat persona LightContext drops with the one
// this run actually needs: the page's own editor writing from the given row.
func draftWriterPersona(args map[string]any) string {
	page := ""
	if workspace, ok := args["workspace"].(map[string]any); ok {
		page = strings.TrimSpace(stringArg(workspace, "label"))
	}
	var sb strings.Builder
	if page != "" {
		sb.WriteString(fmt.Sprintf("Bạn là biên tập viên nội dung của page Facebook \"%s\".", page))
	} else {
		sb.WriteString("Bạn là biên tập viên nội dung của một page Facebook.")
	}
	sb.WriteString(" Lượt này chỉ có một việc: viết caption cho dòng checklist được giao.")
	sb.WriteString(" Những gì dòng checklist còn thiếu đã được tra sẵn ở khối RESEARCHED FACTS bên dưới — dùng chúng như kiến thức của chính bạn.")
	sb.WriteString(" Viết như người làm nghề viết cho page của mình: đi thẳng vào điều tiêu đề hứa, dùng đúng số liệu, tên, bước làm từ dòng checklist và khối dữ kiện đó;")
	sb.WriteString(" có giọng văn, có mở bài, có nối ý; không viết câu chung chung đặt vào page nào cũng đúng; không bịa dữ kiện.")
	return sb.String()
}

type DraftBatchCollectorTool struct {
	batch       map[string]any
	sourceItems []SourceItem
	// nil = page chua co PAGE_PROFILE: schema va validator giu nguyen hinh
	// dang cu, bai viet ra y het truoc. Do la cach luat moi len ma khong vo
	// page dang chay.
	profile *pageProfile
}

func NewDraftBatchCollectorTool(sourceItems ...[]SourceItem) *DraftBatchCollectorTool {
	var items []SourceItem
	if len(sourceItems) > 0 {
		items = sourceItems[0]
	}
	return &DraftBatchCollectorTool{sourceItems: items}
}

// withProfile bat lop tuan thu cho lot viet nay.
func (t *DraftBatchCollectorTool) withProfile(profile *pageProfile) *DraftBatchCollectorTool {
	t.profile = profile
	return t
}

func (t *DraftBatchCollectorTool) Name() string { return finalToolName }

func (t *DraftBatchCollectorTool) Description() string {
	return "Submit the final Tekshot draft post batch as validated structured JSON. Call this once when the batch is complete."
}

func (t *DraftBatchCollectorTool) Parameters() map[string]any {
	schema := t.baseParameters()
	if t.profile == nil {
		return schema
	}
	// Page da bat luat: gan them truong tuan thu vao TUNG bai. Description cua
	// truong rang buoc manh hon van prompt, nen day moi la cho dat luat.
	posts, ok := schema["properties"].(map[string]any)["posts"].(map[string]any)
	if !ok {
		return schema
	}
	items, ok := posts["items"].(map[string]any)
	if !ok {
		return schema
	}
	props, ok := items["properties"].(map[string]any)
	if !ok {
		return schema
	}
	for key, definition := range governancePostProperties() {
		props[key] = definition
	}
	required, _ := items["required"].([]string)
	items["required"] = append(append([]string{}, required...), governanceRequiredFields()...)
	return schema
}

func (t *DraftBatchCollectorTool) baseParameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "Short batch title.",
			},
			"summary": map[string]any{
				"type":        "string",
				"description": "Short summary of what this batch covers.",
			},
			"posts": map[string]any{
				"type":        "array",
				"description": "Complete set of generated posts.",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						// Drupal's guard accepts the title when the WORDS match the source;
						// case changes and edge symbols pass. Without saying so here the
						// model reads the surrounding "echo unchanged" rules literally and
						// returns a bare copy, so the published headline carries no icon.
						"title": map[string]any{
							"type": "string",
							"description": "The post headline. Keep the SAME WORDS as source_title — do not rewrite, translate, shorten or reorder them. " +
								"You SHOULD put it in upper case and add exactly ONE icon at the very start that matches the offer (and optionally one at the very end). " +
								"Changing the words is rejected; changing case and adding edge icons is expected.",
						},
						"brief":  map[string]any{"type": "string"},
						"pillar": map[string]any{"type": "string"},
						// Field descriptions bind far harder than prompt text because they
						// attach to the slot being filled. A single promo template here once
						// (800-1000 chars, sensory clauses per item, a "sell the feeling"
						// beat) turned every post type into a padded food ad: 72% of posts
						// landed at 800-1200 chars and recipes lost their steps.
						"content": map[string]any{
							"type": "string",
							"description": "The complete Facebook caption body, ready to publish — written the way a skilled human copywriter for this page would write it. " +
								"MATERIAL = the source row + the RESEARCHED FACTS block in the prompt, looked up for you in a separate pass. Use the researched facts that help the reader do or understand what the title promises, as if you knew them; skip trivia (nutrition tables, device models, timing footnotes); never invent a fact beyond them, and what is listed as NOT FOUND stays out. " +
								"When a researched fact disagrees with the checklist row, the checklist wins and the other is dropped — never discuss sources or discrepancies in the post. " +
								"LENGTH FOLLOWS THE MATERIAL: carry every useful fact you have and stop. Never pad to reach a length, never drop a fact to stay short. " +
								"NEVER mention the source, the checklist, your research or what is missing — the reader sees only the post; a fact you cannot confirm is simply left out. " +
								"Research in any language, but write for this page's readers: metric units (g, ml, muỗng canh, muỗng cà phê, °C) and Vietnamese kitchen and business terms — never cup, tablespoon, °F or untranslated English words. " +
								"SHAPE BY POST TYPE, decided from the source: " +
								"RECIPE / HOW-TO — an opening that names the result and who it suits; the ingredients with exact quantities, one per line, plus a short practical note only where it helps (how to prep it), never sensory adjectives; every step numbered in order; tips when you have them; a CTA. " +
								"OFFER / MENU / PRICE — a hook naming a concrete moment and the price or offer; one line per item with its concrete details; one CTA carrying a verb, a channel and a time or place. " +
								"KNOWLEDGE / B2B / RECRUITMENT / STORY — open on the specific problem, question or moment the title names; develop each point with its concrete fact (number, threshold, example, name); end on one takeaway and a CTA that fits. " +
								"EVERY SENTENCE does a job: it carries a fact from your material, hooks the reader into the exact problem or result, links two points, or asks for action. What is banned is the sentence that would fit any other page unchanged (generic scene-setting, stock feelings, stock praise) — not the voice. The post must read like a post, not a spec sheet. " +
								"EMOJI: one leading each list header and each list item, optionally one on the CTA line. Never mid-sentence, never two adjacent. " +
								"When the source brief already supplies wording, address form, slang or a CTA sentence, KEEP them verbatim and build around them — expand and format, do not reword into neutral prose. " +
								"Write in the language of the source. Blank line between blocks so it scans on a phone.",
						},
						"hashtags":       map[string]any{"type": "string", "description": "2-3 SUPPLEMENTARY hashtags for this post, space-separated, each starting with #, in the language of the source. Name what is specific to this post — the product, the offer, the occasion. The brand, branch and category tags already live in the page footer and are appended automatically, so never repeat them. Use empty string if none."},
						"publish_at":     map[string]any{"type": "string", "description": "Publish date and time. Must strictly match 'YYYY-MM-DDTHH:MM:SS' (e.g. 2026-06-21T15:30:00). Do NOT include timezone offsets or Z suffix."},
						"publish_date":   map[string]any{"type": "string", "description": "Publish date. Must match 'YYYY-MM-DD' (e.g. 2026-06-21)."},
						"publish_time":   map[string]any{"type": "string", "description": "Publish time in 24-hour format. Must match 'HH:MM' (e.g. 15:30)."},
						"checklist_item": map[string]any{"type": "string"},
						"source_index":   map[string]any{"type": "integer", "description": "Required when source_items is provided. Must match the source_index of the checklist item used for this post."},
					},
					"required": []string{
						"title", "brief", "pillar", "content", "hashtags",
						"publish_at", "publish_date", "publish_time", "checklist_item",
					},
				},
			},
		},
		"required":             []string{"title", "summary", "posts"},
		"additionalProperties": false,
	}
}

func (t *DraftBatchCollectorTool) Execute(_ context.Context, args map[string]any) *tools.Result {
	batch, err := validateDraftBatch(args, t.profile != nil)
	if err != nil {
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: " + err.Error())
	}
	if err := validateBatchSourceIndexes(batch, t.sourceItems); err != nil {
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: " + err.Error())
	}
	t.batch = batch
	return tools.SilentResult("Structured draft batch captured.")
}

func (t *DraftBatchCollectorTool) Batch() map[string]any {
	if t.batch == nil {
		return nil
	}
	return cloneBatch(t.batch)
}

func buildPrompt(args map[string]any, timezone string) string {
	sourceItems := sourceItemsArg(args["source_items"])
	isSinglePost := len(sourceItems) == 1
	var sb strings.Builder
	if isSinglePost {
		sb.WriteString("You are Tekshot Studio's social-content writer. Write one complete, ready-to-publish social post from the single source record below.\n")
		sb.WriteString("When the post is complete, call submit_draft_batch exactly once with the final structured result.\n")
	} else {
		sb.WriteString("You are generating Tekshot Studio draft social posts.\n")
		sb.WriteString("Study every source record carefully and call submit_draft_batch exactly once with the final structured result.\n")
	}
	sb.WriteString("Do not return the final batch as plain text.\n")
	sb.WriteString("Keep every post grounded in the source material. Use empty strings for unknown optional fields and never omit required fields.\n")
	sb.WriteString("Your material is the source records and the RESEARCHED FACTS block below; facts were looked up in a separate pass, so write from them directly.\n")
	sb.WriteString("Do not invent page, brand, product, service, policy, pricing, FAQ, availability, or promotion facts that are not supported by the source records or the researched facts.\n")
	sb.WriteString("Scheduling timezone: ")
	sb.WriteString(timezone)
	sb.WriteString("\n\n")

	if instructions := strings.TrimSpace(stringArg(args, "instructions")); instructions != "" {
		sb.WriteString("Tekshot instructions:\n")
		sb.WriteString(instructions)
		sb.WriteString("\n\n")
	}

	if sourceType := strings.TrimSpace(stringArg(args, "source_type")); sourceType != "" {
		sb.WriteString("Source type: ")
		sb.WriteString(sourceType)
		sb.WriteString("\n")
	}
	if sourceURL := strings.TrimSpace(stringArg(args, "source_url")); sourceURL != "" {
		sb.WriteString("Source URL: ")
		sb.WriteString(sourceURL)
		sb.WriteString("\n")
	}
	if fileName := strings.TrimSpace(stringArg(args, "file_name")); fileName != "" {
		sb.WriteString("Source file name: ")
		sb.WriteString(fileName)
		sb.WriteString("\n")
	}
	// A chunked child carries its canonical source through source_items. Printing
	// source_text as well used to make the model read the same row twice.
	if sourceText := strings.TrimSpace(stringArg(args, "source_text")); sourceText != "" && len(sourceItems) == 0 {
		sb.WriteString("\nResolved source text:\n")
		sb.WriteString(sourceText)
		sb.WriteString("\n")
	}
	if len(sourceItems) > 0 {
		if isSinglePost {
			sb.WriteString("\nSINGLE-POST CONTENT WRITER CONTRACT:\n")
			sb.WriteString("- The only creative output fields are content and hashtags. Treat title, brief, checklist_item, source_index, pillar, and scheduling as source/workflow fields.\n")
			sb.WriteString("- The content field must be the exact public caption for readers. Do not address the operator, requester, or chat user.\n")
			sb.WriteString("- Do NOT start content with the title. Drupal prepends the title when publishing, so a title line inside content is printed twice on the live post. Begin content at the opening sentence.\n")
			sb.WriteString("- Do not explain your process or add assistant follow-up offers. A final sentence is allowed only when it is a public CTA for the reader.\n")
			sb.WriteString("- The title and brief FIELDS are read-only: return them exactly from the source record, never rewritten, translated or corrected. This constrains those two fields only.\n")
			sb.WriteString("- The content field is not constrained by that rule. It must develop the title and brief into a full caption, not restate them.\n")
			sb.WriteString("- If the brief is structured with labels such as Hook, Mở đầu, Nội dung, Benefit, or Kết & CTA, turn every applicable part into the caption. Do not flatten it into generic marketing copy and do not print the labels themselves.\n")
			sb.WriteString("- Open in the way the source supports. Do not force an insight, contrarian claim, pain point, or question when the source already gives a stronger promotional hook.\n")
			sb.WriteString("- Use concrete source details and a CTA that fits this exact post. Do not invent business facts, offers, prices, availability, or contact details.\n")
			sb.WriteString("- Do not include footer/signature information or hashtags in content. Return 2-3 supplementary hashtags as one space-separated hashtags string.\n")
			// Thứ tự làm việc, không phải luật viết: nội dung luật nằm trong
			// Content Writing Guidelines do Drupal gửi kèm instructions.
			sb.WriteString("- Work in this order: classify the post intent and pick the framework per the instructions; write the caption from the source record and the researched facts; run the instructions' self-check; only call submit_draft_batch after the self-check passes.\n")
		}
		sb.WriteString("\nSOURCE RECORDS:\n")
		for _, item := range sourceItems {
			sb.WriteString(fmt.Sprintf("- source_index: %d\n", item.SourceIndex))
			sb.WriteString("  checklist_item (read-only): ")
			sb.WriteString(item.ChecklistItem)
			sb.WriteString("\n  source_title (read-only): ")
			sb.WriteString(item.SourceTitle)
			sb.WriteString("\n  source_brief (read-only): ")
			sb.WriteString(item.SourceBrief)
			if supporting := sourceSupportingText(item); supporting != "" {
				sb.WriteString("\n  supporting_source:\n")
				sb.WriteString(indentPromptBlock(supporting, "    "))
			}
			sb.WriteString("\n")
		}
		if facts := strings.TrimSpace(stringArg(args, "researched_facts")); facts != "" {
			sb.WriteString("\n" + facts + "\n")
		}
		sb.WriteString("\nSTRICT source item rules:\n")
		sb.WriteString("- Return exactly one post for every source_index listed above.\n")
		sb.WriteString("- Every submitted post must include the matching numeric source_index.\n")
		sb.WriteString("- Do not skip, merge, duplicate, or invent source_index values.\n")
	}

	// Descriptions only — the draft job never generates images itself. Listing
	// the curated imagery lets briefs point at photos that actually exist, so
	// the later image_chat step can reuse them as references.
	if refLibrary := referenceLibraryFromRequest(args); len(refLibrary) > 0 {
		sb.WriteString("\nReference image library (curated store imagery — planning context only):\n")
		for _, item := range refLibrary {
			sb.WriteString(fmt.Sprintf("- [%d] %s\n", item.ID, item.Description))
		}
		sb.WriteString("When a post needs a visual, prefer briefs whose imagery matches one of these descriptions so the image step can reuse the real photo. Never claim an image exists that is not listed above.\n")
	}

	sb.WriteString("\nIMPORTANT content rules:\n")
	// "ONLY the core post body text" read as a size instruction and shrank the
	// caption; the actual intent is just to exclude the footer block, which the
	// system appends at publish time.
	sb.WriteString("- The 'content' field is the complete, publish-ready caption body — write it in full. Exclude only the footer block: contact information, company address, phone number, email, website URL, and brand hashtags.\n")
	sb.WriteString("- The footer/signature block is managed separately by the system and will be appended automatically.\n")
	// The footer already ships the brand, branch and category tags, so anything
	// generated here is supplementary — 3-5 more pushed the published post to
	// ~8 tags. A concrete foreign-language example here also biased output, so
	// the shape is given abstractly instead.
	sb.WriteString("- Generate 2-3 SUPPLEMENTARY hashtags naming what is specific to this post (the product, the offer, the occasion) and place them in the 'hashtags' field, space-separated, each starting with #, in the language of the source. Shape: #<Product> #<OfferOrOccasion>.\n")
	sb.WriteString("- The footer already carries the brand, branch and category hashtags and is appended automatically — never repeat a brand, location or generic industry tag here.\n")
	sb.WriteString("\nFinal output schema requirements:\n")
	sb.WriteString("- title: short batch title\n")
	sb.WriteString("- summary: short batch summary\n")
	sb.WriteString("- posts: array of objects with these fields: title, brief, pillar, content, hashtags, publish_at, publish_date, publish_time, checklist_item, source_index\n")
	sb.WriteString("  * source_index: numeric source item index. Required when exact source items are provided.\n")
	sb.WriteString("  * publish_at must strictly use format 'YYYY-MM-DDTHH:MM:SS' (e.g., 2026-06-21T18:00:00). NO timezone offset (+07:00) or Z suffix allowed.\n")
	sb.WriteString("  * publish_date must use format 'YYYY-MM-DD' (e.g., 2026-06-21).\n")
	sb.WriteString("  * publish_time must use format 'HH:MM' (e.g., 18:00).\n")
	sb.WriteString("- If scheduling data is unavailable, set publish_at, publish_date, and publish_time to empty strings.\n")
	return sb.String()
}

// sourceSupportingText removes the canonical title/brief prefix from the
// labelled source row. It leaves scheduling, pillar, and other source facts
// available without showing the editorial source twice.
func sourceSupportingText(item SourceItem) string {
	text := strings.TrimSpace(item.SourceText)
	if text == "" {
		return ""
	}
	prefix := ""
	if item.SourceTitle != "" {
		prefix += "Title: " + item.SourceTitle
	}
	if item.SourceBrief != "" {
		if prefix != "" {
			prefix += "\n"
		}
		prefix += "Brief: " + item.SourceBrief
	}
	if prefix != "" {
		text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
	}
	return text
}

func indentPromptBlock(value, prefix string) string {
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func mediaFilesArg(raw any) []bus.MediaFile {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	media := make([]bus.MediaFile, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		path := strings.TrimSpace(stringArg(entry, "path"))
		if path == "" {
			continue
		}
		media = append(media, bus.MediaFile{
			Path:     path,
			MimeType: strings.TrimSpace(stringArg(entry, "mime_type")),
			Filename: strings.TrimSpace(stringArg(entry, "filename")),
		})
	}
	return media
}

func sourceItemsArg(raw any) []SourceItem {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}

	out := make([]SourceItem, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		sourceIndex, ok := integerNumberArg(entry, "source_index")
		if !ok || sourceIndex <= 0 {
			continue
		}
		checklistItem := strings.TrimSpace(stringArg(entry, "checklist_item"))
		if checklistItem == "" {
			continue
		}
		out = append(out, SourceItem{
			SourceIndex:   sourceIndex,
			ChecklistItem: checklistItem,
			SourceTitle:   strings.TrimSpace(stringArg(entry, "source_title")),
			SourceBrief:   strings.TrimSpace(stringArg(entry, "source_brief")),
			SourceText:    strings.TrimSpace(stringArg(entry, "source_text")),
		})
	}
	return out
}

func validateDraftBatch(args map[string]any, governed bool) (map[string]any, error) {
	requiredRootKeys := map[string]bool{
		"title": true, "summary": true, "posts": true,
	}
	if !sameKeys(args, requiredRootKeys) {
		return nil, fmt.Errorf("batch must contain exactly title, summary, and posts")
	}

	title := strings.TrimSpace(stringArg(args, "title"))
	summary := strings.TrimSpace(stringArg(args, "summary"))
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if summary == "" {
		return nil, fmt.Errorf("summary is required")
	}

	postsRaw, ok := args["posts"].([]any)
	if !ok || len(postsRaw) == 0 {
		return nil, fmt.Errorf("posts must be a non-empty array")
	}
	if len(postsRaw) > maxStructuredPosts {
		return nil, fmt.Errorf("posts exceeds the maximum of %d items", maxStructuredPosts)
	}

	posts := make([]map[string]any, 0, len(postsRaw))
	for index, rawPost := range postsRaw {
		postMap, ok := rawPost.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("posts[%d] must be an object", index)
		}
		post, err := validateDraftPost(postMap, index, governed)
		if err != nil {
			return nil, err
		}
		posts = append(posts, post)
	}

	return map[string]any{
		"title":   title,
		"summary": summary,
		"posts":   posts,
	}, nil
}

func validateDraftPost(post map[string]any, index int, governed bool) (map[string]any, error) {
	requiredPostKeys := map[string]bool{
		"title": true, "brief": true, "pillar": true, "content": true, "hashtags": true,
		"publish_at": true, "publish_date": true, "publish_time": true, "checklist_item": true,
	}
	optionalPostKeys := map[string]bool{"source_index": true}
	if governed {
		for key := range governancePostProperties() {
			optionalPostKeys[key] = true
		}
	}
	for key := range post {
		if !requiredPostKeys[key] && !optionalPostKeys[key] {
			return nil, fmt.Errorf("posts[%d] contains unsupported field %q", index, key)
		}
	}
	for key := range requiredPostKeys {
		if _, ok := post[key]; !ok {
			return nil, fmt.Errorf("posts[%d] must contain title, brief, pillar, content, hashtags, publish_at, publish_date, publish_time, checklist_item", index)
		}
	}

	title := strings.TrimSpace(stringArg(post, "title"))
	content := strings.TrimSpace(stringArg(post, "content"))
	checklistItem := strings.TrimSpace(stringArg(post, "checklist_item"))
	// Bai cham lan ranh la KET QUA hop le, khong phai loi: no ve voi exception
	// va content rong, de nguoi duyet chon phuong an.
	exception, hasException := post["exception"].(map[string]any)
	blocked := governed && hasException && len(exception) > 0
	if title == "" {
		return nil, fmt.Errorf("posts[%d].title is required", index)
	}
	if content == "" && !blocked {
		return nil, fmt.Errorf("posts[%d].content is required", index)
	}
	if checklistItem == "" {
		return nil, fmt.Errorf("posts[%d].checklist_item is required", index)
	}

	publishAt := strings.TrimSpace(stringArg(post, "publish_at"))
	publishDate := strings.TrimSpace(stringArg(post, "publish_date"))
	publishTime := strings.TrimSpace(stringArg(post, "publish_time"))
	if err := validateSchedule(index, publishAt, publishDate, publishTime); err != nil {
		return nil, err
	}

	validated := map[string]any{
		"title":          title,
		"brief":          strings.TrimSpace(stringArg(post, "brief")),
		"pillar":         strings.TrimSpace(stringArg(post, "pillar")),
		"content":        content,
		"hashtags":       strings.TrimSpace(stringArg(post, "hashtags")),
		"publish_at":     publishAt,
		"publish_date":   publishDate,
		"publish_time":   publishTime,
		"checklist_item": checklistItem,
	}
	if _, ok := post["source_index"]; ok {
		sourceIndex, valid := integerNumberArg(post, "source_index")
		if !valid || sourceIndex <= 0 {
			return nil, fmt.Errorf("posts[%d].source_index must be a positive integer", index)
		}
		validated["source_index"] = sourceIndex
	}
	if governed {
		if err := validateGovernanceFields(post, index); err != nil {
			return nil, err
		}
		for key := range governancePostProperties() {
			if value, ok := post[key]; ok {
				validated[key] = value
			}
		}
	}
	return validated, nil
}

func validateBatchSourceIndexes(batch map[string]any, sourceItems []SourceItem) error {
	if len(sourceItems) == 0 {
		return nil
	}
	posts, ok := batch["posts"].([]map[string]any)
	if !ok {
		return fmt.Errorf("posts must be a validated array")
	}
	if len(posts) != len(sourceItems) {
		return fmt.Errorf("returned %d posts for %d source items", len(posts), len(sourceItems))
	}

	expected := make(map[int]string, len(sourceItems))
	for _, item := range sourceItems {
		expected[item.SourceIndex] = item.ChecklistItem
	}
	seen := make(map[int]bool, len(sourceItems))
	for i, post := range posts {
		sourceIndex, valid := integerNumberArg(post, "source_index")
		if !valid || sourceIndex <= 0 {
			return fmt.Errorf("posts[%d].source_index must be a positive integer", i)
		}
		if _, ok := expected[sourceIndex]; !ok {
			return fmt.Errorf("posts[%d].source_index %d is not in source_items", i, sourceIndex)
		}
		if seen[sourceIndex] {
			return fmt.Errorf("posts[%d].source_index %d is duplicated", i, sourceIndex)
		}
		seen[sourceIndex] = true
	}
	for sourceIndex := range expected {
		if !seen[sourceIndex] {
			return fmt.Errorf("missing source_index %d", sourceIndex)
		}
	}
	return nil
}

func validateSchedule(index int, publishAt, publishDate, publishTime string) error {
	emptyCount := 0
	for _, value := range []string{publishAt, publishDate, publishTime} {
		if value == "" {
			emptyCount++
		}
	}
	if emptyCount == 3 {
		return nil
	}
	if emptyCount != 0 {
		return fmt.Errorf("posts[%d] schedule fields must all be present or all empty", index)
	}

	if !isoDateTimePattern.MatchString(publishAt) {
		return fmt.Errorf("posts[%d].publish_at must match YYYY-MM-DDTHH:MM:SS", index)
	}
	if !isoDatePattern.MatchString(publishDate) {
		return fmt.Errorf("posts[%d].publish_date must match YYYY-MM-DD", index)
	}
	if !hourMinutePattern.MatchString(publishTime) {
		return fmt.Errorf("posts[%d].publish_time must match HH:MM", index)
	}
	parsedAt, err := time.Parse("2006-01-02T15:04:05", publishAt)
	if err != nil {
		return fmt.Errorf("posts[%d].publish_at is invalid: %v", index, err)
	}
	parsedDate, err := time.Parse("2006-01-02", publishDate)
	if err != nil {
		return fmt.Errorf("posts[%d].publish_date is invalid: %v", index, err)
	}
	parsedTime, err := time.Parse("15:04", publishTime)
	if err != nil {
		return fmt.Errorf("posts[%d].publish_time is invalid: %v", index, err)
	}
	if parsedAt.Format("2006-01-02") != parsedDate.Format("2006-01-02") {
		return fmt.Errorf("posts[%d] publish_at and publish_date do not match", index)
	}
	if parsedAt.Format("15:04") != parsedTime.Format("15:04") {
		return fmt.Errorf("posts[%d] publish_at and publish_time do not match", index)
	}
	return nil
}

func sameKeys(values map[string]any, required map[string]bool) bool {
	if len(values) != len(required) {
		return false
	}
	for key := range values {
		if !required[key] {
			return false
		}
	}
	return true
}

func stringArg(values map[string]any, key string) string {
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return value
}

func integerNumberArg(values map[string]any, key string) (int, bool) {
	raw, ok := values[key]
	if !ok || raw == nil {
		return 0, false
	}
	switch value := raw.(type) {
	case int:
		return value, true
	case int64:
		return int(value), int64(int(value)) == value
	case float64:
		if math.Trunc(value) != value {
			return 0, false
		}
		return int(value), true
	case json.Number:
		parsed, err := value.Int64()
		if err != nil || int64(int(parsed)) != parsed {
			return 0, false
		}
		return int(parsed), true
	default:
		return 0, false
	}
}

func cloneBatch(batch map[string]any) map[string]any {
	clone := make(map[string]any, len(batch))
	maps.Copy(clone, batch)
	if posts, ok := batch["posts"].([]map[string]any); ok {
		copiedPosts := make([]map[string]any, len(posts))
		for i, post := range posts {
			postClone := make(map[string]any, len(post))
			maps.Copy(postClone, post)
			copiedPosts[i] = postClone
		}
		clone["posts"] = copiedPosts
	}
	return clone
}
