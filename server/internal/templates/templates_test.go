package templates

import (
	"path/filepath"
	"testing"
)

func TestBuiltInTemplatesLoad(t *testing.T) {
	loaded, err := LoadDir(filepath.Join("..", "..", "templates", "pods"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"alpine-go":     false,
		"alpine-nodejs": false,
		"postgres":      false,
		"redfish":       false,
	}
	for _, template := range loaded {
		if _, ok := want[template.ID]; ok {
			want[template.ID] = true
		}
	}
	for id, seen := range want {
		if !seen {
			t.Fatalf("missing built-in template %s", id)
		}
	}
}

func TestLightweightTemplatesStayRunningByDefault(t *testing.T) {
	loaded, err := LoadDir(filepath.Join("..", "..", "templates", "pods"))
	if err != nil {
		t.Fatal(err)
	}
	commands := map[string][]string{}
	for _, template := range loaded {
		commands[template.ID] = template.Command
	}
	for _, id := range []string{"alpine-nodejs", "alpine-go"} {
		if len(commands[id]) == 0 {
			t.Fatalf("%s must define a long-running command for usable stats", id)
		}
	}
}
