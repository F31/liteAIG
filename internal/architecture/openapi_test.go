package architecture

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCompareOpenAPIDetectsBreakingChanges(t *testing.T) {
	tests := []struct {
		name   string
		change func(OpenAPIDocument)
		rule   string
	}{
		{"removed path", func(d OpenAPIDocument) { delete(paths(d), "/things") }, "openapi-path-removed"},
		{"removed method", func(d OpenAPIDocument) { delete(operationPath(d), "post") }, "openapi-method-removed"},
		{"request body required", func(d OpenAPIDocument) { requestBody(d)["required"] = true }, "openapi-request-body-required"},
		{"request media type removed", func(d OpenAPIDocument) { delete(content(requestBody(d)), "application/json") }, "openapi-request-media-type-removed"},
		{"response media type removed", func(d OpenAPIDocument) { delete(content(response(d)), "application/json") }, "openapi-response-media-type-removed"},
		{"request property removed", func(d OpenAPIDocument) { delete(properties(requestSchema(d)), "mode") }, "openapi-request-property-removed"},
		{"request property required", func(d OpenAPIDocument) { requestSchema(d)["required"] = []any{"name", "mode"} }, "openapi-request-property-required"},
		{"request type changed", func(d OpenAPIDocument) { properties(requestSchema(d))["name"].(map[string]any)["type"] = "integer" }, "openapi-request-type-changed"},
		{"request enum removed", func(d OpenAPIDocument) { properties(requestSchema(d))["mode"].(map[string]any)["enum"] = []any{"fast"} }, "openapi-request-enum-changed"},
		{"request null narrowed", func(d OpenAPIDocument) { properties(requestSchema(d))["note"].(map[string]any)["type"] = "string" }, "openapi-request-nullability-narrowed"},
		{"request bound tightened", func(d OpenAPIDocument) {
			properties(requestSchema(d))["name"].(map[string]any)["minLength"] = float64(2)
		}, "openapi-request-bound-tightened"},
		{"response property removed", func(d OpenAPIDocument) { delete(properties(responseSchema(d)), "state") }, "openapi-response-property-removed"},
		{"response property optional", func(d OpenAPIDocument) { responseSchema(d)["required"] = []any{"id"} }, "openapi-response-property-optional"},
		{"response type changed", func(d OpenAPIDocument) { properties(responseSchema(d))["id"].(map[string]any)["type"] = "integer" }, "openapi-response-type-changed"},
		{"response enum removed", func(d OpenAPIDocument) {
			properties(responseSchema(d))["state"].(map[string]any)["enum"] = []any{"ready"}
		}, "openapi-response-enum-changed"},
		{"response enum added", func(d OpenAPIDocument) {
			properties(responseSchema(d))["state"].(map[string]any)["enum"] = []any{"ready", "done", "failed"}
		}, "openapi-response-enum-changed"},
		{"response null widened", func(d OpenAPIDocument) {
			properties(responseSchema(d))["id"].(map[string]any)["type"] = []any{"string", "null"}
		}, "openapi-response-nullability-widened"},
		{"request schema removed", func(d OpenAPIDocument) {
			delete(content(requestBody(d))["application/json"].(map[string]any), "schema")
		}, "openapi-request-schema-removed"},
		{"array items removed", func(d OpenAPIDocument) { delete(properties(requestSchema(d))["tags"].(map[string]any), "items") }, "openapi-request-array-items-removed"},
		{"parameter removed", func(d OpenAPIDocument) { operation(d)["parameters"] = []any{} }, "openapi-parameter-removed"},
		{"parameter required", func(d OpenAPIDocument) { operation(d)["parameters"].([]any)[0].(map[string]any)["required"] = true }, "openapi-parameter-required"},
		{"security required", func(d OpenAPIDocument) { operation(d)["security"] = []any{map[string]any{"bearerAuth": []any{}}} }, "openapi-security-required"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := testOpenAPI(t)
			after := cloneOpenAPI(t, before)
			test.change(after)
			violations := CompareOpenAPI(before, after)
			if !hasRule(violations, test.rule) {
				t.Fatalf("CompareOpenAPI() = %+v, want rule %s", violations, test.rule)
			}
		})
	}
}

func TestCompareOpenAPIResolvesLocalRefs(t *testing.T) {
	before := testOpenAPI(t)
	after := cloneOpenAPI(t, before)
	properties(responseSchema(after))["id"].(map[string]any)["type"] = "number"

	violations := CompareOpenAPI(before, after)
	if !hasRule(violations, "openapi-response-type-changed") {
		t.Fatalf("CompareOpenAPI() = %+v", violations)
	}
}

func TestCompareOpenAPIAllowsAdditionsAndExtensibleResponseEnum(t *testing.T) {
	before := testOpenAPI(t)
	state := properties(responseSchema(before))["state"].(map[string]any)
	state["x-liteaig-extensible-enum"] = true
	after := cloneOpenAPI(t, before)

	paths(after)["/new"] = map[string]any{"get": map[string]any{"responses": map[string]any{"204": map[string]any{"description": "ok"}}}}
	operationPath(after)["get"] = map[string]any{"responses": map[string]any{"204": map[string]any{"description": "ok"}}}
	properties(requestSchema(after))["optional"] = map[string]any{"type": "string"}
	properties(responseSchema(after))["extra"] = map[string]any{"type": "string"}
	properties(responseSchema(after))["state"].(map[string]any)["enum"] = []any{"ready", "done", "new"}

	if violations := CompareOpenAPI(before, after); len(violations) != 0 {
		t.Fatalf("CompareOpenAPI() = %+v", violations)
	}
}

func TestCompareOpenAPIOrderIsDeterministic(t *testing.T) {
	before := testOpenAPI(t)
	paths(before)["/alpha"] = cloneValue(t, operationPath(before))
	after := cloneOpenAPI(t, before)
	delete(paths(after), "/things")
	delete(operationPathFor(after, "/alpha"), "post")

	first := CompareOpenAPI(before, after)
	second := CompareOpenAPI(before, after)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("results differ: %+v and %+v", first, second)
	}
	for i := 1; i < len(first); i++ {
		if first[i-1].String() > first[i].String() {
			t.Fatalf("violations are not sorted: %+v", first)
		}
	}
}

func testOpenAPI(t *testing.T) OpenAPIDocument {
	t.Helper()
	const source = `{
  "openapi": "3.1.0",
  "info": {"title": "test", "version": "1"},
  "paths": {
    "/things": {
      "post": {
        "parameters": [{"name":"mode","in":"query","required":false,"schema":{"type":"string"}}],
        "requestBody": {"required": false, "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Request"}}}},
        "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Response"}}}}}
      }
    }
  },
  "components": {"schemas": {
    "Request": {"type": "object", "required": ["name"], "properties": {
      "name": {"type": "string", "minLength": 1},
      "mode": {"type": "string", "enum": ["fast", "safe"]},
      "note": {"type": ["string", "null"]},
      "tags": {"type":"array","items":{"type":"string"}}
    }},
    "Response": {"type": "object", "required": ["id", "state"], "properties": {
      "id": {"type": "string"},
      "state": {"type": "string", "enum": ["ready", "done"]}
    }}
  }}
}`
	var document OpenAPIDocument
	if err := json.Unmarshal([]byte(source), &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func cloneOpenAPI(t *testing.T, document OpenAPIDocument) OpenAPIDocument {
	t.Helper()
	return OpenAPIDocument(cloneValue(t, document).(map[string]any))
}

func cloneValue(t *testing.T, value any) any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func paths(document OpenAPIDocument) map[string]any {
	return document["paths"].(map[string]any)
}

func operationPath(document OpenAPIDocument) map[string]any {
	return operationPathFor(document, "/things")
}

func operationPathFor(document OpenAPIDocument, path string) map[string]any {
	return paths(document)[path].(map[string]any)
}

func operation(document OpenAPIDocument) map[string]any {
	return operationPath(document)["post"].(map[string]any)
}

func requestBody(document OpenAPIDocument) map[string]any {
	return operation(document)["requestBody"].(map[string]any)
}

func response(document OpenAPIDocument) map[string]any {
	return operation(document)["responses"].(map[string]any)["200"].(map[string]any)
}

func content(value map[string]any) map[string]any {
	return value["content"].(map[string]any)
}

func requestSchema(document OpenAPIDocument) map[string]any {
	return schemas(document)["Request"].(map[string]any)
}

func responseSchema(document OpenAPIDocument) map[string]any {
	return schemas(document)["Response"].(map[string]any)
}

func schemas(document OpenAPIDocument) map[string]any {
	return document["components"].(map[string]any)["schemas"].(map[string]any)
}

func properties(schema map[string]any) map[string]any {
	return schema["properties"].(map[string]any)
}

func hasRule(violations []Violation, rule string) bool {
	for _, violation := range violations {
		if violation.Rule == rule {
			return true
		}
	}
	return false
}
