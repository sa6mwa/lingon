package config

import (
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ApplyOverrides applies dot-delimited overrides to cfg and returns the result.
func ApplyOverrides(cfg Config, overrides map[string]any) (Config, error) {
	if len(overrides) == 0 {
		return cfg, nil
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return Config{}, err
	}

	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return Config{}, err
	}

	for key, value := range overrides {
		if err := setOverride(root, key, value); err != nil {
			return Config{}, err
		}
	}

	merged, err := yaml.Marshal(root)
	if err != nil {
		return Config{}, err
	}

	var out Config
	if err := yaml.Unmarshal(merged, &out); err != nil {
		return Config{}, err
	}
	return out, nil
}

func setOverride(root map[string]any, path string, value any) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("override key is empty")
	}
	parts := strings.Split(path, ".")
	cursor := root
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return fmt.Errorf("override key %q contains empty segment", path)
		}
		if i == len(parts)-1 {
			cursor[part] = value
			return nil
		}
		next, ok := cursor[part]
		if !ok {
			child := map[string]any{}
			cursor[part] = child
			cursor = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			child = map[string]any{}
			cursor[part] = child
		}
		cursor = child
	}
	return nil
}
