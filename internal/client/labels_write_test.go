package client

import (
	"context"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waSyncAction"
)

func TestLabelWrite_NotConnected(t *testing.T) {
	c := newDisconnectedClient()
	ctx := context.Background()
	if _, err := c.SetChatLabel(ctx, "447700000001", "1", true); err == nil {
		t.Fatal("SetChatLabel: expected not-connected error")
	} else {
		assertNotConnected(t, err)
	}
	if _, err := c.CreateLabel(ctx, "x", 0); err == nil {
		t.Fatal("CreateLabel: expected error")
	} else {
		assertNotConnected(t, err)
	}
	name := "x"
	if _, err := c.EditLabel(ctx, "1", &name, nil); err == nil {
		t.Fatal("EditLabel: expected error")
	} else {
		assertNotConnected(t, err)
	}
	if _, err := c.DeleteLabel(ctx, "1", "x"); err == nil {
		t.Fatal("DeleteLabel: expected error")
	} else {
		assertNotConnected(t, err)
	}
}

func TestValidateLabelName(t *testing.T) {
	if got, err := validateLabelName("  Cliente   VIP \n"); err != nil || got != "Cliente VIP" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := validateLabelName("   "); err == nil {
		t.Fatal("empty name must fail")
	}
	if _, err := validateLabelName(strings.Repeat("ñ", maxLabelNameRunes+1)); err == nil {
		t.Fatal("over-long name must fail (counted in characters, not bytes)")
	}
	if _, err := validateLabelName(strings.Repeat("ñ", maxLabelNameRunes)); err != nil {
		t.Fatalf("exactly the limit must pass: %v", err)
	}
}

func TestValidateLabelColor(t *testing.T) {
	for _, ok := range []int{0, 7, maxLabelColor} {
		if err := validateLabelColor(ok); err != nil {
			t.Fatalf("%d: %v", ok, err)
		}
	}
	for _, bad := range []int{-1, maxLabelColor + 1} {
		if err := validateLabelColor(bad); err == nil {
			t.Fatalf("%d must fail", bad)
		}
	}
}

func TestIsBuiltinLabel(t *testing.T) {
	typ := func(v waSyncAction.LabelEditAction_ListType) *waSyncAction.LabelEditAction {
		return &waSyncAction.LabelEditAction{Type: &v}
	}
	for _, v := range []waSyncAction.LabelEditAction_ListType{
		waSyncAction.LabelEditAction_UNREAD, waSyncAction.LabelEditAction_GROUPS, waSyncAction.LabelEditAction_FAVORITES,
	} {
		if !isBuiltinLabel(typ(v)) {
			t.Fatalf("%v must be built in", v)
		}
	}
	for _, v := range []waSyncAction.LabelEditAction_ListType{
		waSyncAction.LabelEditAction_NONE, waSyncAction.LabelEditAction_CUSTOM, waSyncAction.LabelEditAction_PREDEFINED,
	} {
		if isBuiltinLabel(typ(v)) {
			t.Fatalf("%v must be editable", v)
		}
	}
	imm := true
	if !isBuiltinLabel(&waSyncAction.LabelEditAction{IsImmutable: &imm}) {
		t.Fatal("IsImmutable must win")
	}
	if isBuiltinLabel(nil) {
		t.Fatal("nil action is not built in")
	}
}
