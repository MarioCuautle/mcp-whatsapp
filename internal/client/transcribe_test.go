package client

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"

	"github.com/sealjay/mcp-whatsapp/internal/store"
	"github.com/sealjay/mcp-whatsapp/internal/transcribe"
)

const trJID, trMsg = "447700000123@s.whatsapp.net", "AUDIO-1"

// transcribeFixture builds a client whose store holds one downloadable audio
// message and whose speech-to-text call is stubbed.
func transcribeFixture(t *testing.T, mediaType string) (*Client, *int) {
	t.Helper()
	t.Chdir(t.TempDir())
	s, err := store.Open("./store")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	payload := []byte("fake-audio-bytes")
	digest := sha256.Sum256(payload)
	if err := s.StoreChat(trJID, "Test", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := s.StoreMessage(context.Background(), store.Message{
		ID: trMsg, ChatJID: trJID, Sender: trJID, Timestamp: time.Now().UTC(),
		MediaType: mediaType, Filename: "voice.ogg", URL: "https://example.invalid/m", DirectPath: "/m",
	}, []byte{1}, digest[:], []byte{2}, uint64(len(payload))); err != nil {
		t.Fatal(err)
	}
	c := newClientWithStore(t, s)
	c.downloadMedia = func(context.Context, whatsmeow.DownloadableMessage) ([]byte, error) { return payload, nil }
	c.transcribeCfg = transcribe.Config{URL: "http://127.0.0.1:1", Model: "small", MaxSeconds: 600}
	calls := 0
	c.transcribeFn = func(_ context.Context, _ transcribe.Config, path, lang string) (transcribe.Result, error) {
		calls++
		return transcribe.Result{Text: "hola, soy " + lang, Language: "es", Duration: 2}, nil
	}
	return c, &calls
}

func TestTranscribeAudio_NotConfigured(t *testing.T) {
	c := newClientWithStore(t, nil)
	if _, _, err := c.TranscribeAudio(context.Background(), "m", "c", ""); !errors.Is(err, transcribe.ErrNotConfigured) {
		t.Fatalf("got %v", err)
	}
	if c.TranscriptionEnabled() {
		t.Fatal("must be disabled without a URL")
	}
}

func TestTranscribeAudio_TranscribesOnceAndCaches(t *testing.T) {
	c, calls := transcribeFixture(t, "audio")
	tr, cached, err := c.TranscribeAudio(context.Background(), trMsg, trJID, "es")
	if err != nil || cached || tr.Text != "hola, soy es" || tr.Language != "es" || tr.Model != "small" {
		t.Fatalf("first call: %+v cached=%v err=%v", tr, cached, err)
	}
	tr, cached, err = c.TranscribeAudio(context.Background(), trMsg, trJID, "")
	if err != nil || !cached || tr.Text != "hola, soy es" {
		t.Fatalf("second call: %+v cached=%v err=%v", tr, cached, err)
	}
	if *calls != 1 {
		t.Fatalf("the service was called %d times, want 1", *calls)
	}
}

func TestTranscribeAudio_RejectsNonAudioAndBadArgs(t *testing.T) {
	c, calls := transcribeFixture(t, "image")
	if _, _, err := c.TranscribeAudio(context.Background(), trMsg, trJID, ""); err == nil || !strings.Contains(err.Error(), "not audio") {
		t.Fatalf("non-audio: %v", err)
	}
	if *calls != 0 {
		t.Fatal("the service must not be called for non-audio media")
	}
	if _, _, err := c.TranscribeAudio(context.Background(), "", trJID, ""); err == nil {
		t.Fatal("empty message_id must fail")
	}
	if _, _, err := c.TranscribeAudio(context.Background(), "MISSING", trJID, ""); err == nil {
		t.Fatal("unknown message must fail")
	}
}

func TestTranscribeAudio_ServiceErrorIsNotCached(t *testing.T) {
	c, _ := transcribeFixture(t, "audio")
	c.transcribeFn = func(context.Context, transcribe.Config, string, string) (transcribe.Result, error) {
		return transcribe.Result{}, errors.New("backend down")
	}
	if _, _, err := c.TranscribeAudio(context.Background(), trMsg, trJID, ""); err == nil {
		t.Fatal("expected the service error")
	}
	if got, _ := c.store.GetTranscript(context.Background(), trMsg, trJID); got != nil {
		t.Fatalf("a failed transcription must not be cached: %+v", got)
	}
}
