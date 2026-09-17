package mediaworker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

// Transitions are the one place where a wrong offset silently eats seconds, so
// the real chain is measured rather than trusted.
func TestRealFfmpegBlendsTransitionsWithoutLosingTime(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	clips := map[string]string{}
	for index, name := range []string{"one", "two", "three"} {
		path := filepath.Join(dir, name+".mp4")
		args := []string{"-v", "error", "-y", "-f", "lavfi", "-i", "testsrc=s=320x560:r=24:d=2", "-f", "lavfi", "-i", "sine=f=" + []string{"300", "500", "700"}[index] + ":d=2", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-shortest", path}
		if output, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
			t.Fatalf("ffmpeg fixture: %v\n%s", err, output)
		}
		clips["https://src.test/"+name+".mp4"] = path
	}

	processor := NewProcessor(nil, filepath.Join(dir, "work"), nil)
	processor.download = func(_ context.Context, rawURL, destination string, _ int64) error {
		data, err := os.ReadFile(clips[rawURL])
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o600)
	}
	manifest := Manifest{
		ContractVersion: 1, ProjectUUID: "11111111-1111-4111-8111-111111111111", ProjectRevision: 1,
		Scenes: []Scene{
			{UUID: "s1", SourceURL: "https://src.test/one.mp4", MIMEType: "video/mp4", DurationMS: 2000},
			{UUID: "s2", SourceURL: "https://src.test/two.mp4", MIMEType: "video/mp4", DurationMS: 2000, Transition: &Transition{Type: transitionCrossfade, DurationMS: 500}},
			{UUID: "s3", SourceURL: "https://src.test/three.mp4", MIMEType: "video/mp4", DurationMS: 2000, Transition: &Transition{Type: transitionFadeBlack, DurationMS: 400}},
		},
		Output: OutputProfile{Width: 360, Height: 640, FPS: 30, Format: "mp4", VideoCodec: "h264"},
	}
	manifest.DurationMS = timelineDuration(manifest.Scenes)
	if manifest.DurationMS != 5500 {
		t.Fatalf("expected a 5500 ms timeline, got %d", manifest.DurationMS)
	}
	result, err := processor.Process(context.Background(), "job-xfade", manifest)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if result.DurationMS < 5400 || result.DurationMS > 5600 {
		t.Fatalf("output is %d ms, want ~5500", result.DurationMS)
	}
	probe, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=codec_type", "-of", "csv=p=0", result.Path).Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(probe), "audio") {
		t.Fatalf("blended output lost its audio: %s", probe)
	}
}

// Ducking only matters if the music really drops, so the levels are measured.
func TestRealFfmpegDucksMusicUnderNarration(t *testing.T) {
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
	silentClip := filepath.Join(dir, "clip.mp4")
	run("-f", "lavfi", "-i", "color=c=black:s=320x560:r=24:d=4", "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-t", "4", silentClip)
	voice := filepath.Join(dir, "voice.wav")
	run("-f", "lavfi", "-i", "sine=f=1000:d=1", voice)
	music := filepath.Join(dir, "music.wav")
	run("-f", "lavfi", "-i", "sine=f=200:d=2", music)

	sources := map[string]string{"https://src.test/clip.mp4": silentClip, "https://src.test/voice.wav": voice, "https://src.test/music.wav": music}
	processor := NewProcessor(nil, filepath.Join(dir, "work"), nil)
	processor.download = func(_ context.Context, rawURL, destination string, _ int64) error {
		data, err := os.ReadFile(sources[rawURL])
		if err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0o600)
	}
	start := 1000
	manifest := Manifest{
		ContractVersion: 1, ProjectUUID: "11111111-1111-4111-8111-111111111111", ProjectRevision: 1, DurationMS: 4000,
		Scenes: []Scene{{UUID: "s1", SourceURL: "https://src.test/clip.mp4", MIMEType: "video/mp4", DurationMS: 4000}},
		Audio: []AudioTrack{
			{SourceURL: "https://src.test/music.wav", Kind: "music", GainDB: 0, Loop: true, Duck: true},
			{SourceURL: "https://src.test/voice.wav", Kind: "voice", GainDB: 0, StartMS: &start},
		},
		Output: OutputProfile{Width: 360, Height: 640, FPS: 30, Format: "mp4", VideoCodec: "h264"},
	}
	result, err := processor.Process(context.Background(), "job-duck-real", manifest)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	// 200 Hz is the music alone; measuring it inside and outside the spoken
	// second is the only way to see the sidechain actually working.
	level := func(from, to string) float64 {
		t.Helper()
		output, err := exec.Command("ffmpeg", "-v", "info", "-ss", from, "-t", to, "-i", result.Path, "-af", "lowpass=f=300,volumedetect", "-f", "null", "-").CombinedOutput()
		if err != nil {
			t.Fatalf("volumedetect: %v\n%s", err, output)
		}
		for _, line := range strings.Split(string(output), "\n") {
			if index := strings.Index(line, "mean_volume:"); index >= 0 {
				value := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line[index+len("mean_volume:"):]), "dB"))
				parsed, parseErr := strconv.ParseFloat(value, 64)
				if parseErr != nil {
					t.Fatalf("parse %q: %v", value, parseErr)
				}
				return parsed
			}
		}
		t.Fatalf("no mean_volume in:\n%s", output)
		return 0
	}
	speaking := level("1.2", "0.6")
	// 2.5 s, not 3 s: the music's own fade-out starts one second before the end.
	quiet := level("2.5", "0.6")
	if speaking > quiet-6 {
		t.Fatalf("music must drop at least 6 dB while speaking: %.1f dB vs %.1f dB", speaking, quiet)
	}
}
