package templates

import (
	"encoding/json"
	"path/filepath"
	"strings"
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
		if template.Command == nil || template.Ports == nil || template.Volumes == nil || template.Environment == nil || template.Secrets == nil || template.HealthCommand == nil || template.Labels == nil || template.UINotes == nil {
			t.Fatalf("%s has an unnormalized nil collection", template.ID)
		}
		content, err := json.Marshal(template)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), ":null") {
			t.Fatalf("%s serialized a null collection: %s", template.ID, string(content))
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
