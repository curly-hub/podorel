package composecatalog

import (
	"path/filepath"
	"testing"
)

func TestBuiltInComposeStacksLoad(t *testing.T) {
	loaded, err := LoadDir(filepath.Join("..", "..", "templates", "compose"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"example-hello-web": false,
	}
	for _, stack := range loaded {
		if _, ok := want[stack.ID]; ok {
			want[stack.ID] = true
		}
	}
	for id, seen := range want {
		if !seen {
			t.Fatalf("missing built-in compose stack %s", id)
		}
	}
}

func TestBundleFilesIncludeExamplesButExcludeLiveEnv(t *testing.T) {
	loaded, err := LoadDir(filepath.Join("..", "..", "templates", "compose"))
	if err != nil {
		t.Fatal(err)
	}
	var example Stack
	for _, stack := range loaded {
		if stack.ID == "example-hello-web" {
			example = stack
			break
		}
	}
	files, err := BundleFiles(example)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, file := range files {
		seen[file.Path] = true
		if file.Path == ".env" {
			t.Fatal("live .env file must not be bundled")
		}
	}
	if !seen[".env.example"] {
		t.Fatal(".env.example should travel with compose bundles")
	}
	if !seen["docker-compose.yml"] {
		t.Fatal("docker-compose.yml should travel with compose bundles")
	}
}
