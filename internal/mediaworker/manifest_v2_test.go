package mediaworker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Các test v2 dựng manifest từ JSON (đúng dây Drupal gửi) chứ không từ field
// Go, để chúng biên dịch được và ĐỎ trên mã 75f64097 thay vì không chạy được.

func decodeManifestJSON(t *testing.T, raw string) Manifest {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	return manifest
}

func decodeSceneJSON(t *testing.T, raw string) Scene {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var scene Scene
	if err := decoder.Decode(&scene); err != nil {
		t.Fatalf("decode scene: %v", err)
	}
	return scene
}

func snapshotV2(t *testing.T) Manifest {
	t.Helper()
	data, err := os.ReadFile("testdata/manifest-v2-97c8af86.json")
	if err != nil {
		t.Fatal(err)
	}
	return decodeManifestJSON(t, string(data))
}

// v2Manifest dựng manifest v2 một cảnh video 61 s cắt từ startMS, có thể chèn
// thêm field vào cảnh.
func v2Manifest(sceneExtra string, durationMS int) string {
	scene := `{"uuid":"edit-0-0","source_url":"https://drupal.test/src.mp4","mime_type":"video/mp4","duration_ms":` + itoa(durationMS) + sceneExtra + `}`
	return `{"contract_version":2,"project_uuid":"11111111-1111-4111-8111-111111111111","project_revision":3,"duration_ms":` + itoa(durationMS) +
		`,"scenes":[` + scene + `],"audio":[],"subtitles":[],"output":{"width":1080,"height":1920,"fps":30,"format":"mp4","video_codec":"h264","subtitle_mode":"burn"}}`
}

func itoa(value int) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func TestValidateManifestAcceptsDrupalEditSnapshot(t *testing.T) {
	manifest := snapshotV2(t)
	if err := ValidateManifest(manifest); err != nil {
		t.Fatalf("snapshot 97c8af86 must validate: %v", err)
	}
	if manifest.ContractVersion != 2 || len(manifest.Scenes) != 2 || manifest.DurationMS != 36000 {
		t.Fatalf("unexpected snapshot: %#v", manifest)
	}
	// Gateway gửi lại manifest này cho worker: các field v2 phải còn nguyên sau
	// một vòng JSON, không thì worker cắt từ giây 0.
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"source_start_ms":41000`, `"source_start_ms":0`, `"effect":"grayscale"`, `"fade_in_ms":500`, `"fade_out_ms":500`} {
		if !strings.Contains(string(encoded), expected) {
			t.Fatalf("re-encoded manifest lost %s: %s", expected, encoded)
		}
	}
}

func TestValidateManifestRejectsEditFieldsInV1(t *testing.T) {
	for _, field := range []struct{ name, json string }{
		{"source_start_ms", `,"source_start_ms":0`},
		{"effect", `,"effect":"none"`},
		{"fade_in_ms", `,"fade_in_ms":0`},
		{"fade_out_ms", `,"fade_out_ms":500`},
	} {
		raw := strings.Replace(v2Manifest(field.json, 6000), `"contract_version":2`, `"contract_version":1`, 1)
		err := ValidateManifest(decodeManifestJSON(t, raw))
		if err == nil || !strings.Contains(err.Error(), field.name) || !strings.Contains(err.Error(), "contract_version 2") {
			t.Fatalf("v1 manifest with %s must be rejected by name, got %v", field.name, err)
		}
	}
	for _, version := range []int{0, 3} {
		raw := strings.Replace(v2Manifest("", 6000), `"contract_version":2`, `"contract_version":`+itoa(version), 1)
		err := ValidateManifest(decodeManifestJSON(t, raw))
		if err == nil || !strings.Contains(err.Error(), "contract_version "+itoa(version)) {
			t.Fatalf("contract_version %d must be rejected by name, got %v", version, err)
		}
	}
}

func TestValidateManifestChecksEditBounds(t *testing.T) {
	ok := []string{
		`,"source_start_ms":0,"effect":"none","fade_in_ms":0,"fade_out_ms":0`,
		`,"source_start_ms":41000,"effect":"grayscale","fade_in_ms":1500`,
		`,"effect":"warm","fade_out_ms":1500`,
		`,"effect":"cool"`,
	}
	for _, extra := range ok {
		if err := ValidateManifest(decodeManifestJSON(t, v2Manifest(extra, 20000))); err != nil {
			t.Fatalf("%s must pass: %v", extra, err)
		}
	}
	bad := []struct{ extra, want string }{
		{`,"source_start_ms":-1`, "source_start_ms -1"},
		{`,"effect":"sepia"`, `effect "sepia"`},
		{`,"effect":""`, `effect ""`},
		{`,"fade_in_ms":-1`, "fade_in_ms -1"},
		{`,"fade_out_ms":-5`, "fade_out_ms -5"},
		{`,"fade_in_ms":1501`, "fade_in_ms 1501"},
		{`,"fade_out_ms":1600`, "fade_out_ms 1600"},
	}
	for _, item := range bad {
		err := ValidateManifest(decodeManifestJSON(t, v2Manifest(item.extra, 20000)))
		if err == nil || !strings.Contains(err.Error(), item.want) {
			t.Fatalf("%s must fail naming %q, got %v", item.extra, item.want, err)
		}
	}
	// Mờ dài hơn nửa cảnh 2 s.
	for _, extra := range []string{`,"fade_in_ms":1001`, `,"fade_out_ms":1200`} {
		err := ValidateManifest(decodeManifestJSON(t, v2Manifest(extra, 2000)))
		if err == nil || !strings.Contains(err.Error(), "half") {
			t.Fatalf("%s on a 2000 ms scene must fail as longer than half, got %v", extra, err)
		}
	}
	// Ảnh tĩnh không có gì để cắt.
	image := strings.Replace(v2Manifest(`,"source_start_ms":1000`, 6000), `"mime_type":"video/mp4"`, `"mime_type":"image/png"`, 1)
	if err := ValidateManifest(decodeManifestJSON(t, image)); err == nil || !strings.Contains(err.Error(), "source_start_ms") {
		t.Fatalf("source_start_ms on an image scene must fail, got %v", err)
	}
}

func TestNormalizeArgsCutsWithInputSeekAndAppliesEdit(t *testing.T) {
	profile := OutputProfile{Width: 1080, Height: 1920, FPS: 30, Format: "mp4", VideoCodec: "h264"}
	scene := decodeSceneJSON(t, `{"uuid":"edit-1-0","source_url":"https://drupal.test/src.mp4","mime_type":"video/mp4","duration_ms":20000,"source_start_ms":41000,"effect":"grayscale","fade_out_ms":500}`)
	args := normalizeArgs("in.mp4", "out.mp4", scene, profile, true)
	seek, input := slices.Index(args, "-ss"), slices.Index(args, "-i")
	if seek < 0 || input < 0 || seek > input || args[seek+1] != "41.000" || args[input+1] != "in.mp4" {
		t.Fatalf("cut must be -ss 41.000 before -i in.mp4: %q", args)
	}
	if strings.Count(strings.Join(args, " "), "-ss") != 1 {
		t.Fatalf("seek only once, on the input: %q", args)
	}
	joined := strings.Join(args, " ")
	for _, expected := range []string{
		"crop=1080:1920,hue=s=0,fps=30,tpad=stop_mode=clone:stop_duration=20.000,fade=t=out:st=19.500:d=0.500,format=yuv420p",
		"-af apad,afade=t=out:st=19.500:d=0.500",
		"-map 0:a:0",
		"-t 20.000 -c:v libx264",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %q in: %s", expected, joined)
		}
	}
	if strings.Contains(joined, "fade=t=in") {
		t.Fatalf("no fade_in_ms, no fade in: %s", joined)
	}

	first := decodeSceneJSON(t, `{"uuid":"edit-0-0","source_url":"https://drupal.test/src.mp4","mime_type":"video/mp4","duration_ms":16000,"source_start_ms":0,"effect":"none","fade_in_ms":500}`)
	joined = strings.Join(normalizeArgs("in.mp4", "out.mp4", first, profile, true), " ")
	for _, expected := range []string{
		"-ss 0.000 -i in.mp4",
		"crop=1080:1920,fps=30,tpad=stop_mode=clone:stop_duration=16.000,fade=t=in:st=0:d=0.500,format=yuv420p",
		"-af afade=t=in:st=0:d=0.500,apad",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %q in: %s", expected, joined)
		}
	}
	for _, unexpected := range []string{"hue=", "colorbalance", "fade=t=out"} {
		if strings.Contains(joined, unexpected) {
			t.Fatalf("effect none / no fade_out must not add %q: %s", unexpected, joined)
		}
	}

	// Mờ 0 (Drupal luôn gửi) không thêm bộ lọc.
	zero := decodeSceneJSON(t, `{"uuid":"z","source_url":"https://drupal.test/src.mp4","mime_type":"video/mp4","duration_ms":6000,"source_start_ms":0,"fade_in_ms":0,"fade_out_ms":0}`)
	if joined := strings.Join(normalizeArgs("in.mp4", "out.mp4", zero, profile, true), " "); strings.Contains(joined, "fade=") {
		t.Fatalf("a 0 ms fade must not add a filter: %s", joined)
	}
}

func TestNormalizeArgsGivesEachEffectItsOwnFilter(t *testing.T) {
	profile := OutputProfile{Width: 720, Height: 1280, FPS: 30, Format: "mp4", VideoCodec: "h264"}
	filters := map[string]string{}
	for _, effect := range []string{"none", "grayscale", "warm", "cool"} {
		scene := decodeSceneJSON(t, `{"uuid":"e","source_url":"https://drupal.test/src.mp4","mime_type":"video/mp4","duration_ms":6000,"effect":"`+effect+`"}`)
		args := normalizeArgs("in.mp4", "out.mp4", scene, profile, false)
		filters[effect] = args[slices.Index(args, "-vf")+1]
	}
	plain := normalizeArgs("in.mp4", "out.mp4", Scene{MIMEType: "video/mp4", DurationMS: 6000}, profile, false)
	if filters["none"] != plain[slices.Index(plain, "-vf")+1] {
		t.Fatalf("effect none must be the plain filter chain:\n%s\n%s", filters["none"], plain[slices.Index(plain, "-vf")+1])
	}
	seen := map[string]string{}
	for effect, filter := range filters {
		if other, dup := seen[filter]; dup {
			t.Fatalf("%s and %s share the filter %q", effect, other, filter)
		}
		seen[filter] = effect
	}
	if !strings.Contains(filters["grayscale"], "hue=s=0") {
		t.Fatalf("grayscale filter: %s", filters["grayscale"])
	}
	// Ảnh tĩnh cũng nhận màu và mờ (sau zoompan), không bị bỏ qua.
	image := decodeSceneJSON(t, `{"uuid":"i","source_url":"https://drupal.test/a.png","mime_type":"image/png","duration_ms":6000,"effect":"cool","fade_in_ms":300}`)
	joined := strings.Join(normalizeArgs("in.png", "out.mp4", image, profile, false), " ")
	if !strings.Contains(joined, "fps=30,colorbalance=") || !strings.Contains(joined, "fade=t=in:st=0:d=0.300,format=yuv420p") || strings.Contains(joined, "-ss") {
		t.Fatalf("image scene edit: %s", joined)
	}
}

func TestJoinArgsKeepsTimelineForCutScenes(t *testing.T) {
	profile := OutputProfile{Width: 1080, Height: 1920, FPS: 30, Format: "mp4", VideoCodec: "h264"}
	for _, item := range []struct {
		kind     string
		duration int
		offset   string
	}{
		{"crossfade", 16000 + 20000 - 400, "xfade=transition=fade:duration=0.400:offset=15.600"},
		{"fade_black", 16000 + 20000, "xfade=transition=fadeblack:duration=0.400:offset=16.000"},
	} {
		raw := `{"contract_version":2,"project_uuid":"11111111-1111-4111-8111-111111111111","project_revision":3,"duration_ms":` + itoa(item.duration) + `,"scenes":[` +
			`{"uuid":"edit-0-0","source_url":"https://drupal.test/src.mp4","mime_type":"video/mp4","duration_ms":16000,"source_start_ms":0,"effect":"warm","fade_in_ms":500},` +
			`{"uuid":"edit-1-0","source_url":"https://drupal.test/src.mp4","mime_type":"video/mp4","duration_ms":20000,"source_start_ms":41000,"effect":"warm","fade_out_ms":500,"transition":{"type":"` + item.kind + `","duration_ms":400}}` +
			`],"audio":[],"subtitles":[],"output":{"width":1080,"height":1920,"fps":30,"format":"mp4","video_codec":"h264","subtitle_mode":"burn"}}`
		manifest := decodeManifestJSON(t, raw)
		if err := ValidateManifest(manifest); err != nil {
			t.Fatalf("%s: %v", item.kind, err)
		}
		if timelineDuration(manifest.Scenes) != item.duration {
			t.Fatalf("%s timeline = %d, want %d", item.kind, timelineDuration(manifest.Scenes), item.duration)
		}
		joined := strings.Join(joinArgs([]string{"0.mp4", "1.mp4"}, manifest.Scenes, profile, "out.mp4"), " ")
		if !strings.Contains(joined, item.offset) {
			t.Fatalf("%s: missing %q in %s", item.kind, item.offset, joined)
		}
	}
}

// cutRunner báo nguồn dài sourceSeconds để Process kiểm đoạn cắt.
type cutRunner struct {
	recordingRunner
	sourceSeconds string
}

func (r *cutRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == "ffprobe" && strings.Contains(strings.Join(args, " "), "format=duration") {
		r.commands = append(r.commands, append([]string{name}, args...))
		return []byte(`{"format":{"duration":"` + r.sourceSeconds + `"}}`), nil
	}
	if name == "ffprobe" && strings.Contains(strings.Join(args, " "), "output.mp4") {
		r.commands = append(r.commands, append([]string{name}, args...))
		return []byte(`{"streams":[{"codec_type":"video","codec_name":"h264","width":1080,"height":1920,"duration":"20.000"},{"codec_type":"audio","codec_name":"aac","duration":"20.000"}],"format":{"duration":"20.000","size":"1024"}}`), nil
	}
	return r.recordingRunner.Run(ctx, name, args...)
}

func TestProcessRejectsCutPastTheSourceEnd(t *testing.T) {
	runner := &cutRunner{sourceSeconds: "61.000000"}
	processor := NewProcessor(runner, t.TempDir(), nil)
	processor.download = func(_ context.Context, rawURL, destination string, _ int64) error {
		return os.WriteFile(destination, []byte(rawURL), 0o600)
	}
	manifest := decodeManifestJSON(t, v2Manifest(`,"source_start_ms":50000`, 20000))
	_, err := processor.Process(context.Background(), "job-past-end", manifest)
	if err == nil || !strings.Contains(err.Error(), "50000-70000") || !strings.Contains(err.Error(), "61000") {
		t.Fatalf("a 50-70 s cut of a 61 s source must fail clearly, got %v", err)
	}
	for _, command := range runner.commands {
		if command[0] == "ffmpeg" {
			t.Fatalf("nothing may be rendered for an impossible cut (it would hold the last frame): %q", command)
		}
	}

	// Đúng tới cuối nguồn (41 s + 20 s = 61 s) thì được.
	runner = &cutRunner{sourceSeconds: "61.000000"}
	processor = NewProcessor(runner, t.TempDir(), nil)
	processor.download = func(_ context.Context, rawURL, destination string, _ int64) error {
		return os.WriteFile(destination, []byte(rawURL), 0o600)
	}
	if _, err := processor.Process(context.Background(), "job-to-end", decodeManifestJSON(t, v2Manifest(`,"source_start_ms":41000`, 20000))); err != nil {
		t.Fatalf("a cut ending exactly at the source end must pass: %v", err)
	}
}

func TestHealthReportsManifestVersionsAndKeepsContractOne(t *testing.T) {
	server, err := NewServer(&fakeProcessor{}, t.TempDir(), strings.Repeat("t", 32), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	recorder := newRecorder()
	server.Handler().ServeHTTP(recorder, newRequest("GET", "/health", ""))
	var health struct {
		OK                       bool  `json:"ok"`
		ContractVersion          int   `json:"contract_version"`
		AssembleManifestVersions []int `json:"assemble_manifest_versions"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if !health.OK || health.ContractVersion != 1 || !slices.Equal(health.AssembleManifestVersions, []int{1, 2}) {
		t.Fatalf("health = %s", recorder.Body.String())
	}
}

func TestAssembleAcceptsV2AndAnswersContractOne(t *testing.T) {
	processor := &fakeProcessor{}
	server, err := NewServer(processor, t.TempDir(), strings.Repeat("t", 32), "https://worker.test", 1)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/manifest-v2-97c8af86.json")
	if err != nil {
		t.Fatal(err)
	}
	body := `{"job_id":"job-v2","manifest":` + string(data) + `}`
	recorder := newRecorder()
	request := newRequest("POST", "/v1/assemble", body)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("t", 32))
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("v2 assemble status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	// Phản hồi giữ contract 1: gateway (cũ lẫn mới) đòi đúng 1.
	if response["contract_version"] != float64(1) {
		t.Fatalf("assemble response must keep contract_version 1: %s", recorder.Body.String())
	}
	if encoded, _ := json.Marshal(processor.manifest); !strings.Contains(string(encoded), `"source_start_ms":41000`) {
		t.Fatalf("worker lost source_start_ms: %s", encoded)
	}

	// Job v1 (d0202eb3) vẫn được trả contract 1 như cũ, gateway cũ nhận được.
	legacy, err := os.ReadFile("testdata/manifest-v1-d0202eb3.json")
	if err != nil {
		t.Fatal(err)
	}
	recorder = newRecorder()
	request = newRequest("POST", "/v1/assemble", `{"job_id":"job-v1","manifest":`+string(legacy)+`}`)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("t", 32))
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), `"contract_version":1`) {
		t.Fatalf("v1 assemble = %d %s", recorder.Code, recorder.Body.String())
	}

	// Khoá lạ trong cảnh vẫn bị từ chối.
	bad := strings.Replace(body, `"uuid": "edit-0-0"`, `"uuid": "edit-0-0", "foo": 1`, 1)
	if bad == body {
		t.Fatal("fixture changed: could not inject an unknown key")
	}
	recorder = newRecorder()
	request = newRequest("POST", "/v1/assemble", bad)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("t", 32))
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != 400 {
		t.Fatalf("unknown scene key must be 400, got %d", recorder.Code)
	}
}

// fakeProcessor ghi lại manifest worker nhận và tạo một file ra giả.
type fakeProcessor struct {
	manifest Manifest
}

func (p *fakeProcessor) Process(_ context.Context, jobID string, manifest Manifest) (Result, error) {
	p.manifest = manifest
	path := filepath.Join(os.TempDir(), "v30-fake-"+jobID+".mp4")
	if err := os.WriteFile(path, []byte("mp4"), 0o600); err != nil {
		return Result{}, err
	}
	return Result{Path: path, MIMEType: "video/mp4", FileSize: 3, Width: 1080, Height: 1920, DurationMS: manifest.DurationMS, Codec: "h264"}, nil
}

func newRecorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }

func newRequest(method, target, body string) *http.Request {
	return httptest.NewRequest(method, target, strings.NewReader(body))
}
