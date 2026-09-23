package argomodelgen

import (
	"strings"
	"testing"
)

const testSchema = `{"definitions":{"Root":{"type":"object","properties":{"name":{"type":"string"},"child":{"$ref":"#/definitions/Child"},"items":{"type":"array","items":{"$ref":"#/definitions/Child"}},"labels":{"type":"object","additionalProperties":{"type":"string"}}}},"Child":{"type":"object","properties":{"enabled":{"type":"boolean"}}}}}`

const testProjection = `{"package":"models","sourceVersion":"test","definitions":[{"source":"Root","name":"Root","fields":[{"source":"name","name":"Name","goType":"string","json":"name","schema":"string"},{"source":"child","name":"Child","goType":"Child","json":"child","schema":"ref:Child"},{"source":"items","name":"Items","goType":"[]Child","json":"items","schema":"array:ref:Child"},{"source":"labels","name":"Labels","goType":"map[string]string","json":"labels","schema":"map:string"},{"name":"Extra","goType":"string","json":"extra","extension":"compatibility"}]},{"source":"Child","name":"Child","fields":[{"source":"enabled","name":"Enabled","goType":"bool","json":"enabled","schema":"boolean"}]}]}`

func TestGenerateIsDeterministic(t *testing.T) {
	first, err := Generate([]byte(testSchema), []byte(testProjection))
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	second, err := Generate([]byte(testSchema), []byte(testProjection))
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("generation is not deterministic")
	}
	for _, want := range []string{"type Root struct", "`json:\"name,omitempty\"`", "map[string]string", "`json:\"extra,omitempty\"`", "type Child struct"} {
		if !strings.Contains(string(first), want) {
			t.Fatalf("generated source missing %q:\n%s", want, first)
		}
	}
}

func TestGenerateRejectsMissingDefinition(t *testing.T) {
	projection := strings.Replace(testProjection, `"source":"Root"`, `"source":"Missing"`, 1)
	assertGenerateError(t, testSchema, projection, "definition Missing")
}

func TestGenerateRejectsMissingProperty(t *testing.T) {
	projection := strings.Replace(testProjection, `"source":"name"`, `"source":"missing"`, 1)
	assertGenerateError(t, testSchema, projection, "property missing")
}

func TestGenerateRejectsChangedScalarType(t *testing.T) {
	schema := strings.Replace(testSchema, `"name":{"type":"string"}`, `"name":{"type":"integer"}`, 1)
	assertGenerateError(t, schema, testProjection, "expected string")
}

func TestGenerateRejectsChangedNestedReference(t *testing.T) {
	schema := strings.Replace(testSchema, `"child":{"$ref":"#/definitions/Child"}`, `"child":{"$ref":"#/definitions/Other"}`, 1)
	assertGenerateError(t, schema, testProjection, "expected ref:Child")
}

func TestGenerateRejectsChangedArrayElementType(t *testing.T) {
	schema := strings.Replace(testSchema, `"items":{"type":"array","items":{"$ref":"#/definitions/Child"}}`, `"items":{"type":"array","items":{"type":"string"}}`, 1)
	assertGenerateError(t, schema, testProjection, "expected array:ref:Child")
}

func TestGenerateRejectsChangedMapValueType(t *testing.T) {
	schema := strings.Replace(testSchema, `"additionalProperties":{"type":"string"}`, `"additionalProperties":{"type":"boolean"}`, 1)
	assertGenerateError(t, schema, testProjection, "expected map:string")
}

func assertGenerateError(t *testing.T, schema, projection, contains string) {
	t.Helper()
	_, err := Generate([]byte(schema), []byte(projection))
	if err == nil || !strings.Contains(err.Error(), contains) {
		t.Fatalf("expected error containing %q, got %v", contains, err)
	}
}
