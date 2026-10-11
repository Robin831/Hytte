package training

import "testing"

func TestSplitFencedObject(t *testing.T) {
	text, block, ok := SplitFencedObject("Done – I added a note.\n\n```json\n{\"update\": null}\n```\n")
	if !ok || text != "Done – I added a note." || block != `{"update": null}` {
		t.Fatalf("split = %q / %q / %v", text, block, ok)
	}
	if text, _, ok := SplitFencedObject("Just an answer, no change."); ok || text != "Just an answer, no change." {
		t.Fatalf("plain = %q / %v", text, ok)
	}
	if _, ok := ExtractFencedObject("Here:\n```\n[1, 2]\n```"); ok {
		t.Fatal("arrays are not objects")
	}
	if block, ok := ExtractFencedObject("x\n```\r\n{\"a\":1}\r\n```"); !ok || block != `{"a":1}` {
		t.Fatalf("CRLF block = %q / %v", block, ok)
	}
}
