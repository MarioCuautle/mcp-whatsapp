package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/sealjay/mcp-whatsapp/internal/client"
)

// registerStatusTools wires the status (story) tools: get_status_audience and
// post_status.
func (s *Server) registerStatusTools() {
	s.registerGetStatusAudience()
	s.registerPostStatus()
}

// -- get_status_audience ----------------------------------------------------

func (s *Server) registerGetStatusAudience() {
	tool := mcp.NewTool("get_status_audience",
		mcp.WithDescription("Report who a status (story) post would reach under the account's current status privacy, without posting anything; call it before post_status. Read-only and needs a live connection. Returns a JSON object `{PrivacyType, ListSize, Recipients}` where PrivacyType is `contacts`, `blacklist` or `whitelist`, ListSize is the length of that list, and Recipients is the number of resolved recipients including the paired account. No JIDs or names are returned."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
	)
	s.mcp.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, error) {
		aud, err := s.client.StatusAudience(ctx)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(aud)
	}))
}

// -- post_status ------------------------------------------------------------

type postStatusArgs struct {
	Text            string `json:"text,omitempty"`
	MediaPath       string `json:"media_path,omitempty"`
	Caption         string `json:"caption,omitempty"`
	BackgroundColor string `json:"background_color,omitempty"`
}

func (s *Server) registerPostStatus() {
	tool := mcp.NewTool("post_status",
		mcp.WithDescription("Publish a status (story) visible to everyone in the account's status audience, as a text, image or video. It is public to many contacts at once and cannot be recalled by this server, so only call it after the account owner has approved this exact content, and check get_status_audience first. It has its own stricter rate limit, separate from send_message. Returns a JSON object `{Success, Message, ID, Audience}` where Audience is the number of recipients."),
		mcp.WithString("text", mcp.Description("status text (max 700 characters); provide this or media_path, not both")),
		mcp.WithString("media_path", mcp.Description("absolute path to an image or video under WHATSAPP_MCP_MEDIA_ROOT; provide this or text, not both")),
		mcp.WithString("caption", mcp.Description("caption for the image or video; not allowed with text")),
		mcp.WithString("background_color", mcp.Description("background colour of a text status as #RRGGBB; not allowed with media_path")),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
	)
	s.mcp.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, req mcp.CallToolRequest, a postStatusArgs) (*mcp.CallToolResult, error) {
		ctx = withRateLimitOverride(ctx, req)
		r := s.client.PostStatus(ctx, client.PostStatusOptions{
			Text:            a.Text,
			MediaPath:       a.MediaPath,
			Caption:         a.Caption,
			BackgroundColor: a.BackgroundColor,
		})
		return resultJSON(r)
	}))
}
