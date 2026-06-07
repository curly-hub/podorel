package templates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const DefaultPodTemplateDir = "server/templates/pods"

type Template struct {
	ID             string            `json:"id"`
	Version        string            `json:"version"`
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Image          string            `json:"image"`
	Command        []string          `json:"command"`
	Ports          []Port            `json:"ports"`
	Volumes        []Volume          `json:"volumes"`
	Environment    map[string]string `json:"environment"`
	Secrets        []SecretRef       `json:"secrets"`
	HealthCommand  []string          `json:"health_command"`
	ResourceLimits ResourceLimits    `json:"resource_limits"`
	RestartPolicy  string            `json:"restart_policy"`
	Labels         map[string]string `json:"labels"`
	UINotes        []string          `json:"ui_notes"`
	Custom         bool              `json:"custom,omitempty"`
}

type Port struct {
	Host      int    `json:"host"`
	Container int    `json:"container"`
	Protocol  string `json:"protocol"`
}

type Volume struct {
	HostPath      string `json:"host_path"`
	ContainerPath string `json:"container_path"`
	ReadOnly      bool   `json:"read_only"`
}

type SecretRef struct {
	Name        string `json:"name"`
	Target      string `json:"target"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

type ResourceLimits struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

func LoadDir(dir string) ([]Template, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var loaded []Template
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var template Template
		if err := json.Unmarshal(content, &template); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		template.Normalize()
		if err := template.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		loaded = append(loaded, template)
	}
	sort.Slice(loaded, func(i, j int) bool {
		return loaded[i].ID < loaded[j].ID
	})
	return loaded, nil
}

func (t *Template) Normalize() {
	if t.Command == nil {
		t.Command = []string{}
	}
	if t.Ports == nil {
		t.Ports = []Port{}
	}
	if t.Volumes == nil {
		t.Volumes = []Volume{}
	}
	if t.Environment == nil {
		t.Environment = map[string]string{}
	}
	if t.Secrets == nil {
		t.Secrets = []SecretRef{}
	}
	if t.HealthCommand == nil {
		t.HealthCommand = []string{}
	}
	if t.Labels == nil {
		t.Labels = map[string]string{}
	}
	if t.UINotes == nil {
		t.UINotes = []string{}
	}
}

func (t Template) Validate() error {
	required := map[string]string{
		"id":             t.ID,
		"version":        t.Version,
		"name":           t.Name,
		"description":    t.Description,
		"image":          t.Image,
		"restart_policy": t.RestartPolicy,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("missing required field %s", field)
		}
	}
	for _, port := range t.Ports {
		if port.Container <= 0 || port.Protocol == "" {
			return fmt.Errorf("invalid port mapping for template %s", t.ID)
		}
	}
	return nil
}
