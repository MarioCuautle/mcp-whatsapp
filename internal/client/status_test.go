package client

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"github.com/sealjay/mcp-whatsapp/internal/ratelimit"
)

type statusSend struct {
	to  types.JID
	msg *waE2E.Message
}

// newStatusTestClient builds a Client whose WhatsApp seams are fakes; sends
// are recorded in the returned slice.
func newStatusTestClient(t *testing.T, privacy []types.StatusPrivacy, recipients []types.JID, lim *ratelimit.Limiter) (*Client, *[]statusSend) {
	t.Helper()
	var sent []statusSend
	c := &Client{
		log:         NewStderrLogger("test", "ERROR", false),
		limiter:     lim,
		isConnected: func() bool { return true },
		waSend: func(_ context.Context, to types.JID, m *waE2E.Message, _ ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error) {
			sent = append(sent, statusSend{to, m})
			return whatsmeow.SendResponse{ID: "STATUS1"}, nil
		},
		statusPrivacyFn:    func(context.Context) ([]types.StatusPrivacy, error) { return privacy, nil },
		statusRecipientsFn: func(context.Context) ([]types.JID, error) { return recipients, nil },
	}
	return c, &sent
}

func jids(n int) []types.JID {
	out := make([]types.JID, n)
	for i := range out {
		out[i] = types.NewJID("5215500000"+string(rune('0'+i)), types.DefaultUserServer)
	}
	return out
}

func TestPostStatus_TextGoesToStatusBroadcast(t *testing.T) {
	privacy := []types.StatusPrivacy{{Type: types.StatusPrivacyTypeContacts}}
	c, sent := newStatusTestClient(t, privacy, jids(3), nil)
	r := c.PostStatus(context.Background(), PostStatusOptions{Text: "  hola  ", BackgroundColor: "#112233"})
	if !r.Success || r.ID != "STATUS1" || r.Audience != 3 {
		t.Fatalf("unexpected result: %+v", r)
	}
	if len(*sent) != 1 || (*sent)[0].to != types.StatusBroadcastJID {
		t.Fatalf("expected one send to status@broadcast, got %+v", *sent)
	}
	ext := (*sent)[0].msg.GetExtendedTextMessage()
	if ext.GetText() != "hola" || ext.GetBackgroundArgb() != 0xFF112233 {
		t.Fatalf("unexpected message: text=%q argb=%#x", ext.GetText(), ext.GetBackgroundArgb())
	}
}

func TestPostStatus_ValidationSendsNothing(t *testing.T) {
	c, sent := newStatusTestClient(t, nil, jids(1), nil)
	cases := map[string]PostStatusOptions{
		"empty":             {},
		"both":              {Text: "a", MediaPath: "/x.jpg"},
		"caption with text": {Text: "a", Caption: "b"},
		"color with media":  {MediaPath: "/x.jpg", BackgroundColor: "#000000"},
		"bad color":         {Text: "a", BackgroundColor: "red"},
		"too long":          {Text: strings.Repeat("x", maxStatusTextLen+1)},
		"whitespace only":   {Text: "   "},
	}
	for name, opts := range cases {
		if r := c.PostStatus(context.Background(), opts); r.Success {
			t.Errorf("%s: expected failure, got %+v", name, r)
		}
	}
	if len(*sent) != 0 {
		t.Fatalf("invalid requests must not send anything, sent %d", len(*sent))
	}
}

func TestPostStatus_RateLimited(t *testing.T) {
	cfg := ratelimit.DefaultConfig()
	cfg.MinStatusInterval = time.Hour
	c, sent := newStatusTestClient(t, nil, jids(1), ratelimit.New(cfg))
	if r := c.PostStatus(context.Background(), PostStatusOptions{Text: "uno"}); !r.Success {
		t.Fatalf("first post should pass: %+v", r)
	}
	r := c.PostStatus(context.Background(), PostStatusOptions{Text: "dos"})
	if r.Success || !strings.Contains(r.Message, "rate limited") {
		t.Fatalf("second post should be rate limited, got %+v", r)
	}
	if len(*sent) != 1 {
		t.Fatalf("expected exactly one send, got %d", len(*sent))
	}
}

func TestPostStatus_OverrideBypassesLimiter(t *testing.T) {
	cfg := ratelimit.DefaultConfig()
	cfg.MinStatusInterval = time.Hour
	c, sent := newStatusTestClient(t, nil, jids(1), ratelimit.New(cfg))
	ctx := ratelimit.WithBypass(context.Background())
	c.PostStatus(ctx, PostStatusOptions{Text: "uno"})
	if r := c.PostStatus(ctx, PostStatusOptions{Text: "dos"}); !r.Success {
		t.Fatalf("override should bypass the status limiter: %+v", r)
	}
	if len(*sent) != 2 {
		t.Fatalf("expected two sends, got %d", len(*sent))
	}
}

func TestPostStatus_NotConnectedAndAudienceFailure(t *testing.T) {
	c, sent := newStatusTestClient(t, nil, nil, nil)
	c.isConnected = func() bool { return false }
	if r := c.PostStatus(context.Background(), PostStatusOptions{Text: "x"}); r.Success {
		t.Fatalf("disconnected client must not post: %+v", r)
	}
	c.isConnected = func() bool { return true }
	c.statusRecipientsFn = func(context.Context) ([]types.JID, error) { return nil, errors.New("boom") }
	if r := c.PostStatus(context.Background(), PostStatusOptions{Text: "x"}); r.Success {
		t.Fatalf("unresolvable audience must fail closed: %+v", r)
	}
	if len(*sent) != 0 {
		t.Fatalf("nothing should be sent, got %d", len(*sent))
	}
}

func TestStatusAudience_ReportsCountsNotJIDs(t *testing.T) {
	privacy := []types.StatusPrivacy{{Type: types.StatusPrivacyTypeBlacklist, List: jids(2)}}
	c, _ := newStatusTestClient(t, privacy, jids(4), nil)
	aud, err := c.StatusAudience(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if aud.PrivacyType != "blacklist" || aud.ListSize != 2 || aud.Recipients != 4 {
		t.Fatalf("unexpected audience: %+v", aud)
	}
}

func TestParseARGB(t *testing.T) {
	for in, want := range map[string]uint32{"#000000": 0xFF000000, "ffffff": 0xFFFFFFFF, " #0A0b0C ": 0xFF0A0B0C} {
		if got, err := parseARGB(in); err != nil || got != want {
			t.Errorf("parseARGB(%q) = %#x, %v; want %#x", in, got, err, want)
		}
	}
	for _, in := range []string{"", "#12345", "#GGGGGG", "#1234567"} {
		if _, err := parseARGB(in); err == nil {
			t.Errorf("parseARGB(%q) should fail", in)
		}
	}
}
