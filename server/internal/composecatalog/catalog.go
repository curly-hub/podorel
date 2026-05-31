package composecatalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	DefaultComposeTemplateDir = "server/templates/compose"
	ManifestName              = "podorel-compose.json"
	maxBundleFileBytes        = 2 * 1024 * 1024
)

type Stack struct {
	ID               string            `json:"id"`
	Version          string            `json:"version"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	SourcePath       string            `json:"source_path"`
	ComposeFiles     []string          `json:"compose_files"`
	Services         []Service         `json:"services"`
	EnvironmentFiles []string          `json:"environment_files"`
	RequiredFiles    []string          `json:"required_files"`
	Notes            []string          `json:"notes"`
	Labels           map[string]string `json:"labels"`

	rootDir string
}

type Service struct {
	Name          string   `json:"name"`
	Image         string   `json:"image,omitempty"`
	Build         string   `json:"build,omitempty"`
	ContainerName string   `json:"container_name,omitempty"`
	Restart       string   `json:"restart,omitempty"`
	Ports         []string `json:"ports,omitempty"`
	Profiles      []string `json:"profiles,omitempty"`
}

type BundleFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func LoadDir(dir string) ([]Stack, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Stack{}, nil
		}
		return nil, err
	}
	if len(entries) == 0 {
		return []Stack{}, nil
	}

	var stacks []Stack
	err = filepath.WalkDir(dir, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if shouldSkipCatalogDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() != ManifestName {
			return nil
		}
		stack, err := loadManifest(current)
		if err != nil {
			return err
		}
		stacks = append(stacks, stack)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(stacks, func(i, j int) bool {
		return stacks[i].ID < stacks[j].ID
	})
	return stacks, nil
}

func BundleFiles(stack Stack) ([]BundleFile, error) {
	if stack.rootDir == "" {
		return nil, fmt.Errorf("compose stack %s has no source directory", stack.ID)
	}
	var files []BundleFile
	err := filepath.WalkDir(stack.rootDir, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := entry.Name()
		if entry.IsDir() {
			if shouldSkipBundleDir(name) {
				return filepath.SkipDir
			}
			return nil
		}
		if name == ManifestName || shouldSkipBundleFile(name) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxBundleFileBytes {
			return fmt.Errorf("%s is too large for a compose migration bundle", current)
		}
		rel, err := filepath.Rel(stack.rootDir, current)
		if err != nil {
			return err
		}
		clean, err := CleanBundlePath(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		content, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		files = append(files, BundleFile{Path: clean, Content: string(content)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files, nil
}

func PreviewCommand(stack Stack, projectName string) []string {
	command := []string{"podman", "compose", "-p", projectName}
	for _, composeFile := range stack.ComposeFiles {
		command = append(command, "-f", composeFile)
	}
	return append(command, "up", "-d")
}

func CleanBundlePath(value string) (string, error) {
	trimmed := strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if trimmed == "" {
		return "", fmt.Errorf("bundle path is required")
	}
	clean := path.Clean(trimmed)
	if clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("unsafe bundle path %q", value)
	}
	return clean, nil
}

func loadManifest(path string) (Stack, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Stack{}, err
	}
	var stack Stack
	if err := json.Unmarshal(content, &stack); err != nil {
		return Stack{}, fmt.Errorf("%s: %w", path, err)
	}
	stack.rootDir = filepath.Dir(path)
	if err := stack.validate(); err != nil {
		return Stack{}, fmt.Errorf("%s: %w", path, err)
	}
	return stack, nil
}

func (s *Stack) validate() error {
	required := map[string]string{
		"id":          s.ID,
		"version":     s.Version,
		"name":        s.Name,
		"description": s.Description,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("missing required field %s", field)
		}
	}
	if len(s.ComposeFiles) == 0 {
		return fmt.Errorf("compose stack %s must declare at least one compose file", s.ID)
	}
	for i, composeFile := range s.ComposeFiles {
		clean, err := CleanBundlePath(composeFile)
		if err != nil {
			return err
		}
		s.ComposeFiles[i] = clean
		if _, err := os.Stat(filepath.Join(s.rootDir, filepath.FromSlash(clean))); err != nil {
			return fmt.Errorf("compose file %s: %w", clean, err)
		}
	}
	for _, required := range s.RequiredFiles {
		clean, err := CleanBundlePath(required)
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(s.rootDir, filepath.FromSlash(clean))); err != nil {
			return fmt.Errorf("required file %s: %w", clean, err)
		}
	}
	for _, service := range s.Services {
		if strings.TrimSpace(service.Name) == "" {
			return fmt.Errorf("compose stack %s has a service without a name", s.ID)
		}
	}
	return nil
}

func shouldSkipCatalogDir(name string) bool {
	switch name {
	case ".git", "node_modules", "__pycache__":
		return true
	default:
		return false
	}
}

func shouldSkipBundleDir(name string) bool {
	switch name {
	case ".git", "node_modules", "__pycache__", ".venv", "venv", "data", "downloads", "ollama", "models":
		return true
	default:
		return false
	}
}

func shouldSkipBundleFile(name string) bool {
	if name == ".env.example" || name == ".env.sample" {
		return false
	}
	return name == ".env" || strings.HasPrefix(name, ".env.")
}
