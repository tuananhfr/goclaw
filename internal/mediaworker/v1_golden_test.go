package mediaworker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// File golden ghi lại từng lệnh ffmpeg/ffprobe mà mã 75f64097 sinh cho các
// manifest v1. Manifest v2 thêm field vào Scene và nhánh vào normalizeArgs, nên
// đây là chốt chặn "job v1 y như cũ". Sinh lại (chỉ trên mã 75f64097):
// V30_WRITE_GOLDEN=1 go test ./internal/mediaworker -run TestV1FfmpegArgsMatch
const v1GoldenPath = "testdata/v1-ffmpeg-args-75f64097.json"

// goldenRunner ghi lệnh như recordingRunner nhưng trả độ dài đúng manifest và
// có tiếng cho các nguồn được chỉ định, để Process đi hết mọi nhánh v1.
type goldenRunner struct {
	recordingRunner
	durationMS int
	voiced     map[string]bool
}

func (r *goldenRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == "ffprobe" {
		r.commands = append(r.commands, append([]string{name}, args...))
		path := args[len(args)-1]
		seconds := strconv.FormatFloat(float64(r.durationMS)/1000, 'f', 3, 64)
		streams := `{"codec_type":"video","codec_name":"h264","width":1080,"height":1920,"duration":"` + seconds + `"}`
		if r.voiced[filepath.Base(path)] || filepath.Base(path) == "output.mp4" {
			streams += `,{"codec_type":"audio","codec_name":"aac","duration":"` + seconds + `"}`
		}
		return []byte(`{"streams":[` + streams + `],"format":{"duration":"` + seconds + `","size":"4096"}}`), nil
	}
	return r.recordingRunner.Run(ctx, name, args...)
}

type v1GoldenCase struct {
	Name     string     `json:"name"`
	Commands [][]string `json:"commands"`
}

func v1GoldenManifests(t *testing.T) []struct {
	name     string
	manifest Manifest
	voiced   map[string]bool
} {
	t.Helper()
	data, err := os.ReadFile("testdata/manifest-v1-d0202eb3.json")
	if err != nil {
		t.Fatal(err)
	}
	var d0202eb3 Manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d0202eb3); err != nil {
		t.Fatalf("decode d0202eb3: %v", err)
	}

	withAudio := validManifest()
	withAudio.Audio = []AudioTrack{{SourceURL: "https://example.test/music.mp3", Kind: "music", GainDB: -12}}
	withAudio.Subtitles = []SubtitleCue{{StartMS: 0, EndMS: 3000, Text: "Xin chào Việt Nam"}}

	start := 1000
	ducked := validManifest()
	ducked.Audio = []AudioTrack{
		{SourceURL: "https://example.test/music.mp3", Kind: "music", GainDB: -14, Loop: true, Duck: true},
		{SourceURL: "https://example.test/voice.wav", Kind: "voice", StartMS: &start},
	}

	mixed := Manifest{
		ContractVersion: 1, ProjectUUID: "11111111-1111-4111-8111-111111111111", ProjectRevision: 1, DurationMS: 6000,
		Scenes: []Scene{
			{UUID: "s1", SourceURL: "https://src.test/still.png", MIMEType: "image/png", DurationMS: 2000, Motion: "smooth"},
			{UUID: "s2", SourceURL: "https://src.test/voiced.mp4", MIMEType: "video/mp4", DurationMS: 3000},
			{UUID: "s3", SourceURL: "https://src.test/silent.mp4", MIMEType: "video/mp4", DurationMS: 1000},
		},
		Subtitles: []SubtitleCue{{StartMS: 0, EndMS: 2000, Text: "Xin chào"}},
		Output:    OutputProfile{Width: 360, Height: 640, FPS: 30, Format: "mp4", VideoCodec: "h264", SubtitleMode: "mux"},
	}

	blended := Manifest{
		ContractVersion: 1, ProjectUUID: "11111111-1111-4111-8111-111111111111", ProjectRevision: 1, DurationMS: 5500,
		Scenes: []Scene{
			{UUID: "s1", SourceURL: "https://src.test/one.mp4", MIMEType: "video/mp4", DurationMS: 2000},
			{UUID: "s2", SourceURL: "https://src.test/two.mp4", MIMEType: "video/mp4", DurationMS: 2000, Transition: &Transition{Type: transitionCrossfade, DurationMS: 500}},
			{UUID: "s3", SourceURL: "https://src.test/three.mp4", MIMEType: "video/mp4", DurationMS: 2000, Transition: &Transition{Type: transitionFadeBlack, DurationMS: 400}},
		},
		Output: OutputProfile{Width: 360, Height: 640, FPS: 30, Format: "mp4", VideoCodec: "h264"},
	}

	return []struct {
		name     string
		manifest Manifest
		voiced   map[string]bool
	}{
		{"d0202eb3", d0202eb3, nil},
		{"valid-manifest", validManifest(), nil},
		{"music-and-mux-subtitles", withAudio, nil},
		{"ducked-music", ducked, nil},
		{"still-voiced-silent", mixed, map[string]bool{"source-001.mp4": true}},
		{"crossfade-fade-black", blended, map[string]bool{"source-000.mp4": true, "source-001.mp4": true, "source-002.mp4": true}},
	}
}

func TestV1FfmpegArgsMatch75f64097(t *testing.T) {
	got := make([]v1GoldenCase, 0, 6)
	for _, item := range v1GoldenManifests(t) {
		root := t.TempDir()
		runner := &goldenRunner{durationMS: item.manifest.DurationMS, voiced: item.voiced}
		processor := NewProcessor(runner, root, nil)
		processor.download = func(_ context.Context, rawURL, destination string, _ int64) error {
			return os.WriteFile(destination, []byte(rawURL), 0o600)
		}
		if _, err := processor.Process(context.Background(), "job-golden", item.manifest); err != nil {
			t.Fatalf("%s: process: %v", item.name, err)
		}
		// Thư mục tạm đổi mỗi lần chạy; thay bằng <root> để so được.
		commands := make([][]string, 0, len(runner.commands))
		for _, command := range runner.commands {
			normalized := make([]string, len(command))
			for index, value := range command {
				normalized[index] = strings.ReplaceAll(filepath.ToSlash(value), filepath.ToSlash(root), "<root>")
			}
			commands = append(commands, normalized)
		}
		got = append(got, v1GoldenCase{Name: item.name, Commands: commands})
	}

	if os.Getenv("V30_WRITE_GOLDEN") == "1" {
		data, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(v1GoldenPath, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", v1GoldenPath)
		return
	}
	data, err := os.ReadFile(v1GoldenPath)
	if err != nil {
		t.Fatal(err)
	}
	var want []v1GoldenCase
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if len(want) != len(got) {
		t.Fatalf("golden has %d cases, got %d", len(want), len(got))
	}
	for index := range want {
		if want[index].Name != got[index].Name {
			t.Fatalf("case %d: golden %q, got %q", index, want[index].Name, got[index].Name)
		}
		if len(want[index].Commands) != len(got[index].Commands) {
			t.Fatalf("%s: golden has %d commands, got %d", want[index].Name, len(want[index].Commands), len(got[index].Commands))
		}
		for command := range want[index].Commands {
			if !reflect.DeepEqual(want[index].Commands[command], got[index].Commands[command]) {
				t.Errorf("%s command %d changed:\nwant %q\ngot  %q", want[index].Name, command, want[index].Commands[command], got[index].Commands[command])
			}
		}
	}
	// Phải có đủ cả normalize, join có chuyển cảnh và decorate thì mới là chốt chặn thật.
	all, _ := json.Marshal(got)
	for _, expected := range []string{"zoompan", "xfade=transition=fadeblack", "sidechaincompress", "subtitles=", "mov_text", "-af\",\"apad"} {
		if !strings.Contains(string(all), expected) {
			t.Errorf("golden commands never exercise %q", expected)
		}
	}
}
