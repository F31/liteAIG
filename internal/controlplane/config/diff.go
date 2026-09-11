package config

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type DiffOperation string

const (
	DiffAdded    DiffOperation = "added"
	DiffRemoved  DiffOperation = "removed"
	DiffModified DiffOperation = "modified"
)

type DiffEntry struct {
	Path      string        `json:"path"`
	Operation DiffOperation `json:"operation"`
	Before    string        `json:"before,omitempty"`
	After     string        `json:"after,omitempty"`
	Sensitive bool          `json:"sensitive"`
}

func SemanticDiff(before, after TenantConfig) ([]DiffEntry, error) {
	left, err := flatten(before)
	if err != nil {
		return nil, err
	}
	right, err := flatten(after)
	if err != nil {
		return nil, err
	}
	paths := make(map[string]struct{}, len(left)+len(right))
	for path := range left {
		paths[path] = struct{}{}
	}
	for path := range right {
		paths[path] = struct{}{}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)

	var result []DiffEntry
	for _, path := range ordered {
		oldValue, hadOld := left[path]
		newValue, hasNew := right[path]
		if hadOld && hasNew && oldValue == newValue {
			continue
		}
		entry := DiffEntry{Path: path, Before: oldValue, After: newValue, Sensitive: sensitivePath(path)}
		switch {
		case !hadOld:
			entry.Operation = DiffAdded
		case !hasNew:
			entry.Operation = DiffRemoved
		default:
			entry.Operation = DiffModified
		}
		if entry.Sensitive {
			if hadOld {
				entry.Before = "[redacted]"
			}
			if hasNew {
				entry.After = "[redacted]"
			}
		}
		result = append(result, entry)
	}
	return result, nil
}

func flatten(document TenantConfig) (map[string]string, error) {
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode config for diff: %w", err)
	}
	var value any
	if err := json.Unmarshal(encoded, &value); err != nil {
		return nil, fmt.Errorf("decode config for diff: %w", err)
	}
	result := make(map[string]string)
	flattenValue(value, "", result)
	return result, nil
}

func flattenValue(value any, path string, result map[string]string) {
	switch value := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			flattenValue(value[key], path+"/"+escapePointer(key), result)
		}
	case []any:
		for index, item := range value {
			flattenValue(item, fmt.Sprintf("%s/%d", path, index), result)
		}
	default:
		encoded, _ := json.Marshal(value)
		result[path] = string(encoded)
	}
}

func escapePointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func sensitivePath(path string) bool {
	parts := strings.Split(strings.ToLower(path), "/")
	name := parts[len(parts)-1]
	return name == "secret_ref" || name == "authorization" || strings.Contains(name, "password") || strings.Contains(name, "secret")
}
