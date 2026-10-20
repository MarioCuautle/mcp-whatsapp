package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerLabelTools wires the WhatsApp Business label tools: read-only lookups plus write tools.
// Labels are attached to chats, so lookups return chats, not contacts.
func (s *Server) registerLabelTools() {
	s.registerListLabels()
	s.registerListChatsByLabel()
	s.registerGetChatLabels()
	s.registerResyncLabels()
	s.registerLabelWriteTools()
}

func (s *Server) registerListLabels() {
	tool := mcp.NewTool("list_labels",
		mcp.WithDescription(offlineSafePrefix+"List the WhatsApp Business labels cached from the paired account, each with its id, name, colour index and the number of chats carrying it. Read-only; only available for WhatsApp Business accounts. If the list is empty the account has no labels or they have not synced yet; try resync_labels. Returns a JSON array of {id, name, color, chat_count}."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.mcp.AddTool(tool, func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		labels, err := s.client.Store().ListLabels(ctx)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(labels)
	})
}

type listChatsByLabelArgs struct {
	Label              string `json:"label"`
	Limit              int    `json:"limit"`
	Page               int    `json:"page"`
	IncludeLastMessage *bool  `json:"include_last_message"`
}

func (s *Server) registerListChatsByLabel() {
	tool := mcp.NewTool("list_chats_by_label",
		mcp.WithDescription(offlineSafePrefix+"List the chats that carry a given WhatsApp Business label, most recently active first. Read-only. The label may be given by id or by name (case-insensitive; an ambiguous name returns an error asking for the id). Labels attach to chats, so a contact with no chat has no label. Includes labelled chats that are not in the local message cache (cached=false; name from the address book when known, otherwise only the JID). Returns a JSON array of chat objects."),
		mcp.WithString("label", mcp.Required(), mcp.Description("label id or name, as returned by list_labels")),
		mcp.WithNumber("limit", mcp.Description("max chats to return (default 20)")),
		mcp.WithNumber("page", mcp.Description("zero-based page number (default 0)")),
		mcp.WithBoolean("include_last_message", mcp.Description("include the latest message of each chat (default true)")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.mcp.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, a listChatsByLabelArgs) (*mcp.CallToolResult, error) {
		if r := requireNonEmpty("label", a.Label); r != nil {
			return r, nil
		}
		label, err := s.client.Store().ResolveLabel(ctx, a.Label)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if label == nil {
			return mcp.NewToolResultError(fmt.Sprintf("no label matches %q; call list_labels", a.Label)), nil
		}
		includeLast := true
		if a.IncludeLastMessage != nil {
			includeLast = *a.IncludeLastMessage
		}
		chats, err := s.client.Store().ListChatsByLabel(ctx, label.ID, a.Limit, a.Page, includeLast)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(chats)
	}))
}

type getChatLabelsArgs struct {
	JID string `json:"jid"`
}

func (s *Server) registerGetChatLabels() {
	tool := mcp.NewTool("get_chat_labels",
		mcp.WithDescription(offlineSafePrefix+"List the WhatsApp Business labels attached to one chat. Read-only. Returns a JSON array of {id, name, color}."),
		mcp.WithString("jid", mcp.Required(), mcp.Description("chat JID, e.g. 5215512345678@s.whatsapp.net")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.mcp.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, a getChatLabelsArgs) (*mcp.CallToolResult, error) {
		if r := requireNonEmpty("jid", a.JID); r != nil {
			return r, nil
		}
		labels, err := s.client.Store().LabelsForChat(ctx, a.JID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(labels)
	}))
}

func (s *Server) registerResyncLabels() {
	tool := mcp.NewTool("resync_labels",
		mcp.WithDescription("Force a full re-fetch of the account's app state from WhatsApp so labels and label assignments are re-captured into the local cache. Does not change anything on the account and is idempotent, but can take several seconds and needs a live connection. Use it when list_labels is empty or looks stale. Returns the plain-text string `Labels resynced`."),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
	)
	s.mcp.AddTool(tool, func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := s.client.ResyncLabels(ctx); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText("Labels resynced"), nil
	})
}

// registerLabelWriteTools wires the tools that change labels on the account.
// They sync to the phone through app state, so they only work while connected.
func (s *Server) registerLabelWriteTools() {
	s.registerLabelChat()
	s.registerUnlabelChat()
	s.registerCreateLabel()
	s.registerEditLabel()
	s.registerDeleteLabel()
}

type labelChatArgs struct {
	Chat  string `json:"chat"`
	Label string `json:"label"`
}

func (s *Server) registerLabelChat() {
	s.registerLabelAssignment("label_chat", true,
		"Attach a WhatsApp Business label to a chat. Idempotent; reversible with unlabel_chat. Changes the account's labels on the phone. The label may be an id or a name from list_labels. Returns the plain-text string `Labelled <chat> with <label>`.")
}

func (s *Server) registerUnlabelChat() {
	s.registerLabelAssignment("unlabel_chat", false,
		"Remove a WhatsApp Business label from a chat. Idempotent; reversible with label_chat. Changes the account's labels on the phone. The label may be an id or a name from list_labels. Returns the plain-text string `Removed <label> from <chat>`.")
}

func (s *Server) registerLabelAssignment(name string, labeled bool, desc string) {
	tool := mcp.NewTool(name,
		mcp.WithDescription(desc),
		mcp.WithString("chat", mcp.Required(), mcp.Description(recipientDesc)),
		mcp.WithString("label", mcp.Required(), mcp.Description("label id or name, as returned by list_labels")),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
	)
	s.mcp.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, a labelChatArgs) (*mcp.CallToolResult, error) {
		if r := requireNonEmpty("chat", a.Chat); r != nil {
			return r, nil
		}
		if r := requireNonEmpty("label", a.Label); r != nil {
			return r, nil
		}
		l, err := s.client.SetChatLabel(ctx, a.Chat, a.Label, labeled)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if labeled {
			return mcp.NewToolResultText(fmt.Sprintf("Labelled %s with %s", a.Chat, l.Name)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Removed %s from %s", l.Name, a.Chat)), nil
	}))
}

type createLabelArgs struct {
	Name  string `json:"name"`
	Color *int   `json:"color"`
}

func (s *Server) registerCreateLabel() {
	tool := mcp.NewTool("create_label",
		mcp.WithDescription("Create a new WhatsApp Business label on the account (name up to 20 characters, colour index 0-19, default 0). Fails if a label with that name already exists. Changes the account's labels on the phone; reversible with delete_label. Returns a JSON object {id, name, color}."),
		mcp.WithString("name", mcp.Required(), mcp.Description("label name, at most 20 characters")),
		mcp.WithNumber("color", mcp.Description("colour index 0-19 (default 0)")),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
	)
	s.mcp.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, a createLabelArgs) (*mcp.CallToolResult, error) {
		if r := requireNonEmpty("name", a.Name); r != nil {
			return r, nil
		}
		color := 0
		if a.Color != nil {
			color = *a.Color
		}
		l, err := s.client.CreateLabel(ctx, a.Name, color)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(l)
	}))
}

type editLabelArgs struct {
	Label string  `json:"label"`
	Name  *string `json:"name"`
	Color *int    `json:"color"`
}

func (s *Server) registerEditLabel() {
	tool := mcp.NewTool("edit_label",
		mcp.WithDescription("Rename and/or recolour an existing WhatsApp Business label (name up to 20 characters, colour index 0-19). Pass at least one of name or color. Built-in labels cannot be edited. Changes the account's labels on the phone; reversible by editing again. Returns a JSON object {id, name, color}."),
		mcp.WithString("label", mcp.Required(), mcp.Description("label id or name, as returned by list_labels")),
		mcp.WithString("name", mcp.Description("new name, at most 20 characters")),
		mcp.WithNumber("color", mcp.Description("new colour index 0-19")),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
	)
	s.mcp.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, a editLabelArgs) (*mcp.CallToolResult, error) {
		if r := requireNonEmpty("label", a.Label); r != nil {
			return r, nil
		}
		l, err := s.client.EditLabel(ctx, a.Label, a.Name, a.Color)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return resultJSON(l)
	}))
}

type deleteLabelArgs struct {
	Label       string `json:"label"`
	ConfirmName string `json:"confirm_name"`
}

func (s *Server) registerDeleteLabel() {
	tool := mcp.NewTool("delete_label",
		mcp.WithDescription("DESTRUCTIVE: delete a WhatsApp Business label from the account, removing it from every chat that carries it. To guard against mistakes, `confirm_name` must equal the label's current name. Built-in labels cannot be deleted. Changes the account's labels on the phone. Not reversible by this server (re-create the label and re-assign it to each chat). Returns the plain-text string `Deleted label <name>`."),
		mcp.WithString("label", mcp.Required(), mcp.Description("label id or name, as returned by list_labels")),
		mcp.WithString("confirm_name", mcp.Required(), mcp.Description("the label's exact current name, to confirm the deletion")),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(true),
	)
	s.mcp.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, a deleteLabelArgs) (*mcp.CallToolResult, error) {
		if r := requireNonEmpty("label", a.Label); r != nil {
			return r, nil
		}
		if r := requireNonEmpty("confirm_name", a.ConfirmName); r != nil {
			return r, nil
		}
		l, err := s.client.DeleteLabel(ctx, a.Label, a.ConfirmName)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Deleted label %s", l.Name)), nil
	}))
}
