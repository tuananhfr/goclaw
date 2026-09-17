package mediaworker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Runs only where ffmpeg exists (the golang image with `apk add ffmpeg`); the
// recording runner cannot catch filter mistakes, and one ran image scenes 100x long.
func TestRealFfmpegHonoursSceneDurationsAndKeepsClipAudio(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if output, err := exec.Command("ffmpeg", append([]string{"-v", "error", "-y"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("ffmpeg %v: %v\n%s", args, err, output)
		}
	}
	still := filepath.Join(dir, "still.png")
	run("-f", "lavfi", "-i", "color=c=red:s=400x700", "-frames:v", "1", still)
	voiced := filepath.Join(dir, "voiced.mp4")
	run("-f", "lavfi", "-i", "testsrc=s=320x560:r=24:d=1", "-f", "lavfi", "-i", "sine=f=440:d=1", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-shortest", voiced)
	silent := filepath.Join(dir, "silent.mp4")
	run("-f", "lavfi", "-i", "testsrc=s=320x560:r=24:d=1", "-c:v", "libx264", "-preset", "ultrafast", silent)

	sources := map[string]string{"https://src.test/still.png": still, "https://src.test/voiced.mp4": voiced, "https://src.test/silent.mp4": silent}
	processor := NewProcessor(nil, filepath.Join(dir, "work"), nil)
	processor.download = func(_ context.Context, rawURL, destination string, _ int64) error {
		data, err := os.ReadFile(sources[rawURL])
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o600)
	}
	manifest := Manifest{
		ContractVersion: 1, ProjectUUID: "11111111-1111-4111-8111-111111111111", ProjectRevision: 1, DurationMS: 6000,
		Scenes: []Scene{
			{UUID: "s1", SourceURL: "https://src.test/still.png", MIMEType: "image/png", DurationMS: 2000, Motion: "smooth"},
			{UUID: "s2", SourceURL: "https://src.test/voiced.mp4", MIMEType: "video/mp4", DurationMS: 3000},
			{UUID: "s3", SourceURL: "https://src.test/silent.mp4", MIMEType: "video/mp4", DurationMS: 1000},
		},
		Subtitles: []SubtitleCue{{StartMS: 0, EndMS: 2000, Text: "Xin chào"}},
		Output:    OutputProfile{Width: 360, Height: 640, FPS: 30, Format: "mp4", VideoCodec: "h264", SubtitleMode: "mux"},
	}
	result, err := processor.Process(context.Background(), "job-real", manifest)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if result.DurationMS < 5900 || result.DurationMS > 6100 {
		t.Fatalf("output is %d ms, want ~6000", result.DurationMS)
	}
	probe, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=codec_type", "-of", "csv=p=0", result.Path).Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(probe), "audio") {
		t.Fatalf("output has no audio stream: %s", probe)
	}
}
