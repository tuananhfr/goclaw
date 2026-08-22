package tekshot

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func choiceFixture() []referenceLibraryItem {
	return []referenceLibraryItem{
		{ID: 12, URL: "https://x/a.jpg", Description: "Tô bún bò trên bàn gỗ."},
		{ID: 15, URL: "https://x/b.jpg", Description: "Mặt tiền quán buổi tối."},
	}
}

func TestCapReferenceItems(t *testing.T) {
	items := choiceFixture()
	if got := capReferenceItems(items, 5); len(got) != 2 {
		t.Fatalf("under the cap must pass through, got %d", len(got))
	}
	if got := capReferenceItems(items, 1); len(got) != 1 || got[0].ID != 12 {
		t.Fatalf("cap must keep the head of the list, got %+v", got)
	}
	if got := capReferenceItems(items, 0); len(got) != 2 {
		t.Fatalf("a non-positive cap must disable capping, got %d", len(got))
	}
}

func TestBuildReferenceChoicePromptListsEveryItem(t *testing.T) {
	prompt := buildReferenceChoicePrompt("Ảnh combo buổi sáng", choiceFixture())
	for _, want := range []string{
		"Ảnh combo buổi sáng",
		"id 12: Tô bún bò trên bàn gỗ.",
		"id 15: Mặt tiền quán buổi tối.",
		`{"id": 0}`,
		"Do not call any tools",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q\n---\n%s", want, prompt)
		}
	}
	// URL không được lộ: lượt này chọn bằng mô tả, không được tải gì.
	if strings.Contains(prompt, "https://x/a.jpg") {
		t.Fatalf("prompt must not carry image URLs\n---\n%s", prompt)
	}
}

func TestParseReferenceChoiceAcceptsPlainJSON(t *testing.T) {
	if got := parseReferenceChoice(`{"id": 15}`, choiceFixture()); got != 15 {
		t.Fatalf("expected 15, got %d", got)
	}
}

func TestParseReferenceChoiceAcceptsFencedJSON(t *testing.T) {
	reply := "Ảnh này hợp nhất.\n```json\n{\"id\": 12}\n```\n"
	if got := parseReferenceChoice(reply, choiceFixture()); got != 12 {
		t.Fatalf("expected 12, got %d", got)
	}
}

func TestParseReferenceChoiceRejectsUnknownID(t *testing.T) {
	if got := parseReferenceChoice(`{"id": 99}`, choiceFixture()); got != 0 {
		t.Fatalf("an id outside the manifest must be refused, got %d", got)
	}
}

func TestParseReferenceChoiceAcceptsUnquotedKey(t *testing.T) {
	// Codex-family models often emit JS-style objects without quoted keys.
	if got := parseReferenceChoice(`{id: 12}`, choiceFixture()); got != 12 {
		t.Fatalf("expected 12 for an unquoted key, got %d", got)
	}
	if got := parseReferenceChoice(`id: 15`, choiceFixture()); got != 15 {
		t.Fatalf("expected 15 for a bare key, got %d", got)
	}
}

func TestParseReferenceChoiceIgnoresWordsEndingInID(t *testing.T) {
	// Asserted on the raw reader so it holds whatever the fixture contains:
	// the word boundary, not the manifest check, is what must reject this.
	if _, parsed := referenceChoiceRawID(`candid: 9`); parsed {
		t.Fatal("a word merely ending in 'id' must not parse as the id key")
	}
	if _, parsed := referenceChoiceRawID(`{"reference_image_id": 9}`); parsed {
		t.Fatal("a longer key ending in '_id' must not parse as the id key")
	}
}

func TestParseReferenceChoiceHandlesDotsFallback(t *testing.T) {
	// loop_finalize.go substitutes "..." when the model returns empty content,
	// which is what a tool-call-only turn produces.
	if got := parseReferenceChoice("...", choiceFixture()); got != 0 {
		t.Fatalf("expected 0 for the dots fallback, got %d", got)
	}
}

func TestParseReferenceChoiceHandlesNoFit(t *testing.T) {
	for _, reply := range []string{`{"id": 0}`, "Không ảnh nào hợp cả.", ""} {
		if got := parseReferenceChoice(reply, choiceFixture()); got != 0 {
			t.Fatalf("reply %q must yield 0, got %d", reply, got)
		}
	}
}

// referenceChoiceMaxItems has a twin on the Drupal side: MANIFEST_MAX_ITEMS in
// StudioReferenceImageRepository.php, pinned by StudioReferenceManifestCapTest.
// The two caps must stay equal or one side trims a list the other kept.
func TestReferenceChoiceMaxItemsMatchesDrupalCap(t *testing.T) {
	if referenceChoiceMaxItems != 60 {
		t.Fatalf("referenceChoiceMaxItems must stay 60 to match MANIFEST_MAX_ITEMS in StudioReferenceImageRepository.php, got %d", referenceChoiceMaxItems)
	}
}

// fakeChoiceAgent captures the RunRequest. Embedding agent.Agent satisfies the
// methods this test never calls.
type fakeChoiceAgent struct {
	agent.Agent
	captured agent.RunRequest
	result   *agent.RunResult
	err      error
}

func (f *fakeChoiceAgent) Run(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
	f.captured = req
	return f.result, f.err
}

func choiceJob() *store.TekshotJob {
	return &store.TekshotJob{
		SessionKey:      "tekshot:sess-1",
		ExternalUserID:  "7",
		ExternalJobUUID: "job-uuid-1",
	}
}

func TestChooseReferenceImageRestrictsTools(t *testing.T) {
	fake := &fakeChoiceAgent{result: &agent.RunResult{Content: `{"id": 12}`}}
	job := choiceJob()
	(&JobService{}).chooseReferenceImage(context.Background(), fake, job, "Ảnh combo buổi sáng", choiceFixture())

	req := fake.captured
	// Two traps stack here. (1) An EMPTY ToolAllow is not "no tools": policy.go's
	// group-allow step gates on len(groupToolAllow) > 0, so []string{} reads the
	// same as nil and grants the full toolset. (2) Any REAL tool named here gets
	// called — gpt-5.4-mini spent its single iteration calling datetime instead
	// of answering. The sentinel is non-empty for the gate yet resolves to no
	// tool, so the provider request carries none.
	if len(req.ToolAllow) != 1 || req.ToolAllow[0] != referenceChoiceNoTools {
		t.Fatalf("ToolAllow must be exactly the no-tools sentinel, got %#v", req.ToolAllow)
	}
	if req.MaxIterations != 1 {
		t.Fatalf("expected MaxIterations 1, got %d", req.MaxIterations)
	}
	if len(req.Media) != 0 {
		t.Fatalf("the selection pass must attach nothing, got %d media files", len(req.Media))
	}
	if req.SessionKey == job.SessionKey {
		t.Fatalf("the selection pass must not reuse the job session, got %q", req.SessionKey)
	}
	// Cheap turn: the agent's image skill and context files would drown the JSON.
	if req.SkillFilter == nil || len(req.SkillFilter) != 0 {
		t.Fatalf("SkillFilter must be an empty (not nil) slice, got %#v", req.SkillFilter)
	}
	if !req.LightContext || req.HistoryLimit != 1 {
		t.Fatalf("expected LightContext=true HistoryLimit=1, got %v/%d", req.LightContext, req.HistoryLimit)
	}
}

func TestChooseReferenceImageDegradesOnError(t *testing.T) {
	fake := &fakeChoiceAgent{err: errors.New("provider exploded")}
	got := (&JobService{}).chooseReferenceImage(context.Background(), fake, choiceJob(), "brief", choiceFixture())
	if got.ID != 0 {
		t.Fatalf("a failed selection must degrade to no image, got %+v", got)
	}
}

func TestChooseReferenceImageRejectsHallucinatedID(t *testing.T) {
	fake := &fakeChoiceAgent{result: &agent.RunResult{Content: `{"id": 999}`}}
	got := (&JobService{}).chooseReferenceImage(context.Background(), fake, choiceJob(), "brief", choiceFixture())
	if got.ID != 0 {
		t.Fatalf("an id outside the shortlist must not be downloaded, got %+v", got)
	}
}

func TestChooseReferenceImagePicksNamedItem(t *testing.T) {
	fake := &fakeChoiceAgent{result: &agent.RunResult{Content: "Chọn ảnh này.\n```json\n{\"id\": 15}\n```"}}
	got := (&JobService{}).chooseReferenceImage(context.Background(), fake, choiceJob(), "brief", choiceFixture())
	if got.ID != 15 || got.URL != "https://x/b.jpg" {
		t.Fatalf("expected item 15, got %+v", got)
	}
}
