package store

import (
	"context"
	"database/sql"
	"testing"
)

// openLabelsStore opens the fixture store and applies the labels schema, which
// seed.sql (a frozen copy of the original schema) does not contain.
func openLabelsStore(t *testing.T) *Store {
	t.Helper()
	s := openTestStore(t)
	if _, err := s.db.Exec(labelsSchema); err != nil {
		t.Fatalf("apply labels schema: %v", err)
	}
	return s
}

func TestLabels_RoundTrip(t *testing.T) {
	s := openLabelsStore(t)
	ctx := context.Background()

	if err := s.UpsertLabel(ctx, "1", "Cliente VIP", 3, false, false); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertLabel(ctx, "2", "Pendiente", 5, false, false); err != nil {
		t.Fatal(err)
	}
	alice := "447700000001@s.whatsapp.net"
	if err := s.SetChatLabel(ctx, "1", alice, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChatLabel(ctx, "1", alice, true); err != nil { // idempotent
		t.Fatal(err)
	}

	labels, err := s.ListLabels(ctx)
	if err != nil || len(labels) != 2 {
		t.Fatalf("ListLabels: %v %v", labels, err)
	}
	if labels[0].Name != "Cliente VIP" || labels[0].ChatCount != 1 || labels[1].ChatCount != 0 {
		t.Fatalf("unexpected labels: %+v", labels)
	}

	l, err := s.ResolveLabel(ctx, "cliente vip")
	if err != nil || l == nil || l.ID != "1" {
		t.Fatalf("ResolveLabel by name: %+v %v", l, err)
	}
	if l, _ := s.ResolveLabel(ctx, "2"); l == nil || l.Name != "Pendiente" {
		t.Fatalf("ResolveLabel by id: %+v", l)
	}
	if l, err := s.ResolveLabel(ctx, "nope"); l != nil || err != nil {
		t.Fatalf("missing label: %+v %v", l, err)
	}

	chats, err := s.ListChatsByLabel(ctx, "1", 10, 0, false)
	if err != nil || len(chats) != 1 || chats[0].JID != alice {
		t.Fatalf("ListChatsByLabel: %+v %v", chats, err)
	}
	// A labelled chat that is not in the message cache is still returned.
	ghost := "447799999999@s.whatsapp.net"
	if err := s.SetChatLabel(ctx, "1", ghost, true); err != nil {
		t.Fatal(err)
	}
	chats, err = s.ListChatsByLabel(ctx, "1", 10, 0, false)
	if err != nil || len(chats) != 2 || !chats[0].Cached || chats[1].Cached || chats[1].JID != ghost {
		t.Fatalf("uncached labelled chat not returned: %+v %v", chats, err)
	}
	if page, _ := s.ListChatsByLabel(ctx, "1", 1, 1, false); len(page) != 1 || page[0].JID != ghost {
		t.Fatalf("paging over combined list: %+v", page)
	}
	if err := s.SetChatLabel(ctx, "1", ghost, false); err != nil {
		t.Fatal(err)
	}
	got, err := s.LabelsForChat(ctx, alice)
	if err != nil || len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("LabelsForChat: %+v %v", got, err)
	}

	if err := s.SetChatLabel(ctx, "1", alice, false); err != nil {
		t.Fatal(err)
	}
	if chats, _ := s.ListChatsByLabel(ctx, "1", 10, 0, false); len(chats) != 0 {
		t.Fatalf("expected no chats after unlabel, got %+v", chats)
	}
}

func TestLabels_DeletedAndAmbiguous(t *testing.T) {
	s := openLabelsStore(t)
	ctx := context.Background()
	_ = s.UpsertLabel(ctx, "1", "Dup", 0, false, false)
	_ = s.UpsertLabel(ctx, "2", "dup", 0, false, false)
	if _, err := s.ResolveLabel(ctx, "DUP"); err == nil {
		t.Fatal("expected ambiguity error")
	}
	_ = s.SetChatLabel(ctx, "2", "447700000001@s.whatsapp.net", true)
	_ = s.UpsertLabel(ctx, "2", "dup", 0, true, false) // deleted
	if labels, _ := s.ListLabels(ctx); len(labels) != 1 {
		t.Fatalf("deleted label still listed: %+v", labels)
	}
	if chats, _ := s.ListChatsByLabel(ctx, "2", 10, 0, false); len(chats) != 0 {
		t.Fatal("associations of a deleted label must be dropped")
	}
}

func TestMeta(t *testing.T) {
	s := openLabelsStore(t)
	ctx := context.Background()
	if v, _ := s.GetMeta(ctx, "k"); v != "" {
		t.Fatalf("expected empty, got %q", v)
	}
	_ = s.SetMeta(ctx, "k", "1")
	_ = s.SetMeta(ctx, "k", "2")
	if v, _ := s.GetMeta(ctx, "k"); v != "2" {
		t.Fatalf("got %q", v)
	}
}

func TestNextLabelID_NeverReusesDeleted(t *testing.T) {
	s := openLabelsStore(t)
	ctx := context.Background()
	if id, err := s.NextLabelID(ctx); err != nil || id != "1" {
		t.Fatalf("empty table: %q %v", id, err)
	}
	_ = s.UpsertLabel(ctx, "3", "a", 0, false, false)
	_ = s.UpsertLabel(ctx, "25", "b", 0, true, false) // deleted ids still count
	_ = s.UpsertLabel(ctx, "weird", "c", 0, false, false)
	if id, err := s.NextLabelID(ctx); err != nil || id != "26" {
		t.Fatalf("got %q %v, want 26", id, err)
	}
}

func TestLabels_ImmutableFlagRoundTrip(t *testing.T) {
	s := openLabelsStore(t)
	ctx := context.Background()
	_ = s.UpsertLabel(ctx, "1", "Unread", 0, false, true)
	_ = s.UpsertLabel(ctx, "9", "Mine", 2, false, false)
	l, err := s.ResolveLabel(ctx, "Unread")
	if err != nil || l == nil || !l.Immutable {
		t.Fatalf("immutable not stored: %+v %v", l, err)
	}
	if l, _ := s.ResolveLabel(ctx, "Mine"); l == nil || l.Immutable {
		t.Fatalf("mutable label flagged immutable: %+v", l)
	}
}

func TestMigrateLabels_AddsImmutableToLegacyTable(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:migrate_labels_legacy?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE labels (id TEXT PRIMARY KEY, name TEXT NOT NULL DEFAULT '', color INTEGER NOT NULL DEFAULT 0, deleted BOOLEAN NOT NULL DEFAULT 0);
		INSERT INTO labels (id, name) VALUES ('7', 'Old');`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // idempotent
		if err := migrateLabels(db); err != nil {
			t.Fatalf("migrate run %d: %v", i, err)
		}
	}
	var imm bool
	if err := db.QueryRow("SELECT immutable FROM labels WHERE id = '7'").Scan(&imm); err != nil || imm {
		t.Fatalf("immutable after migration: %v %v", imm, err)
	}
}
