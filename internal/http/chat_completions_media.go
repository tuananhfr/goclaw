package http

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	channelmedia "github.com/nextlevelbuilder/goclaw/internal/channels/media"
)

const (
	maxChatRequestBodyBytes = 30 * 1024 * 1024
	maxChatInputImages      = 4
	maxChatInputImageBytes  = 8 * 1024 * 1024
	maxChatInputTotalBytes  = 20 * 1024 * 1024
)

// chatInputImage is a bounded private-media bridge for backend integrations.
// Bytes stay out of public URLs and callers cannot submit arbitrary server paths.
type chatInputImage struct {
	Data     string `json:"data"`
	MimeType string `json:"mime_type"`
	Filename string `json:"filename,omitempty"`
}

func decodeChatInputImages(images []chatInputImage) ([]bus.MediaFile, func(), error) {
	cleanupPaths := make([]string, 0, len(images))
	cleanup := func() {
		for _, path := range cleanupPaths {
			_ = os.Remove(path)
		}
	}
	if len(images) > maxChatInputImages {
		return nil, cleanup, fmt.Errorf("input_images supports at most %d images", maxChatInputImages)
	}

	mediaFiles := make([]bus.MediaFile, 0, len(images))
	totalBytes := 0
	for index, image := range images {
		mimeType := strings.ToLower(strings.TrimSpace(image.MimeType))
		extension, supported := map[string]string{
			"image/jpeg": ".jpg",
			"image/png":  ".png",
			"image/webp": ".webp",
		}[mimeType]
		if !supported {
			cleanup()
			return nil, func() {}, fmt.Errorf("input_images[%d] has unsupported MIME type", index)
		}
		if image.Data == "" || base64.StdEncoding.DecodedLen(len(image.Data)) > maxChatInputImageBytes {
			cleanup()
			return nil, func() {}, fmt.Errorf("input_images[%d] exceeds the 8 MB limit", index)
		}
		decoded, err := base64.StdEncoding.DecodeString(image.Data)
		if err != nil || len(decoded) == 0 {
			cleanup()
			return nil, func() {}, fmt.Errorf("input_images[%d] is not valid base64", index)
		}
		if len(decoded) > maxChatInputImageBytes || totalBytes+len(decoded) > maxChatInputTotalBytes {
			cleanup()
			return nil, func() {}, fmt.Errorf("input_images exceeds the media size limit")
		}
		totalBytes += len(decoded)

		file, err := os.CreateTemp("", "goclaw-chat-reference-*"+extension)
		if err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("create input image buffer: %w", err)
		}
		path := file.Name()
		cleanupPaths = append(cleanupPaths, path)
		if _, err := file.Write(decoded); err != nil {
			_ = file.Close()
			cleanup()
			return nil, func() {}, fmt.Errorf("write input image buffer: %w", err)
		}
		if err := file.Close(); err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("close input image buffer: %w", err)
		}

		filename := filepath.Base(strings.TrimSpace(image.Filename))
		if filename == "." || filename == "" {
			filename = fmt.Sprintf("character-reference-%d%s", index+1, extension)
		}
		mediaFiles = append(mediaFiles, bus.MediaFile{
			Path:     path,
			MimeType: mimeType,
			Filename: filename,
		})
	}
	return mediaFiles, cleanup, nil
}

func decorateChatMessageWithMedia(message string, mediaFiles []bus.MediaFile) string {
	if len(mediaFiles) == 0 {
		return message
	}
	infos := make([]channelmedia.MediaInfo, 0, len(mediaFiles))
	for _, item := range mediaFiles {
		infos = append(infos, channelmedia.MediaInfo{
			Type:        "image",
			FilePath:    item.Path,
			ContentType: item.MimeType,
			FileName:    item.Filename,
		})
	}
	if tags := channelmedia.BuildMediaTags(infos); tags != "" {
		return tags + "\n\n" + message
	}
	return message
}
