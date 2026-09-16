package http

import (
	"encoding/base64"
	"os"
	"testing"
)

func TestDecodeChatInputImagesCreatesBoundedTemporaryMedia(t *testing.T) {
	data := base64.StdEncoding.EncodeToString([]byte("fake-image"))
	media, cleanup, err := decodeChatInputImages([]chatInputImage{{
		Data:     data,
		MimeType: "image/png",
		Filename: "người-mẫu.png",
	}})
	if err != nil {
		t.Fatalf("decodeChatInputImages() error = %v", err)
	}
	defer cleanup()
	if len(media) != 1 {
		t.Fatalf("media count = %d, want 1", len(media))
	}
	if media[0].MimeType != "image/png" || media[0].Filename != "người-mẫu.png" {
		t.Fatalf("unexpected media metadata: %+v", media[0])
	}
	if _, err := os.Stat(media[0].Path); err != nil {
		t.Fatalf("temporary image was not created: %v", err)
	}
}

func TestDecodeChatInputImagesRejectsNonImageAndTooManyFiles(t *testing.T) {
	data := base64.StdEncoding.EncodeToString([]byte("payload"))
	if _, _, err := decodeChatInputImages([]chatInputImage{{
		Data: data, MimeType: "text/plain", Filename: "note.txt",
	}}); err == nil {
		t.Fatal("expected a non-image MIME error")
	}

	images := make([]chatInputImage, maxChatInputImages+1)
	for i := range images {
		images[i] = chatInputImage{Data: data, MimeType: "image/jpeg", Filename: "person.jpg"}
	}
	if _, _, err := decodeChatInputImages(images); err == nil {
		t.Fatal("expected an image count error")
	}
}
