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
	// inputs=2: the timeline's own audio plus the music track.
	for _, expected := range []string{"volume=-12dB", "[0:a:0]", "amix=inputs=2", "mov_text", "language=vie"} {
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

func TestProcessorPositionsVoiceTracksAndPadsTheMix(t *testing.T) {
	runner := &recordingRunner{}
	processor := NewProcessor(runner, t.TempDir(), nil)
	processor.download = func(_ context.Context, rawURL, destination string, _ int64) error {
		return os.WriteFile(destination, []byte(rawURL), 0o600)
	}
	start := 3000
	manifest := validManifest()
	manifest.Audio = []AudioTrack{
		{SourceURL: "https://example.test/music.mp3", Kind: "music", GainDB: -12},
		{SourceURL: "https://example.test/voice.wav", Kind: "voice", StartMS: &start},
	}

	if _, err := processor.Process(context.Background(), "job-voice", manifest); err != nil {
		t.Fatal(err)
	}

	var mix []string
	for _, command := range runner.commands {
		if strings.Contains(strings.Join(command, " "), "amix") {
			mix = command
		}
	}
	if mix == nil {
		t.Fatal("no mixing command recorded")
	}

	looped := map[string]bool{}
	for i := 0; i+3 < len(mix); i++ {
		if mix[i] == "-stream_loop" && mix[i+1] == "-1" && mix[i+2] == "-i" {
			looped[filepath.Base(mix[i+3])] = true
		}
	}
	if !looped["audio-00.bin"] {
		t.Fatal("music track must still loop")
	}
	if looped["audio-01.bin"] {
		t.Fatal("voice track must not loop")
	}

	joined := strings.Join(mix, " ")
	// The output length comes from -t, never from whichever track ends first.
	for _, expected := range []string{"adelay=delays=3000:all=1", "apad[aout]", "-t 6.000"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %q in mix command:\n%s", expected, joined)
		}
	}
}

func TestValidateManifestRejectsVoiceOutsideTimeline(t *testing.T) {
	for _, start := range []int{-1, 7000} {
		manifest := validManifest()
		manifest.Audio = []AudioTrack{{SourceURL: "https://example.test/voice.wav", Kind: "voice", StartMS: &start}}
		if err := ValidateManifest(manifest); err == nil {
			t.Fatalf("start_ms %d must be rejected for a 6000 ms timeline", start)
		}
	}
}

func TestJoinArgsBlendsTransitionsAndKeepsPlainCutsOnConcat(t *testing.T) {
	profile := OutputProfile{Width: 720, Height: 1280, FPS: 30, Format: "mp4", VideoCodec: "h264"}
	scenes := []Scene{
		{UUID: "a", DurationMS: 3000},
		{UUID: "b", DurationMS: 3000, Transition: &Transition{Type: transitionCrossfade, DurationMS: 500}},
		{UUID: "c", DurationMS: 3000, Transition: &Transition{Type: transitionFadeBlack, DurationMS: 400}},
		{UUID: "d", DurationMS: 3000, Transition: &Transition{Type: transitionCut}},
	}
	joined := strings.Join(joinArgs([]string{"0.mp4", "1.mp4", "2.mp4", "3.mp4"}, scenes, profile, "out.mp4"), " ")
	for _, expected := range []string{
		"xfade=transition=fade:duration=0.500:offset=2.500[v1]",
		"[0:a][1:a]acrossfade=d=0.500[a1]",
		"[v1]tpad=stop_mode=clone:stop_duration=0.400[p2]",
		"[a1]apad=pad_dur=0.400[q2]",
		// Cảnh 3 vào lúc 5.5 s (đã trừ hoà tan), dip đen kéo dài tổng thêm 0.4 s.
		"xfade=transition=fadeblack:duration=0.400:offset=5.500[v2]",
		"[v2][a2][3:v][3:a]concat=n=2:v=1:a=1[v3][a3]",
		"-map [v3] -map [a3]",
		"-c:v libx264",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %q in join command:\n%s", expected, joined)
		}
	}
	if strings.Contains(joined, "-f concat") {
		t.Fatal("a blended timeline must not use the stream-copy concat")
	}
	if timelineDuration(scenes) != 12000-500 {
		t.Fatalf("only a crossfade shortens the film, got %d", timelineDuration(scenes))
	}
	if joinArgs([]string{"0.mp4", "1.mp4"}, []Scene{{DurationMS: 3000}, {DurationMS: 3000, Transition: &Transition{Type: transitionCut}}}, profile, "out.mp4") != nil {
		t.Fatal("hard cuts only must keep the cheap concat path")
	}
}

func TestValidateManifestChecksTransitions(t *testing.T) {
	manifest := validManifest()
	manifest.Scenes[0].Transition = &Transition{Type: transitionCrossfade, DurationMS: 500}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("the first scene cannot have a transition")
	}

	manifest = validManifest()
	manifest.Scenes[1].Transition = &Transition{Type: "wipe", DurationMS: 500}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("an unknown transition must be rejected")
	}

	manifest = validManifest()
	manifest.Scenes[1].Transition = &Transition{Type: transitionCrossfade, DurationMS: 4000}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("a transition longer than its scenes must be rejected")
	}

	manifest = validManifest()
	manifest.Scenes[1].Transition = &Transition{Type: transitionCrossfade, DurationMS: 500}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("a crossfade shortens the timeline, so 6000 ms no longer matches")
	}
	manifest.DurationMS = 5500
	if err := ValidateManifest(manifest); err != nil {
		t.Fatalf("overlap-aware duration must pass: %v", err)
	}
}

func TestProcessorDucksLoopedMusicUnderNarration(t *testing.T) {
	runner := &recordingRunner{}
	processor := NewProcessor(runner, t.TempDir(), nil)
	processor.download = func(_ context.Context, rawURL, destination string, _ int64) error {
		return os.WriteFile(destination, []byte(rawURL), 0o600)
	}
	start := 1000
	manifest := validManifest()
	manifest.Audio = []AudioTrack{
		{SourceURL: "https://example.test/music.mp3", Kind: "music", GainDB: -14, Loop: true, Duck: true},
		{SourceURL: "https://example.test/voice.wav", Kind: "voice", StartMS: &start},
	}
	if _, err := processor.Process(context.Background(), "job-duck", manifest); err != nil {
		t.Fatal(err)
	}
	var mix string
	for _, command := range runner.commands {
		if joined := strings.Join(command, " "); strings.Contains(joined, "amix") {
			mix = joined
		}
	}
	for _, expected := range []string{
		"volume=-14dB[a0]",
		"[0:a:0][a1]amix=inputs=2:duration=longest:normalize=0[speech]",
		"[speech]asplit=2[sc0][speechmix]",
		"[sc0]apad=whole_dur=6.000[scpad0]",
		"[a0][scpad0]sidechaincompress=threshold=0.02:ratio=12:attack=20:release=300[duck0]",
		"[duck0]afade=t=out:st=5.000:d=1[music0]",
		"[speechmix][music0]amix=inputs=2",
		"alimiter=limit=0.95,apad[aout]",
		"-t 6.000",
	} {
		if !strings.Contains(mix, expected) {
			t.Fatalf("missing %q in mix command:\n%s", expected, mix)
		}
	}

	runner = &recordingRunner{}
	processor = NewProcessor(runner, t.TempDir(), nil)
	processor.download = func(_ context.Context, rawURL, destination string, _ int64) error {
		return os.WriteFile(destination, []byte(rawURL), 0o600)
	}
	plain := validManifest()
	plain.Audio = []AudioTrack{{SourceURL: "https://example.test/music.mp3", Kind: "music", GainDB: -14}}
	if _, err := processor.Process(context.Background(), "job-plain", plain); err != nil {
		t.Fatal(err)
	}
	for _, command := range runner.commands {
		if strings.Contains(strings.Join(command, " "), "sidechaincompress") {
			t.Fatal("music without duck must not be compressed")
		}
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

func TestNormalizeArgsKeepsSceneDurationAndAlwaysEmitsAudio(t *testing.T) {
	profile := OutputProfile{Width: 720, Height: 1280, FPS: 30, Format: "mp4", VideoCodec: "h264"}
	image := strings.Join(normalizeArgs("in.png", "out.mp4", Scene{MIMEType: "image/png", DurationMS: 4000, Motion: "smooth"}, profile, false), " ")
	for _, expected := range []string{"zoompan=z='min(zoom+0.0010,1.12)':d=1:", "-framerate 25 -t 4.000 -i in.png", "anullsrc=r=48000:cl=stereo", "-map 1:a:0", "-t 4.000 -c:v libx264", "-c:a aac -ar 48000 -ac 2"} {
		if !strings.Contains(image, expected) {
			t.Fatalf("image scene missing %q in: %s", expected, image)
		}
	}
	if strings.Contains(image, "-an") {
		t.Fatal("scenes must carry an audio track so concat and the mix stay aligned")
	}

	silent := strings.Join(normalizeArgs("in.mp4", "out.mp4", Scene{MIMEType: "video/mp4", DurationMS: 8000}, profile, false), " ")
	for _, expected := range []string{"tpad=stop_mode=clone:stop_duration=8.000", "anullsrc", "-map 1:a:0", "-t 8.000 -c:v"} {
		if !strings.Contains(silent, expected) {
			t.Fatalf("silent clip missing %q in: %s", expected, silent)
		}
	}

	voiced := strings.Join(normalizeArgs("in.mp4", "out.mp4", Scene{MIMEType: "video/mp4", DurationMS: 6000}, profile, true), " ")
	for _, expected := range []string{"-af apad", "-map 0:a:0", "tpad=stop_mode=clone:stop_duration=6.000"} {
		if !strings.Contains(voiced, expected) {
			t.Fatalf("voiced clip missing %q in: %s", expected, voiced)
		}
	}
	if strings.Contains(voiced, "anullsrc") {
		t.Fatal("a clip with sound must keep it, not get silence")
	}
}

func TestProcessRejectsOutputWhoseLengthDriftsFromManifest(t *testing.T) {
	runner := &driftingRunner{}
	processor := NewProcessor(runner, t.TempDir(), nil)
	processor.download = func(_ context.Context, rawURL, destination string, _ int64) error {
		return os.WriteFile(destination, []byte(rawURL), 0o600)
	}
	_, err := processor.Process(context.Background(), "job-drift", validManifest())
	if err == nil || !strings.Contains(err.Error(), "manifest is 6000 ms") {
		t.Fatalf("a 400 s output for a 6 s manifest must fail, got %v", err)
	}
}

// driftingRunner reports a final file 100x longer than the manifest, the shape of the old zoompan bug.
type driftingRunner struct{ recordingRunner }

func (r *driftingRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name == "ffprobe" && strings.Contains(strings.Join(args, " "), "output.mp4") {
		return []byte(`{"streams":[{"codec_type":"video","codec_name":"h264","width":1280,"height":720}],"format":{"duration":"400.000","size":"1024"}}`), nil
	}
	return r.recordingRunner.Run(ctx, name, args...)
}
