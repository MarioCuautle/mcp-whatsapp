package client

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/sealjay/mcp-whatsapp/internal/ratelimit"
)

// PostStatusOptions describes a status (story) post. Exactly one of Text or
// MediaPath is required; Caption only applies to media.
type PostStatusOptions struct {
	Text            string
	MediaPath       string // image or video under the media root
	Caption         string
	BackgroundColor string // text statuses only: "#RRGGBB"; empty keeps WhatsApp's default
}

// StatusResult is the outcome of PostStatus.
type StatusResult struct {
	Success  bool
	Message  string
	ID       string
	Audience int // contacts the status was addressed to, including the paired account
}

// StatusAudience reports who a status would reach under the account's current
// status-privacy setting. It carries counts only, never JIDs.
type StatusAudience struct {
	PrivacyType string // "contacts", "blacklist" or "whitelist"
	ListSize    int    // entries in the blacklist/whitelist; 0 for "contacts"
	Recipients  int    // resolved recipients, including the paired account
}

const maxStatusTextLen = 700

// StatusAudience resolves the recipients a status post would be sent to,
// without sending anything. It uses the same resolution whatsmeow applies on
// send: the account's status-privacy setting over the locally cached contacts.
func (c *Client) StatusAudience(ctx context.Context) (StatusAudience, error) {
	if !c.isConnected() {
		return StatusAudience{}, fmt.Errorf("not connected to WhatsApp")
	}
	privacy, err := c.statusPrivacy(ctx)
	if err != nil {
		return StatusAudience{}, fmt.Errorf("read status privacy: %w", err)
	}
	recipients, err := c.statusRecipients(ctx)
	if err != nil {
		return StatusAudience{}, fmt.Errorf("resolve status audience: %w", err)
	}
	out := StatusAudience{PrivacyType: string(types.StatusPrivacyTypeContacts), Recipients: len(recipients)}
	if len(privacy) > 0 {
		out.PrivacyType = string(privacy[0].Type)
		out.ListSize = len(privacy[0].List)
	}
	// whatsmeow always adds the paired account to the recipient list.
	if own := c.ownUser(); own != "" {
		found := false
		for _, r := range recipients {
			if r.User == own {
				found = true
				break
			}
		}
		if !found {
			out.Recipients++
		}
	}
	return out, nil
}

// PostStatus publishes a text, image or video status to the account's status
// audience. It enforces its own rate limit (checkStatusRate), is never cached
// as a chat message, and refuses to send when the audience cannot be resolved.
func (c *Client) PostStatus(ctx context.Context, opts PostStatusOptions) StatusResult {
	if !c.isConnected() {
		return StatusResult{Message: "Not connected to WhatsApp"}
	}
	msg, err := c.buildStatusMessage(ctx, opts)
	if err != nil {
		return StatusResult{Message: err.Error()}
	}
	aud, err := c.StatusAudience(ctx)
	if err != nil {
		return StatusResult{Message: err.Error()}
	}
	if err := c.checkStatusRate(ctx); err != nil {
		return StatusResult{Message: err.Error()}
	}
	resp, err := c.waSend(ctx, types.StatusBroadcastJID, msg)
	if err != nil {
		return StatusResult{Message: fmt.Sprintf("Error posting status: %v", err)}
	}
	c.log.Infof("Status posted to %d recipients (privacy=%s)", aud.Recipients, aud.PrivacyType)
	return StatusResult{
		Success:  true,
		Message:  fmt.Sprintf("Status posted to %d recipients (%s)", aud.Recipients, aud.PrivacyType),
		ID:       resp.ID,
		Audience: aud.Recipients,
	}
}

// buildStatusMessage validates opts and builds the WhatsApp message. Media is
// uploaded here, so validation runs before any network work or rate budget is
// spent on a request that is malformed.
func (c *Client) buildStatusMessage(ctx context.Context, opts PostStatusOptions) (*waE2E.Message, error) {
	text := strings.TrimSpace(opts.Text)
	hasText, hasMedia := text != "", opts.MediaPath != ""
	switch {
	case hasText == hasMedia:
		return nil, fmt.Errorf("provide exactly one of text or media_path")
	case hasText && opts.Caption != "":
		return nil, fmt.Errorf("caption only applies to media_path; use text for a text status")
	case !hasText && opts.BackgroundColor != "":
		return nil, fmt.Errorf("background_color only applies to a text status")
	}
	if hasText {
		if len([]rune(text)) > maxStatusTextLen {
			return nil, fmt.Errorf("text is %d characters; the limit is %d", len([]rune(text)), maxStatusTextLen)
		}
		ext := &waE2E.ExtendedTextMessage{Text: proto.String(text)}
		if opts.BackgroundColor != "" {
			argb, err := parseARGB(opts.BackgroundColor)
			if err != nil {
				return nil, err
			}
			ext.BackgroundArgb = proto.Uint32(argb)
		}
		return &waE2E.Message{ExtendedTextMessage: ext}, nil
	}
	safePath, err := c.ValidateMediaPath(opts.MediaPath)
	if err != nil {
		return nil, fmt.Errorf("media_path rejected: %w", err)
	}
	if mt, _ := mediaTypeFromExt(safePath); mt != whatsmeow.MediaImage && mt != whatsmeow.MediaVideo {
		return nil, fmt.Errorf("a status can only be an image or a video")
	}
	msg := &waE2E.Message{}
	if err := c.attachMedia(ctx, msg, safePath, opts.Caption, false); err != nil {
		return nil, err
	}
	return msg, nil
}

// parseARGB converts "#RRGGBB" (or "RRGGBB") to an opaque 0xAARRGGBB value.
func parseARGB(s string) (uint32, error) {
	hex := strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(hex) != 6 {
		return 0, fmt.Errorf("background_color must look like #RRGGBB, got %q", s)
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("background_color must look like #RRGGBB, got %q", s)
	}
	return 0xFF000000 | uint32(v), nil
}

// checkStatusRate applies the status-specific limiter. The operator override
// header bypasses it exactly as it does for sends (see checkSendRate).
func (c *Client) checkStatusRate(ctx context.Context) error {
	if c.limiter == nil {
		return nil
	}
	if ratelimit.BypassFromContext(ctx) {
		c.log.Warnf("RATE LIMIT OVERRIDE: status post bypassed the limiter via X-Rate-Limit-Override")
		return nil
	}
	if d := c.limiter.AllowStatus(); !d.Allowed {
		c.log.Warnf("Rate limited: status post denied (%s); retry in %s", d.Reason, d.RetryAfter.Round(time.Second))
		return fmt.Errorf("rate limited: %s — retry in %s", d.Reason, d.RetryAfter.Round(time.Second))
	}
	return nil
}

func (c *Client) statusPrivacy(ctx context.Context) ([]types.StatusPrivacy, error) {
	if c.statusPrivacyFn != nil {
		return c.statusPrivacyFn(ctx)
	}
	return c.wa.GetStatusPrivacy(ctx)
}

func (c *Client) statusRecipients(ctx context.Context) ([]types.JID, error) {
	if c.statusRecipientsFn != nil {
		return c.statusRecipientsFn(ctx)
	}
	return c.wa.DangerousInternals().GetStatusBroadcastRecipients(ctx)
}

func (c *Client) ownUser() string {
	if c.wa != nil && c.wa.Store != nil && c.wa.Store.ID != nil {
		return c.wa.Store.ID.User
	}
	return ""
}
