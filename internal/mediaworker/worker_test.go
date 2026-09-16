package mediaworker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recordingRunner struct {
	commands [][]string
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.commands = append(r.commands, append([]string{name}, args...))
	if name == "ffprobe" {
		return []byte(`{"streams":[{"codec_type":"video","codec_name":"h264","width":1280,"height":720}],"format":{"duration":"6.000","size":"1024"}}`), nil
	}
	for index, value := range args {
		if value == "-i" || index == 0 {
			continue
		}
		if strings.HasSuffix(value, ".mp4") {
			if err := os.WriteFile(value, []byte("mp4"), 0o600); err != nil {
				return nil, err
			}
		}
	}
	return nil, nil
}

func TestValidateManifestRejectsUnsafeOrIncompleteInput(t *testing.T) {
	manifest := validManifest()
	manifest.Scenes = nil
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected empty scenes to be rejected")
	}

	manifest = validManifest()
	manifest.Scenes[0].SourceURL = "file:///etc/passwd"
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("expected non-HTTP source URL to be rejected")
	}
}

func TestProcessorBuildsNormalizedScenesConcatAndVerifiedOutput(t *testing.T) {
	temp := t.TempDir()
	runner := &recordingRunner{}
	processor := NewProcessor(runner, temp, nil)
	processor.download = func(_ context.Context, rawURL, destination string, _ int64) error {
		return os.WriteFile(destination, []byte(rawURL), 0o600)
	}

	result, err := processor.Process(context.Background(), "job-1", validManifest())
	if err != nil {
		t.Fatalf("process manifest: %v", err)
	}
	if result.Width != 1280 || result.Height != 720 || result.DurationMS != 6000 || result.Codec != "h264" {
		t.Fatalf("unexpected probe result: %#v", result)
	}
	if filepath.Ext(result.Path) != ".mp4" {
		t.Fatalf("expected mp4 output, got %s", result.Path)
	}
	joined := make([]string, 0, len(runner.commands))
	for _, command := range runner.commands {
		joined = append(joined, strings.Join(command, " "))
	}
	all := strings.Join(joined, "\n")
	for _, expected := range []string{"zoompan", "concat.txt", "ffprobe", "-movflags +faststart"} {
		if !strings.Contains(all, expected) {
			t.Fatalf("expected command plan to contain %q:\n%s", expected, all)
		}
	}
}

func TestProcessorMixesAudioAndMuxesVietnameseSubtitles(t *testing.T) {
	runner := &recordingRunner{}
	processor := NewProcessor(runner, t.TempDir(), nil)
	processor.download = func(_ context.Context, rawURL, destination string, _ int64) error {
		return os.WriteFile(destination, []byte(rawURL), 0o600)
	}
	manifest := validManifest()
	manifest.Audio = []AudioTrack{{SourceURL: "https://example.test/music.mp3", Kind: "music", GainDB: -12}}
	manifest.Subtitles = []SubtitleCue{{StartMS: 0, EndMS: 3000, Text: "Xin chào Việt Nam"}}

	if _, err := processor.Process(context.Background(), "job-audio", manifest); err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, command := range runner.commands {
		all.WriteString(strings.Join(command, " "))
		all.WriteByte('\n')
	}
	for _, expected := range []string{"volume=-12dB", "amix=inputs=1", "mov_text", "language=vie"} {
		if !strings.Contains(all.String(), expected) {
			t.Fatalf("missing %q in commands:\n%s", expected, all.String())
		}
	}
	srt, err := os.ReadFile(filepath.Join(processor.root, "job-audio", "subtitles.srt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(srt), "00:00:00,000 --> 00:00:03,000\nXin chào Việt Nam") {
		t.Fatalf("unexpected SRT:\n%s", srt)
	}
}

func validManifest() Manifest {
	return Manifest{
		ContractVersion: 1,
		ProjectUUID:     "11111111-1111-4111-8111-111111111111",
		ProjectRevision: 3,
		DurationMS:      6000,
		Scenes: []Scene{
			{UUID: "22222222-2222-4222-8222-222222222222", SourceURL: "https://example.test/one.jpg", MIMEType: "image/jpeg", DurationMS: 3000, Motion: "subtle"},
			{UUID: "33333333-3333-4333-8333-333333333333", SourceURL: "https://example.test/two.png", MIMEType: "image/png", DurationMS: 3000, Motion: "smooth"},
		},
		Output: OutputProfile{Width: 1280, Height: 720, FPS: 30, Format: "mp4", VideoCodec: "h264", SubtitleMode: "mux"},
	}
}
