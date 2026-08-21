package ogen_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/ogen-go/ogen"
	"github.com/ogen-go/ogen/gen"
	"github.com/ogen-go/ogen/location"
)

// captureFS collects generated sources instead of writing them out.
type captureFS map[string]string

func (c captureFS) WriteFile(baseName string, source []byte) error {
	c[baseName] = string(source)
	return nil
}

const openapi31Spec = `
openapi: 3.1.0
info: {title: openapi 3.1 keywords, version: 1.0.0}
paths:
  /upload:
    post:
      operationId: upload
      requestBody:
        required: true
        content:
          multipart/form-data:
            schema:
              type: object
              properties:
                file:
                  description: the uploaded file
              required: [file]
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema: {$ref: '#/components/schemas/Result'}
components:
  schemas:
    Result:
      type: object
      properties:
        metadata:
          type: object
          unevaluatedProperties: {}
      required: [metadata]
`

// OpenAPI 3.1 has no format: binary and no additionalProperties-for-open-objects.
// Both spellings must still produce the same Go types the 3.0 equivalents do —
// generation succeeds either way, so only the emitted source proves it.
func TestOpenAPI31Keywords(t *testing.T) {
	data := []byte(openapi31Spec)

	spec, err := ogen.Parse(data)
	require.NoError(t, err)

	g, err := gen.NewGenerator(spec, gen.Options{
		Parser: gen.ParseOptions{
			InferSchemaType: true,
			File:            location.NewFile("spec.yaml", "spec.yaml", data),
		},
		Logger: zaptest.NewLogger(t),
	})
	require.NoError(t, err)

	fs := captureFS{}
	require.NoError(t, g.WriteSource(fs, "api"))

	schemas, ok := fs["oas_schemas_gen.go"]
	require.True(t, ok, "schemas file was not generated")

	// A binary multipart part, spelled in 3.1 as a part with no schema type.
	require.Contains(t, schemas, "ht.MultipartFile",
		"an untyped multipart part should be a file, not raw JSON")
	require.NotContains(t, schemas, "File jx.Raw")

	// An open object, spelled in 3.1 with unevaluatedProperties.
	require.True(t,
		strings.Contains(schemas, "map[string]jx.Raw") || strings.Contains(schemas, "map[string]"),
		"unevaluatedProperties should produce a map, not an empty struct")
	require.NotContains(t, schemas, "ResultMetadata struct{}")
}
