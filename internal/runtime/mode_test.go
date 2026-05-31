package runtime

import "testing"

func TestResolveModeDefaultsToProduction(t *testing.T) {
	mode, err := ResolveMode(false, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if mode != Production {
		t.Fatalf("mode = %q, want %q", mode, Production)
	}
}

func TestResolveModeFlagsOverrideEnv(t *testing.T) {
	mode, err := ResolveMode(true, false, "production")
	if err != nil {
		t.Fatal(err)
	}
	if mode != Development {
		t.Fatalf("mode = %q, want %q", mode, Development)
	}
}

func TestResolveModeRejectsConflict(t *testing.T) {
	if _, err := ResolveMode(true, true, ""); err == nil {
		t.Fatal("expected conflicting mode flags to fail")
	}
}

func TestResolveModeRejectsInvalidEnv(t *testing.T) {
	if _, err := ResolveMode(false, false, "debug"); err == nil {
		t.Fatal("expected invalid env mode to fail")
	}
}
