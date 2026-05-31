package correlation

import "testing"

func TestNewIDIsHexAndUnique(t *testing.T) {
	first := NewID()
	second := NewID()
	if len(first) != 32 {
		t.Fatalf("id length = %d, want 32", len(first))
	}
	if first == second {
		t.Fatal("generated duplicate correlation IDs")
	}
}

func TestFromHeaderRejectsUnsafeValues(t *testing.T) {
	if got := FromHeader("abc-123_DEF.:xyz"); got == "" {
		t.Fatal("expected safe correlation header to survive")
	}
	if got := FromHeader("abc\n123"); got != "" {
		t.Fatalf("unsafe header survived as %q", got)
	}
}
