package discord

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/typing"
)

func TestSendStopsTypingAfterPlaceholderEditSucceeds(t *testing.T) {
	var stopCalled atomic.Bool
	var stopBeforeRequest atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/channels/channel-1/messages/placeholder-1" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if stopCalled.Load() {
			stopBeforeRequest.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"placeholder-1","channel_id":"channel-1","content":"done"}`))
	}))
	defer server.Close()

	ch := newTestChannel(t, server)

	ctrl := typing.New(typing.Options{
		StopFn: func() error {
			stopCalled.Store(true)
			return nil
		},
	})
	ch.typingCtrls.Store("channel-1", ctrl)
	ch.placeholders.Store("inbound-1", "placeholder-1")

	err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "discord",
		ChatID:  "channel-1",
		Content: "done",
		Metadata: map[string]string{
			"placeholder_key": "inbound-1",
		},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !stopCalled.Load() {
		t.Fatal("expected typing controller to stop after successful placeholder edit")
	}
	if stopBeforeRequest.Load() {
		t.Fatal("typing controller stopped before placeholder edit request")
	}
	if _, ok := ch.typingCtrls.Load("channel-1"); ok {
		t.Fatal("expected typing controller to be removed after successful delivery")
	}
}

func TestSendKeepsTypingActiveWhenDeliveryFails(t *testing.T) {
	var stopCalled atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/channels/channel-1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	ch := newTestChannel(t, server)

	ctrl := typing.New(typing.Options{
		StopFn: func() error {
			stopCalled.Store(true)
			return nil
		},
	})
	ch.typingCtrls.Store("channel-1", ctrl)

	err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "discord",
		ChatID:  "channel-1",
		Content: "done",
	})
	if err == nil {
		t.Fatal("expected Send() to return an error")
	}
	if stopCalled.Load() {
		t.Fatal("typing controller stopped even though Discord delivery failed")
	}
	if stored, ok := ch.typingCtrls.Load("channel-1"); !ok || stored != ctrl {
		t.Fatal("expected typing controller to remain active after delivery failure")
	}
}

func TestSendConvertsOutboundImageMarkerToDiscordAttachment(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "final.png")
	if err := os.WriteFile(imagePath, []byte("fake-png-bytes"), 0o644); err != nil {
		t.Fatalf("write image fixture: %v", err)
	}

	var mu sync.Mutex
	var sawMediaRequest bool
	var sawTextRequest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/channels/channel-1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		if strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
			sawMediaRequest = true
			if !bytes.Contains(body, []byte("fake-png-bytes")) {
				t.Fatal("expected uploaded image bytes in Discord request")
			}
			if bytes.Contains(body, []byte("<media:image")) {
				t.Fatal("expected media marker to be stripped from message content")
			}
			if bytes.Contains(body, []byte("final image")) {
				t.Fatal("expected text to be sent after media, not as upload caption")
			}
		} else {
			sawTextRequest = true
			if !bytes.Contains(body, []byte("final image")) {
				t.Fatal("expected surrounding text content to be sent after media")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg-1","channel_id":"channel-1","content":"ok"}`))
	}))
	defer server.Close()

	ch := newTestChannel(t, server)
	err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "discord",
		ChatID:  "channel-1",
		Content: "final image:\n\n<media:image path=\"" + imagePath + "\">",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !sawMediaRequest {
		t.Fatal("expected Discord media upload request")
	}
	if !sawTextRequest {
		t.Fatal("expected Discord text follow-up request")
	}
}

func TestSendMediaLongTextSendsTailAfterMedia(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "final.png")
	if err := os.WriteFile(imagePath, []byte("fake-png-bytes"), 0o644); err != nil {
		t.Fatalf("write image fixture: %v", err)
	}

	longText := strings.Repeat("intro ", 450) + "tail-marker"
	var mu sync.Mutex
	var requestKinds []string
	var sawTail bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/channels/channel-1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		if strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
			requestKinds = append(requestKinds, "media")
			if bytes.Contains(body, []byte("tail-marker")) {
				t.Fatal("tail text should not be included in media upload caption")
			}
		} else {
			requestKinds = append(requestKinds, "text")
			if bytes.Contains(body, []byte("tail-marker")) {
				sawTail = true
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg-1","channel_id":"channel-1","content":"ok"}`))
	}))
	defer server.Close()

	ch := newTestChannel(t, server)
	err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "discord",
		ChatID:  "channel-1",
		Content: longText,
		Media: []bus.MediaAttachment{{
			URL:         imagePath,
			ContentType: "image/png",
		}},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requestKinds) < 3 {
		t.Fatalf("expected media plus multiple text chunk requests, got %v", requestKinds)
	}
	if requestKinds[0] != "media" {
		t.Fatalf("expected media request first, got %v", requestKinds)
	}
	if !sawTail {
		t.Fatal("expected long text tail to be sent after media")
	}
}

func newTestChannel(t *testing.T, server *httptest.Server) *Channel {
	t.Helper()

	prevEndpointChannels := discordgo.EndpointChannels
	discordgo.EndpointChannels = server.URL + "/channels/"
	t.Cleanup(func() {
		discordgo.EndpointChannels = prevEndpointChannels
	})

	session, err := discordgo.New("Bot test-token")
	if err != nil {
		t.Fatalf("discordgo.New() error = %v", err)
	}
	session.Client = server.Client()

	ch := &Channel{
		BaseChannel: channels.NewBaseChannel(channels.TypeDiscord, nil, nil),
		session:     session,
	}
	ch.SetRunning(true)
	return ch
}
