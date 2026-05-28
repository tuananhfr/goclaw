package discord

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bwmarrin/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// sendMediaMessage sends media attachments first, then sends text as follow-up
// messages. Keeping text out of the upload caption prevents Discord's 2000-char
// caption limit from dropping the tail of long responses.
func (c *Channel) sendMediaMessage(channelID string, content string, mediaList []bus.MediaAttachment) error {
	var files []*discordgo.File

	for _, att := range mediaList {
		filePath := att.URL
		if filePath == "" {
			continue
		}

		maxBytes := c.config.MediaMaxBytes
		if maxBytes <= 0 {
			maxBytes = defaultMediaMaxBytes
		}

		f, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("open media file %s: %w", filePath, err)
		}
		defer f.Close()

		if info, err := f.Stat(); err == nil && info.Size() > maxBytes {
			return fmt.Errorf("outbound media too large: %d bytes (limit %d)", info.Size(), maxBytes)
		}

		ct := att.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}

		files = append(files, &discordgo.File{
			Name:        filepath.Base(filePath),
			ContentType: ct,
			Reader:      f,
		})
	}

	if len(files) == 0 {
		return nil
	}

	// Discord supports multiple files + text in a single message.
	msg := &discordgo.MessageSend{
		Files: files,
	}

	_, err := c.session.ChannelMessageSendComplex(channelID, msg)
	if err != nil {
		return fmt.Errorf("send discord media message: %w", err)
	}
	if content != "" {
		if err := c.sendChunked(channelID, content); err != nil {
			return err
		}
	}
	return nil
}
