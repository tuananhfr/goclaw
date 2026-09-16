package mediaworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	ContractVersion = 1
	maxScenes       = 60
	maxSourceBytes  = int64(2 << 30)
)

type Manifest struct {
	ContractVersion int           `json:"contract_version"`
	ProjectUUID     string        `json:"project_uuid"`
	ProjectRevision int           `json:"project_revision"`
	DurationMS      int           `json:"duration_ms,omitempty"`
	Scenes          []Scene       `json:"scenes"`
	Audio           []AudioTrack  `json:"audio,omitempty"`
	Subtitles       []SubtitleCue `json:"subtitles,omitempty"`
	Output          OutputProfile `json:"output"`
}

type Scene struct {
	UUID       string         `json:"uuid"`
	AssetUUID  string         `json:"asset_uuid,omitempty"`
	SourceURL  string         `json:"source_url"`
	MIMEType   string         `json:"mime_type"`
	DurationMS int            `json:"duration_ms"`
	Motion     string         `json:"motion,omitempty"`
	Transition map[string]any `json:"transition,omitempty"`
}

type AudioTrack struct {
	SourceURL string  `json:"source_url"`
	Kind      string  `json:"kind"`
	GainDB    float64 `json:"gain_db"`
	// StartMS places a clip at a fixed offset and disables looping; nil keeps
	// the background-music behaviour of looping for the whole video.
	StartMS *int `json:"start_ms,omitempty"`
}

type SubtitleCue struct {
	StartMS int    `json:"start_ms"`
	EndMS   int    `json:"end_ms"`
	Text    string `json:"text"`
}

type OutputProfile struct {
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	FPS          int    `json:"fps"`
	Format       string `json:"format"`
	VideoCodec   string `json:"video_codec"`
	SubtitleMode string `json:"subtitle_mode"`
}

type Result struct {
	Path       string `json:"-"`
	MIMEType   string `json:"mime_type"`
	FileSize   int64  `json:"file_size"`
	Checksum   string `json:"checksum_sha256,omitempty"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	DurationMS int    `json:"duration_ms"`
	Codec      string `json:"codec"`
}

type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type commandRunner struct{}

func (commandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

type Processor struct {
	runner   Runner
	root     string
	client   *http.Client
	download func(context.Context, string, string, int64) error
}

func NewProcessor(runner Runner, root string, client *http.Client) *Processor {
	if runner == nil {
		runner = commandRunner{}
	}
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	processor := &Processor{runner: runner, root: root, client: client}
	processor.download = processor.downloadHTTP
	return processor
}

func ValidateManifest(manifest Manifest) error {
	if manifest.ContractVersion != ContractVersion || strings.TrimSpace(manifest.ProjectUUID) == "" || manifest.ProjectRevision < 1 {
		return errors.New("invalid contract, project UUID, or project revision")
	}
	if len(manifest.Scenes) == 0 || len(manifest.Scenes) > maxScenes {
		return fmt.Errorf("scene count must be between 1 and %d", maxScenes)
	}
	if manifest.Output.Width < 240 || manifest.Output.Width > 3840 || manifest.Output.Height < 240 || manifest.Output.Height > 3840 || manifest.Output.FPS < 1 || manifest.Output.FPS > 60 {
		return errors.New("invalid output dimensions or fps")
	}
	if manifest.Output.Format != "mp4" || manifest.Output.VideoCodec != "h264" {
		return errors.New("only h264 mp4 output is currently supported")
	}
	if manifest.Output.SubtitleMode != "" && manifest.Output.SubtitleMode != "mux" && manifest.Output.SubtitleMode != "burn" {
		return errors.New("subtitle mode must be mux or burn")
	}
	totalDuration := 0
	for _, scene := range manifest.Scenes {
		if strings.TrimSpace(scene.UUID) == "" || scene.DurationMS < 1000 || scene.DurationMS > 30000 {
			return errors.New("invalid scene UUID or duration")
		}
		if err := validateRemoteURL(scene.SourceURL); err != nil {
			return fmt.Errorf("scene %s: %w", scene.UUID, err)
		}
		if !strings.HasPrefix(scene.MIMEType, "image/") && !strings.HasPrefix(scene.MIMEType, "video/") {
			return fmt.Errorf("scene %s has unsupported MIME type", scene.UUID)
		}
		totalDuration += scene.DurationMS
	}
	if totalDuration > 600000 || manifest.DurationMS > 0 && manifest.DurationMS != totalDuration {
		return errors.New("manifest duration does not match its scene timeline")
	}
	for _, track := range manifest.Audio {
		if err := validateRemoteURL(track.SourceURL); err != nil {
			return fmt.Errorf("audio track: %w", err)
		}
		if track.StartMS != nil && (*track.StartMS < 0 || *track.StartMS > totalDuration) {
			return fmt.Errorf("audio track start_ms %d is outside the %d ms timeline", *track.StartMS, totalDuration)
		}
	}
	return nil
}

func (p *Processor) Process(ctx context.Context, jobID string, manifest Manifest) (Result, error) {
	if err := ValidateManifest(manifest); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(jobID) == "" || strings.ContainsAny(jobID, `/\\`) {
		return Result{}, errors.New("invalid job ID")
	}
	jobDir := filepath.Join(p.root, jobID)
	if err := os.MkdirAll(jobDir, 0o700); err != nil {
		return Result{}, fmt.Errorf("create job directory: %w", err)
	}

	normalized := make([]string, 0, len(manifest.Scenes))
	for index, scene := range manifest.Scenes {
		extension := extensionForMIME(scene.MIMEType)
		source := filepath.Join(jobDir, fmt.Sprintf("source-%03d%s", index, extension))
		if err := p.download(ctx, scene.SourceURL, source, maxSourceBytes); err != nil {
			return Result{}, fmt.Errorf("download scene %d: %w", index+1, err)
		}
		clip := filepath.Join(jobDir, fmt.Sprintf("scene-%03d.mp4", index))
		args := normalizeArgs(source, clip, scene, manifest.Output)
		if _, err := p.runner.Run(ctx, "ffmpeg", args...); err != nil {
			return Result{}, fmt.Errorf("normalize scene %d: %w", index+1, err)
		}
		normalized = append(normalized, clip)
	}

	concatPath := filepath.Join(jobDir, "concat.txt")
	var concat strings.Builder
	for _, clip := range normalized {
		concat.WriteString("file '")
		concat.WriteString(strings.ReplaceAll(filepath.ToSlash(clip), "'", "'\\''"))
		concat.WriteString("'\n")
	}
	if err := os.WriteFile(concatPath, []byte(concat.String()), 0o600); err != nil {
		return Result{}, fmt.Errorf("write concat manifest: %w", err)
	}

	outputPath := filepath.Join(jobDir, "output.mp4")
	concatOutput := outputPath
	if len(manifest.Audio) > 0 || len(manifest.Subtitles) > 0 {
		concatOutput = filepath.Join(jobDir, "timeline.mp4")
	}
	concatArgs := []string{"-y", "-f", "concat", "-safe", "0", "-i", concatPath, "-c", "copy", "-movflags", "+faststart", "-map_metadata", "-1", concatOutput}
	if _, err := p.runner.Run(ctx, "ffmpeg", concatArgs...); err != nil {
		return Result{}, fmt.Errorf("assemble timeline: %w", err)
	}
	if concatOutput != outputPath {
		if err := p.decorate(ctx, jobDir, concatOutput, outputPath, manifest); err != nil {
			return Result{}, err
		}
	}
	return p.probe(ctx, outputPath)
}

func (p *Processor) decorate(ctx context.Context, jobDir, timeline, output string, manifest Manifest) error {
	args := []string{"-y", "-i", timeline}
	audioPaths := make([]string, 0, len(manifest.Audio))
	for index, track := range manifest.Audio {
		path := filepath.Join(jobDir, fmt.Sprintf("audio-%02d.bin", index))
		if err := p.download(ctx, track.SourceURL, path, maxSourceBytes); err != nil {
			return fmt.Errorf("download audio track %d: %w", index+1, err)
		}
		audioPaths = append(audioPaths, path)
		if track.StartMS == nil {
			args = append(args, "-stream_loop", "-1")
		}
		args = append(args, "-i", path)
	}

	subtitlePath := ""
	if len(manifest.Subtitles) > 0 {
		subtitlePath = filepath.Join(jobDir, "subtitles.srt")
		if err := os.WriteFile(subtitlePath, []byte(renderSRT(manifest.Subtitles)), 0o600); err != nil {
			return fmt.Errorf("write subtitles: %w", err)
		}
		if manifest.Output.SubtitleMode == "mux" {
			args = append(args, "-i", subtitlePath)
		}
	}

	args = append(args, "-map", "0:v:0")
	if len(audioPaths) > 0 {
		parts := make([]string, 0, len(audioPaths)+1)
		labels := make([]string, 0, len(audioPaths))
		for index, track := range manifest.Audio {
			label := fmt.Sprintf("a%d", index)
			filter := fmt.Sprintf("[%d:a]volume=%gdB", index+1, track.GainDB)
			if track.StartMS != nil {
				filter += fmt.Sprintf(",adelay=delays=%d:all=1", *track.StartMS)
			}
			parts = append(parts, filter+"["+label+"]")
			labels = append(labels, "["+label+"]")
		}
		// A narration clip ends; without apad, -shortest would cut the video to it.
		parts = append(parts, fmt.Sprintf("%samix=inputs=%d:duration=longest:normalize=0[mixed]", strings.Join(labels, ""), len(labels)))
		parts = append(parts, "[mixed]apad[aout]")
		args = append(args, "-filter_complex", strings.Join(parts, ";"), "-map", "[aout]", "-c:a", "aac", "-b:a", "192k")
	} else {
		args = append(args, "-map", "0:a?")
	}

	if subtitlePath != "" && manifest.Output.SubtitleMode == "burn" {
		filterPath := strings.ReplaceAll(filepath.ToSlash(subtitlePath), "'", "'\\''")
		args = append(args, "-vf", "subtitles='"+filterPath+"'", "-c:v", "libx264", "-preset", "veryfast", "-crf", "20")
	} else {
		args = append(args, "-c:v", "copy")
	}
	if subtitlePath != "" && manifest.Output.SubtitleMode == "mux" {
		subtitleInput := 1 + len(audioPaths)
		args = append(args, "-map", fmt.Sprintf("%d:s:0", subtitleInput), "-c:s", "mov_text", "-metadata:s:s:0", "language=vie")
	}
	args = append(args, "-shortest", "-movflags", "+faststart", "-map_metadata", "-1", output)
	if _, err := p.runner.Run(ctx, "ffmpeg", args...); err != nil {
		return fmt.Errorf("mix audio and subtitles: %w", err)
	}
	return nil
}

func renderSRT(cues []SubtitleCue) string {
	var output strings.Builder
	for index, cue := range cues {
		if cue.EndMS <= cue.StartMS || strings.TrimSpace(cue.Text) == "" {
			continue
		}
		fmt.Fprintf(&output, "%d\n%s --> %s\n%s\n\n", index+1, srtTime(cue.StartMS), srtTime(cue.EndMS), strings.ReplaceAll(strings.TrimSpace(cue.Text), "\r", ""))
	}
	return output.String()
}

func srtTime(milliseconds int) string {
	if milliseconds < 0 {
		milliseconds = 0
	}
	hours := milliseconds / 3600000
	milliseconds %= 3600000
	minutes := milliseconds / 60000
	milliseconds %= 60000
	seconds := milliseconds / 1000
	milliseconds %= 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", hours, minutes, seconds, milliseconds)
}

func normalizeArgs(source, destination string, scene Scene, profile OutputProfile) []string {
	duration := strconv.FormatFloat(float64(scene.DurationMS)/1000, 'f', 3, 64)
	scale := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d", profile.Width, profile.Height, profile.Width, profile.Height)
	args := []string{"-y"}
	if strings.HasPrefix(scene.MIMEType, "image/") {
		frames := max(1, scene.DurationMS*profile.FPS/1000)
		zoomStep := "0.0005"
		if scene.Motion == "smooth" {
			zoomStep = "0.0010"
		} else if scene.Motion == "dynamic" {
			zoomStep = "0.0018"
		}
		filter := fmt.Sprintf("%s,zoompan=z='min(zoom+%s,1.12)':d=%d:s=%dx%d:fps=%d,format=yuv420p", scale, zoomStep, frames, profile.Width, profile.Height, profile.FPS)
		args = append(args, "-loop", "1", "-t", duration, "-i", source, "-vf", filter)
	} else {
		filter := fmt.Sprintf("%s,fps=%d,format=yuv420p", scale, profile.FPS)
		args = append(args, "-t", duration, "-i", source, "-vf", filter)
	}
	return append(args, "-an", "-c:v", "libx264", "-preset", "veryfast", "-crf", "20", "-r", strconv.Itoa(profile.FPS), "-movflags", "+faststart", "-map_metadata", "-1", destination)
}

func (p *Processor) probe(ctx context.Context, path string) (Result, error) {
	output, err := p.runner.Run(ctx, "ffprobe", "-v", "error", "-show_streams", "-show_format", "-of", "json", path)
	if err != nil {
		return Result{}, err
	}
	var probe struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
			Size     string `json:"size"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &probe); err != nil {
		return Result{}, fmt.Errorf("decode ffprobe output: %w", err)
	}
	for _, stream := range probe.Streams {
		if stream.CodecType != "video" {
			continue
		}
		duration, _ := strconv.ParseFloat(probe.Format.Duration, 64)
		size, _ := strconv.ParseInt(probe.Format.Size, 10, 64)
		if size <= 0 {
			if stat, statErr := os.Stat(path); statErr == nil {
				size = stat.Size()
			}
		}
		return Result{Path: path, MIMEType: "video/mp4", FileSize: size, Width: stream.Width, Height: stream.Height, DurationMS: int(duration * 1000), Codec: stream.CodecName}, nil
	}
	return Result{}, errors.New("ffprobe found no video stream")
}

func (p *Processor) downloadHTTP(ctx context.Context, rawURL, destination string, maxBytes int64) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("source returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxBytes {
		return errors.New("source exceeds byte limit")
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	written, err := io.Copy(file, io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return err
	}
	if written > maxBytes {
		return errors.New("source exceeds byte limit")
	}
	return nil
}

func validateRemoteURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("source URL must be an absolute HTTP(S) URL without credentials")
	}
	return nil
}

func extensionForMIME(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	default:
		return ".mp4"
	}
}
