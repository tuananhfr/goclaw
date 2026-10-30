package mediaworker

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Manifest v2 ("Xuất video đã chỉnh"): cắt, màu và mờ chỉ tin được khi đo trên
// khung hình thật, vì recordingRunner không thấy -ss đặt sai chỗ hay bộ lọc sai.

func requireFfmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
}

func runFfmpeg(t *testing.T, args ...string) {
	t.Helper()
	if output, err := exec.Command("ffmpeg", append([]string{"-v", "error", "-y"}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg %v: %v\n%s", args, err, output)
	}
}

var signalStatPattern = regexp.MustCompile(`lavfi\.signalstats\.([A-Z]+)=([0-9.]+)`)

// frameStats đo signalstats của đúng một khung ở mốc seconds.
func frameStats(t *testing.T, path string, seconds float64) map[string]float64 {
	t.Helper()
	output, err := exec.Command("ffmpeg", "-v", "info", "-ss", strconv.FormatFloat(seconds, 'f', 3, 64), "-i", path,
		"-frames:v", "1", "-vf", "signalstats,metadata=mode=print", "-f", "null", "-").CombinedOutput()
	if err != nil {
		t.Fatalf("signalstats %s @%.3f: %v\n%s", path, seconds, err, output)
	}
	stats := map[string]float64{}
	for _, match := range signalStatPattern.FindAllStringSubmatch(string(output), -1) {
		value, parseErr := strconv.ParseFloat(match[2], 64)
		if parseErr == nil {
			stats[match[1]] = value
		}
	}
	if _, ok := stats["YAVG"]; !ok {
		t.Fatalf("no signalstats for %s @%.3f:\n%s", path, seconds, output)
	}
	return stats
}

func processV2(t *testing.T, dir, jobID, source, sceneJSON string, durationMS int) Result {
	t.Helper()
	processor := NewProcessor(nil, filepath.Join(dir, "work"), nil)
	processor.download = func(_ context.Context, _ string, destination string, _ int64) error {
		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o600)
	}
	raw := `{"contract_version":2,"project_uuid":"11111111-1111-4111-8111-111111111111","project_revision":1,"duration_ms":` + strconv.Itoa(durationMS) +
		`,"scenes":[` + sceneJSON + `],"audio":[],"subtitles":[],"output":{"width":360,"height":640,"fps":30,"format":"mp4","video_codec":"h264","subtitle_mode":"burn"}}`
	result, err := processor.Process(context.Background(), jobID, decodeManifestJSON(t, raw))
	if err != nil {
		t.Fatalf("process %s: %v", jobID, err)
	}
	return result
}

// Nguồn 8 s mà mỗi giây một độ sáng (16 + 28*giây) và có tiếng: khung ra ở giây
// k phải sáng bằng khung nguồn ở giây 4+k, không phải giây k.
func TestRealFfmpegCutsFromSourceStart(t *testing.T) {
	requireFfmpeg(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "steps.mp4")
	runFfmpeg(t, "-f", "lavfi", "-i", "color=c=gray:s=360x640:r=30:d=8,format=yuv420p,geq=lum='16+28*mod(floor(T),8)':cb=128:cr=128",
		"-f", "lavfi", "-i", "sine=f=440:d=8", "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", source)

	result := processV2(t, dir, "job-cut", source,
		`{"uuid":"c","source_url":"https://src.test/steps.mp4","mime_type":"video/mp4","duration_ms":3000,"source_start_ms":4000,"effect":"none","fade_in_ms":0,"fade_out_ms":0}`, 3000)
	video, sound := streamSeconds(t, result.Path, "v:0"), streamSeconds(t, result.Path, "a:0")
	t.Logf("cut 4.0-7.0 s: video %.3f s, audio %.3f s", video, sound)
	if math.Abs(video-3) > 0.05 || math.Abs(sound-3) > 0.05 {
		t.Fatalf("a 3000 ms cut must run 3.0 ±0.05 s, got video %.3f audio %.3f", video, sound)
	}
	for _, second := range []float64{0.5, 1.5, 2.5} {
		got := frameStats(t, result.Path, second)["YAVG"]
		want := frameStats(t, source, 4+second)["YAVG"]
		wrong := frameStats(t, source, second)["YAVG"]
		t.Logf("out@%.1f YAVG %.1f, source@%.1f %.1f, source@%.1f %.1f", second, got, 4+second, want, second, wrong)
		if math.Abs(got-want) > 6 || math.Abs(got-wrong) < 20 {
			t.Fatalf("out@%.1f (Y %.1f) must match source@%.1f (Y %.1f), not source@%.1f (Y %.1f)", second, got, 4+second, want, second, wrong)
		}
	}

	// Cắt quá cuối nguồn (6 s + 3 s trên nguồn 8 s) phải hỏng, không giữ khung cuối.
	processor := NewProcessor(nil, filepath.Join(dir, "work-past"), nil)
	processor.download = func(_ context.Context, _ string, destination string, _ int64) error {
		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o600)
	}
	past := decodeManifestJSON(t, `{"contract_version":2,"project_uuid":"11111111-1111-4111-8111-111111111111","project_revision":1,"duration_ms":3000,"scenes":[`+
		`{"uuid":"p","source_url":"https://src.test/steps.mp4","mime_type":"video/mp4","duration_ms":3000,"source_start_ms":6000}`+
		`],"output":{"width":360,"height":640,"fps":30,"format":"mp4","video_codec":"h264"}}`)
	if _, err := processor.Process(context.Background(), "job-past", past); err == nil || !strings.Contains(err.Error(), "source runs only") {
		t.Fatalf("a cut past the source end must fail, got %v", err)
	}
}

func TestRealFfmpegAppliesColourEffects(t *testing.T) {
	requireFfmpeg(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "bars.mp4")
	runFfmpeg(t, "-f", "lavfi", "-i", "smptebars=s=360x640:r=30:d=4", "-f", "lavfi", "-i", "sine=f=440:d=4",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", source)
	if saturation := frameStats(t, source, 1.5)["SATAVG"]; saturation <= 5 {
		t.Fatalf("fixture must be colourful, SATAVG %.2f", saturation)
	}
	tint := map[string]float64{}
	for _, effect := range []string{"none", "grayscale", "warm", "cool"} {
		result := processV2(t, dir, "job-"+effect, source,
			`{"uuid":"e","source_url":"https://src.test/bars.mp4","mime_type":"video/mp4","duration_ms":3000,"source_start_ms":500,"effect":"`+effect+`"}`, 3000)
		stats := frameStats(t, result.Path, 1.0)
		tint[effect] = stats["VAVG"] - stats["UAVG"]
		t.Logf("%s: SATAVG %.2f, VAVG-UAVG %.2f", effect, stats["SATAVG"], tint[effect])
		if effect == "grayscale" && stats["SATAVG"] > 1 {
			t.Fatalf("grayscale must have SATAVG <= 1, got %.2f", stats["SATAVG"])
		}
		if effect == "none" && stats["SATAVG"] <= 5 {
			t.Fatalf("effect none must keep the colour, SATAVG %.2f", stats["SATAVG"])
		}
	}
	if tint["warm"] <= tint["none"]+1 || tint["cool"] >= tint["none"]-1 {
		t.Fatalf("warm must push V-U above none and cool below it: %#v", tint)
	}
}

func TestRealFfmpegFadesInAndOut(t *testing.T) {
	requireFfmpeg(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "bright.mp4")
	runFfmpeg(t, "-f", "lavfi", "-i", "color=c=0xB0B0B0:s=360x640:r=30:d=4", "-f", "lavfi", "-i", "sine=f=440:d=4",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", source)
	result := processV2(t, dir, "job-fade", source,
		`{"uuid":"f","source_url":"https://src.test/bright.mp4","mime_type":"video/mp4","duration_ms":3000,"source_start_ms":0,"effect":"none","fade_in_ms":500,"fade_out_ms":500}`, 3000)
	video := streamSeconds(t, result.Path, "v:0")
	full := frameStats(t, result.Path, 1.0)["YAVG"]
	first := frameStats(t, result.Path, 0)["YAVG"]
	early := frameStats(t, result.Path, 0.6)["YAVG"]
	last := frameStats(t, result.Path, video-1.0/30)["YAVG"]
	t.Logf("fade 500: first %.1f, @0.6 %.1f, @1.0 %.1f, last(@%.3f) %.1f", first, early, full, video-1.0/30, last)
	if first > 0.3*full || last > 0.3*full {
		t.Fatalf("first (%.1f) and last (%.1f) frames must be <= 30%% of @1.0 (%.1f)", first, last, full)
	}
	if early < 0.9*full {
		t.Fatalf("a 500 ms fade must be over by 0.6 s: %.1f vs %.1f", early, full)
	}
}
