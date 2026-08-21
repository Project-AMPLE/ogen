package gen

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ogen-go/ogen/jsonschema"
)

// A multipart file part is spelled differently in the two OpenAPI dialects:
// 3.0 marks it with format: binary, which JSON Schema 2020-12 removed, and 3.1
// expresses it as an empty schema. Both must be recognized as binary.
func TestIsBinary(t *testing.T) {
	for _, tt := range []struct {
		name   string
		schema *jsonschema.Schema
		want   bool
	}{
		{
			name:   "3.0 string with format binary",
			schema: &jsonschema.Schema{Type: jsonschema.String, Format: "binary"},
			want:   true,
		},
		{
			name:   "3.1 empty schema",
			schema: &jsonschema.Schema{},
			want:   true,
		},
		{
			name:   "plain string is not a file",
			schema: &jsonschema.Schema{Type: jsonschema.String},
			want:   false,
		},
		{
			name:   "object is not a file",
			schema: &jsonschema.Schema{Type: jsonschema.Object},
			want:   false,
		},
		{
			name:   "nil schema",
			schema: nil,
			want:   false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isBinary(tt.schema))
		})
	}
}
