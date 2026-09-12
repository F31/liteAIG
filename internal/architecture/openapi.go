package architecture

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// OpenAPIDocument is the dependency-free representation used by the
// compatibility gate. Keeping the document as JSON also preserves OpenAPI 3.1
// features that a narrower Go model might discard.
type OpenAPIDocument map[string]any

// LoadOpenAPI reads and validates an OpenAPI 3.1 JSON document.
func LoadOpenAPI(path string) (OpenAPIDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var document OpenAPIDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	version, _ := document["openapi"].(string)
	if !strings.HasPrefix(version, "3.1.") {
		return nil, fmt.Errorf("%s: openapi version must be 3.1.x", path)
	}
	if _, ok := object(document["paths"]); !ok {
		return nil, fmt.Errorf("%s: paths object is required", path)
	}
	return document, nil
}

// CompareOpenAPIFiles loads two snapshots and reports breaking changes.
func CompareOpenAPIFiles(beforePath, afterPath string) ([]Violation, error) {
	before, err := LoadOpenAPI(beforePath)
	if err != nil {
		return nil, err
	}
	after, err := LoadOpenAPI(afterPath)
	if err != nil {
		return nil, err
	}
	return CompareOpenAPI(before, after), nil
}

var openAPIMethods = []string{"delete", "get", "head", "options", "patch", "post", "put", "trace"}

// CompareOpenAPI returns changes that break clients of the before document.
// Additive paths, methods, response properties, and optional request
// properties are compatible.
func CompareOpenAPI(before, after OpenAPIDocument) []Violation {
	c := openAPIComparator{before: before, after: after, seen: make(map[string]bool)}
	beforePaths, _ := object(before["paths"])
	afterPaths, _ := object(after["paths"])
	for _, path := range sortedKeys(beforePaths) {
		beforeItem, _ := c.resolve(before, beforePaths[path])
		afterValue, exists := afterPaths[path]
		if !exists {
			c.add("openapi-path-removed", path, "path was removed")
			continue
		}
		afterItem, _ := c.resolve(after, afterValue)
		for _, method := range openAPIMethods {
			beforeOperation, exists := beforeItem[method]
			if !exists {
				continue
			}
			operation := strings.ToUpper(method) + " " + path
			afterOperation, exists := afterItem[method]
			if !exists {
				c.add("openapi-method-removed", operation, "operation was removed")
				continue
			}
			c.compareOperation(operation, beforeOperation, afterOperation)
		}
	}
	SortViolations(c.violations)
	return c.violations
}

type openAPIComparator struct {
	before, after OpenAPIDocument
	violations    []Violation
	seen          map[string]bool
}

func (c *openAPIComparator) add(rule, operation, detail string) {
	c.violations = append(c.violations, Violation{Rule: rule, Package: operation, Detail: detail})
}

func (c *openAPIComparator) compareOperation(operation string, beforeValue, afterValue any) {
	before, _ := c.resolve(c.before, beforeValue)
	after, _ := c.resolve(c.after, afterValue)
	c.compareParameters(operation, before["parameters"], after["parameters"])
	beforeSecurity, beforeHasSecurity := before["security"]
	afterSecurity, afterHasSecurity := after["security"]
	if (!beforeHasSecurity || emptyArray(beforeSecurity)) && afterHasSecurity && !emptyArray(afterSecurity) {
		c.add("openapi-security-required", operation, "operation now requires authentication")
	}
	c.compareRequestBody(operation, before["requestBody"], after["requestBody"])

	beforeResponses, _ := object(before["responses"])
	afterResponses, _ := object(after["responses"])
	for _, status := range sortedKeys(beforeResponses) {
		beforeResponse, _ := c.resolve(c.before, beforeResponses[status])
		afterValue, exists := afterResponses[status]
		if !exists {
			c.add("openapi-response-removed", operation, "response "+status+" was removed")
			continue
		}
		afterResponse, _ := c.resolve(c.after, afterValue)
		c.compareContent(operation, "response "+status, beforeResponse["content"], afterResponse["content"], false)
	}
}

func (c *openAPIComparator) compareParameters(operation string, beforeValue, afterValue any) {
	after := make(map[string]any)
	if values, ok := afterValue.([]any); ok {
		for _, value := range values {
			parameter, _ := c.resolve(c.after, value)
			name, _ := parameter["name"].(string)
			location, _ := parameter["in"].(string)
			if name != "" && location != "" {
				after[location+":"+name] = value
			}
		}
	}
	values, _ := beforeValue.([]any)
	for _, value := range values {
		parameter, _ := c.resolve(c.before, value)
		name, _ := parameter["name"].(string)
		location, _ := parameter["in"].(string)
		key := location + ":" + name
		afterValue, exists := after[key]
		if !exists {
			c.add("openapi-parameter-removed", operation, "parameter "+key+" was removed")
			continue
		}
		afterParameter, _ := c.resolve(c.after, afterValue)
		beforeRequired, _ := parameter["required"].(bool)
		afterRequired, _ := afterParameter["required"].(bool)
		if !beforeRequired && afterRequired {
			c.add("openapi-parameter-required", operation, "optional parameter "+key+" became required")
		}
		c.compareSchema(operation, "parameter "+key, parameter["schema"], afterParameter["schema"], true)
	}
}

func (c *openAPIComparator) compareRequestBody(operation string, beforeValue, afterValue any) {
	if beforeValue == nil {
		return
	}
	before, _ := c.resolve(c.before, beforeValue)
	after, _ := c.resolve(c.after, afterValue)
	if after == nil {
		c.add("openapi-request-body-removed", operation, "request body was removed")
		return
	}
	beforeRequired, _ := before["required"].(bool)
	afterRequired, _ := after["required"].(bool)
	if !beforeRequired && afterRequired {
		c.add("openapi-request-body-required", operation, "optional request body became required")
	}
	c.compareContent(operation, "request", before["content"], after["content"], true)
}

func (c *openAPIComparator) compareContent(operation, location string, beforeValue, afterValue any, request bool) {
	before, _ := object(beforeValue)
	after, _ := object(afterValue)
	for _, mediaType := range sortedKeys(before) {
		afterMedia, exists := after[mediaType]
		if !exists {
			kind := "response"
			if request {
				kind = "request"
			}
			c.add("openapi-"+kind+"-media-type-removed", operation, location+" media type "+mediaType+" was removed")
			continue
		}
		beforeMedia, _ := c.resolve(c.before, before[mediaType])
		afterMediaObject, _ := c.resolve(c.after, afterMedia)
		c.compareSchema(operation, location+" "+mediaType, beforeMedia["schema"], afterMediaObject["schema"], request)
	}
}

func (c *openAPIComparator) compareSchema(operation, location string, beforeValue, afterValue any, request bool) {
	if beforeValue != nil && afterValue == nil {
		kind := "response"
		if request {
			kind = "request"
		}
		c.add("openapi-"+kind+"-schema-removed", operation, location+" schema was removed")
		return
	}
	before, beforeID := c.resolve(c.before, beforeValue)
	after, afterID := c.resolve(c.after, afterValue)
	if before == nil || after == nil {
		return
	}
	if beforeID != "" || afterID != "" {
		key := operation + "|" + strconv.FormatBool(request) + "|" + beforeID + "|" + afterID
		if c.seen[key] {
			return
		}
		c.seen[key] = true
	}

	beforeTypes, beforeNull := schemaTypes(before)
	afterTypes, afterNull := schemaTypes(after)
	typeChanged := len(beforeTypes) > 0 && len(afterTypes) > 0 && !equalStrings(beforeTypes, afterTypes)
	if request && len(beforeTypes) == 0 && len(afterTypes) > 0 {
		typeChanged = true
	}
	if !request && len(beforeTypes) > 0 && len(afterTypes) == 0 {
		typeChanged = true
	}
	if typeChanged {
		kind := "response"
		if request {
			kind = "request"
		}
		beforeDescription, afterDescription := strings.Join(beforeTypes, "|"), strings.Join(afterTypes, "|")
		if beforeDescription == "" {
			beforeDescription = "any"
		}
		if afterDescription == "" {
			afterDescription = "any"
		}
		c.add("openapi-"+kind+"-type-changed", operation, location+" type changed from "+beforeDescription+" to "+afterDescription)
	}
	if request && beforeNull && !afterNull {
		c.add("openapi-request-nullability-narrowed", operation, location+" no longer accepts null")
	}
	if !request && !beforeNull && afterNull {
		c.add("openapi-response-nullability-widened", operation, location+" may now be null")
	}

	c.compareEnums(operation, location, before, after, request)
	if request {
		c.compareRequestBounds(operation, location, before, after)
	}

	beforeProperties, _ := object(before["properties"])
	afterProperties, _ := object(after["properties"])
	beforeRequired := stringSet(before["required"])
	afterRequired := stringSet(after["required"])
	if request {
		for _, name := range sortedSetDifference(afterRequired, beforeRequired) {
			c.add("openapi-request-property-required", operation, location+" property "+name+" became required")
		}
	} else {
		for _, name := range sortedSetDifference(beforeRequired, afterRequired) {
			c.add("openapi-response-property-optional", operation, location+" required property "+name+" became optional")
		}
	}
	for _, name := range sortedKeys(beforeProperties) {
		afterProperty, exists := afterProperties[name]
		if !exists {
			kind := "response"
			if request {
				kind = "request"
			}
			c.add("openapi-"+kind+"-property-removed", operation, location+" property "+name+" was removed")
			continue
		}
		c.compareSchema(operation, location+" property "+name, beforeProperties[name], afterProperty, request)
	}
	if beforeItems, exists := before["items"]; exists {
		afterItems, ok := after["items"]
		if !ok {
			kind := "response"
			if request {
				kind = "request"
			}
			c.add("openapi-"+kind+"-array-items-removed", operation, location+" array item schema was removed")
		} else {
			c.compareSchema(operation, location+" items", beforeItems, afterItems, request)
		}
	}
}

func emptyArray(value any) bool {
	items, ok := value.([]any)
	return ok && len(items) == 0
}

func (c *openAPIComparator) compareEnums(operation, location string, before, after map[string]any, request bool) {
	beforeEnum, beforeOK := enumSet(before["enum"])
	afterEnum, afterOK := enumSet(after["enum"])
	if !beforeOK {
		return
	}
	extensible, _ := before["x-liteaig-extensible-enum"].(bool)
	if !request && extensible {
		return
	}
	for _, value := range sortedSetDifference(beforeEnum, afterEnum) {
		kind := "response"
		if request {
			kind = "request"
		}
		c.add("openapi-"+kind+"-enum-changed", operation, location+" enum value "+value+" was removed")
	}
	if !request && afterOK {
		for _, value := range sortedSetDifference(afterEnum, beforeEnum) {
			c.add("openapi-response-enum-changed", operation, location+" enum value "+value+" was added")
		}
	}
}

func (c *openAPIComparator) compareRequestBounds(operation, location string, before, after map[string]any) {
	minKeys := []string{"minimum", "exclusiveMinimum", "minLength", "minItems", "minProperties"}
	maxKeys := []string{"maximum", "exclusiveMaximum", "maxLength", "maxItems", "maxProperties"}
	for _, key := range minKeys {
		old, oldOK := number(before[key])
		value, valueOK := number(after[key])
		if valueOK && (!oldOK || value > old) {
			c.add("openapi-request-bound-tightened", operation, fmt.Sprintf("%s %s tightened to %v", location, key, value))
		}
	}
	for _, key := range maxKeys {
		old, oldOK := number(before[key])
		value, valueOK := number(after[key])
		if valueOK && (!oldOK || value < old) {
			c.add("openapi-request-bound-tightened", operation, fmt.Sprintf("%s %s tightened to %v", location, key, value))
		}
	}
}

// resolve follows a local JSON Pointer and overlays any OpenAPI 3.1 $ref
// siblings. The returned identity prevents recursive component schemas from
// recursing forever during comparison.
func (c *openAPIComparator) resolve(document OpenAPIDocument, value any) (map[string]any, string) {
	current, ok := object(value)
	if !ok {
		return nil, ""
	}
	var refs []string
	visited := make(map[string]bool)
	for {
		ref, _ := current["$ref"].(string)
		if ref == "" || !strings.HasPrefix(ref, "#/") || visited[ref] {
			return current, strings.Join(refs, "->")
		}
		visited[ref] = true
		refs = append(refs, ref)
		target, ok := localPointer(document, ref)
		if !ok {
			return current, strings.Join(refs, "->")
		}
		resolved, ok := object(target)
		if !ok {
			return current, strings.Join(refs, "->")
		}
		combined := make(map[string]any, len(resolved)+len(current))
		for key, item := range resolved {
			combined[key] = item
		}
		for key, item := range current {
			if key != "$ref" {
				combined[key] = item
			}
		}
		current = combined
	}
}

func localPointer(document OpenAPIDocument, ref string) (any, bool) {
	var current any = map[string]any(document)
	for _, encoded := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		part := strings.ReplaceAll(strings.ReplaceAll(encoded, "~1", "/"), "~0", "~")
		object, ok := object(current)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func schemaTypes(schema map[string]any) ([]string, bool) {
	types := make(map[string]bool)
	switch value := schema["type"].(type) {
	case string:
		types[value] = true
	case []any:
		for _, item := range value {
			if name, ok := item.(string); ok {
				types[name] = true
			}
		}
	}
	if nullable, _ := schema["nullable"].(bool); nullable {
		types["null"] = true
	}
	for _, keyword := range []string{"anyOf", "oneOf"} {
		if variants, ok := schema[keyword].([]any); ok {
			for _, variant := range variants {
				if item, ok := object(variant); ok {
					if value, _ := item["type"].(string); value == "null" {
						types["null"] = true
					}
				}
			}
		}
	}
	nullable := types["null"]
	delete(types, "null")
	return sortedKeys(types), nullable
}

func object(value any) (map[string]any, bool) {
	result, ok := value.(map[string]any)
	return result, ok
}

func number(value any) (float64, bool) {
	switch result := value.(type) {
	case float64:
		return result, true
	case int:
		return float64(result), true
	case int64:
		return float64(result), true
	default:
		return 0, false
	}
}

func stringSet(value any) map[string]bool {
	set := make(map[string]bool)
	switch items := value.(type) {
	case []any:
		for _, item := range items {
			if name, ok := item.(string); ok {
				set[name] = true
			}
		}
	case []string:
		for _, name := range items {
			set[name] = true
		}
	}
	return set
}

func enumSet(value any) (map[string]bool, bool) {
	var items []any
	switch value := value.(type) {
	case []any:
		items = value
	case []string:
		items = make([]any, len(value))
		for i := range value {
			items[i] = value[i]
		}
	default:
		return nil, false
	}
	set := make(map[string]bool, len(items))
	for _, item := range items {
		encoded, _ := json.Marshal(item)
		set[string(encoded)] = true
	}
	return set, true
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedSetDifference(left, right map[string]bool) []string {
	var difference []string
	for value := range left {
		if !right[value] {
			difference = append(difference, value)
		}
	}
	sort.Strings(difference)
	return difference
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
