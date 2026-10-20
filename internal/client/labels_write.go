package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"go.mau.fi/whatsmeow/appstate"

	"github.com/sealjay/mcp-whatsapp/internal/store"
)

const (
	maxLabelNameRunes = 20 // WhatsApp Business caps label names at 20 characters
	maxLabelColor     = 19 // colour indices 0-19 (the palette offered by the app)
)

// requireLabel resolves a label by id or name and rejects unknown ones.
func (c *Client) requireLabel(ctx context.Context, ref string) (*store.Label, error) {
	if strings.TrimSpace(ref) == "" {
		return nil, errors.New("label: required")
	}
	l, err := c.store.ResolveLabel(ctx, ref)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, fmt.Errorf("no label matches %q; call list_labels (if you just created it elsewhere, call resync_labels)", ref)
	}
	return l, nil
}

func validateLabelName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "", errors.New("name: required")
	}
	if n := utf8.RuneCountInString(name); n > maxLabelNameRunes {
		return "", fmt.Errorf("name: %d characters, WhatsApp allows at most %d", n, maxLabelNameRunes)
	}
	return name, nil
}

func validateLabelColor(color int) error {
	if color < 0 || color > maxLabelColor {
		return fmt.Errorf("color: must be between 0 and %d", maxLabelColor)
	}
	return nil
}

// SetChatLabel adds (labeled=true) or removes a label from a chat, syncing the
// change to the phone through app state, then mirrors it in the local cache.
func (c *Client) SetChatLabel(ctx context.Context, chatRaw, labelRef string, labeled bool) (*store.Label, error) {
	if !c.wa.IsConnected() {
		return nil, errors.New("not connected to WhatsApp")
	}
	label, err := c.requireLabel(ctx, labelRef)
	if err != nil {
		return nil, err
	}
	chat, err := parseRecipient(chatRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid chat: %w", err)
	}
	if err := c.wa.SendAppState(ctx, appstate.BuildLabelChat(chat, label.ID, labeled)); err != nil {
		return nil, fmt.Errorf("sync label to WhatsApp: %w", err)
	}
	chatJID := chat.String()
	if chat.Server == "lid" {
		chatJID = c.store.ResolveLIDToJID(chatJID)
	}
	if err := c.store.SetChatLabel(ctx, label.ID, chatJID, labeled); err != nil {
		return nil, fmt.Errorf("update local cache: %w", err)
	}
	return label, nil
}

// CreateLabel creates a new label with the next free id and syncs it to the phone.
func (c *Client) CreateLabel(ctx context.Context, name string, color int) (*store.Label, error) {
	if !c.wa.IsConnected() {
		return nil, errors.New("not connected to WhatsApp")
	}
	name, err := validateLabelName(name)
	if err != nil {
		return nil, err
	}
	if err := validateLabelColor(color); err != nil {
		return nil, err
	}
	if dup, err := c.store.ResolveLabel(ctx, name); err != nil {
		return nil, fmt.Errorf("a label named %q already exists (ambiguous); use a different name", name)
	} else if dup != nil {
		return nil, fmt.Errorf("a label named %q already exists (id %s)", name, dup.ID)
	}
	id, err := c.store.NextLabelID(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.wa.SendAppState(ctx, appstate.BuildLabelEdit(id, name, int32(color), false)); err != nil {
		return nil, fmt.Errorf("sync label to WhatsApp: %w", err)
	}
	if err := c.store.UpsertLabel(ctx, id, name, color, false, false); err != nil {
		return nil, fmt.Errorf("update local cache: %w", err)
	}
	return &store.Label{ID: id, Name: name, Color: color}, nil
}

// EditLabel renames and/or recolours a label. A nil argument keeps the current value.
func (c *Client) EditLabel(ctx context.Context, labelRef string, name *string, color *int) (*store.Label, error) {
	if !c.wa.IsConnected() {
		return nil, errors.New("not connected to WhatsApp")
	}
	label, err := c.requireLabel(ctx, labelRef)
	if err != nil {
		return nil, err
	}
	if label.Immutable {
		return nil, fmt.Errorf("label %q is built in and cannot be edited", label.Name)
	}
	newName, newColor := label.Name, label.Color
	if name != nil {
		if newName, err = validateLabelName(*name); err != nil {
			return nil, err
		}
	}
	if color != nil {
		if err := validateLabelColor(*color); err != nil {
			return nil, err
		}
		newColor = *color
	}
	if name == nil && color == nil {
		return nil, errors.New("nothing to change: pass name and/or color")
	}
	if err := c.wa.SendAppState(ctx, appstate.BuildLabelEdit(label.ID, newName, int32(newColor), false)); err != nil {
		return nil, fmt.Errorf("sync label to WhatsApp: %w", err)
	}
	if err := c.store.UpsertLabel(ctx, label.ID, newName, newColor, false, false); err != nil {
		return nil, fmt.Errorf("update local cache: %w", err)
	}
	label.Name, label.Color = newName, newColor
	return label, nil
}

// DeleteLabel deletes a label from the account (removing it from every chat).
// confirmName must equal the label's current name, so a mistaken id or an
// injected instruction cannot delete the wrong label.
func (c *Client) DeleteLabel(ctx context.Context, labelRef, confirmName string) (*store.Label, error) {
	if !c.wa.IsConnected() {
		return nil, errors.New("not connected to WhatsApp")
	}
	label, err := c.requireLabel(ctx, labelRef)
	if err != nil {
		return nil, err
	}
	if label.Immutable {
		return nil, fmt.Errorf("label %q is built in and cannot be deleted", label.Name)
	}
	if !strings.EqualFold(strings.TrimSpace(confirmName), label.Name) {
		return nil, fmt.Errorf("confirm_name must equal the label's name (%q) to delete it; it is currently on %d chat(s)", label.Name, label.ChatCount)
	}
	if err := c.wa.SendAppState(ctx, appstate.BuildLabelEdit(label.ID, label.Name, int32(label.Color), true)); err != nil {
		return nil, fmt.Errorf("sync label to WhatsApp: %w", err)
	}
	if err := c.store.UpsertLabel(ctx, label.ID, label.Name, label.Color, true, false); err != nil {
		return nil, fmt.Errorf("update local cache: %w", err)
	}
	return label, nil
}
