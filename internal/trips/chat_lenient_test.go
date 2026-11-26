package trips

import (
	"strings"
	"testing"
)

// The assistant sometimes writes a plain-text field (here a hotel's name and
// phone) as a {"nb","en","th"} object. The change must still be read.
func TestSplitReplyForgivesTranslatedPlainFields(t *testing.T) {
	reply := "Lagt inn hotellet.\n```json\n" + `{"update": {"kind": "family", "start_date": "2026-12-01", "end_date": "2026-12-03", "doc": {
		"stays": [
			{"name": {"nb": "Hotell Oslo", "en": "Hotel Oslo", "th": "โรงแรมออสโล"}, "phone": {"en": "+47 123"}, "check_in": "2026-12-01T15:00", "tz": "Europe/Oslo", "note": {"nb": "Frokost inkludert", "en": "Breakfast included"}},
			{"name": "Second Hotel"}
		]
	}}, "checklist": [], "search_prices": null, "remember": [], "forget": []}` + "\n```"

	text, change, err := splitReply(reply)
	if err != nil {
		t.Fatalf("splitReply: %v", err)
	}
	if text != "Lagt inn hotellet." || change == nil || change.Update == nil {
		t.Fatalf("text %q, change %+v", text, change)
	}
	stays := change.Update.Doc.Stays
	if len(stays) != 2 || stays[0].Name != "Hotell Oslo" || stays[0].Phone != "+47 123" || stays[1].Name != "Second Hotel" {
		t.Fatalf("stays = %+v", stays)
	}
	// Real translated texts are left alone.
	if stays[0].Note["en"] != "Breakfast included" {
		t.Fatalf("note = %v", stays[0].Note)
	}

	// Go versions differ on whether the error's field path names the list
	// position ("stays.0.name") or not ("stays.name"); both must work.
	for _, path := range []string{"doc.stays.1.name", "doc.stays.name"} {
		raw := map[string]any{"doc": map[string]any{"stays": []any{
			map[string]any{"name": "Plain"},
			map[string]any{"name": map[string]any{"en": "Hotel", "nb": "Hotell"}},
		}}}
		if !collapseText(raw, strings.Split(path, ".")) {
			t.Fatalf("%s: nothing collapsed", path)
		}
		got := raw["doc"].(map[string]any)["stays"].([]any)
		if got[0].(map[string]any)["name"] != "Plain" || got[1].(map[string]any)["name"] != "Hotell" {
			t.Fatalf("%s: stays = %v", path, got)
		}
	}

	// Anything else that doesn't fit is still an error.
	if _, _, err := splitReply("x\n```json\n{\"update\": {\"doc\": {\"stays\": \"nope\"}}}\n```"); err == nil {
		t.Fatal("a string where a list belongs must still fail")
	}
}
