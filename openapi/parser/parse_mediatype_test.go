package parser_test

import (
	"testing"

	"github.com/go-faster/yaml"
	"github.com/stretchr/testify/require"

	"github.com/ogen-go/ogen"
	"github.com/ogen-go/ogen/jsonschema"
	"github.com/ogen-go/ogen/openapi"
	"github.com/ogen-go/ogen/openapi/parser"
)

func extensionString(v string) yaml.Node {
	return yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

func extensionBool(v bool) yaml.Node {
	if v {
		return yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"}
	}
	return yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"}
}

func sseSpec(media ogen.Media) *ogen.Spec {
	return &ogen.Spec{
		OpenAPI: "3.0.0",
		Info: ogen.Info{
			Title:   "test",
			Version: "0.0.0",
		},
		Paths: map[string]*ogen.PathItem{
			"/stream": {
				Get: &ogen.Operation{
					OperationID: "stream",
					Responses: map[string]*ogen.Response{
						"200": {
							Content: map[string]ogen.Media{
								"text/event-stream": media,
							},
						},
					},
				},
			},
		},
	}
}

func TestParseMediaTypeSSEShapeDefault(t *testing.T) {
	api, err := parser.Parse(sseSpec(ogen.Media{
		Schema: &ogen.Schema{Type: "object"},
	}), parser.Settings{})
	require.NoError(t, err)

	media := api.Operations[0].Responses.StatusCode[200].Content["text/event-stream"]
	require.Equal(t, openapi.SSEEventShapeDataOnly, media.XOgenSSEEventShape)
}

func TestParseMediaTypeSSEShapeBinaryStreamDefault(t *testing.T) {
	for name, schema := range map[string]*ogen.Schema{
		"no schema":     nil,
		"string":        {Type: "string"},
		"binary string": {Type: "string", Format: "binary"},
	} {
		t.Run(name, func(t *testing.T) {
			api, err := parser.Parse(sseSpec(ogen.Media{
				Schema: schema,
			}), parser.Settings{})
			require.NoError(t, err)

			media := api.Operations[0].Responses.StatusCode[200].Content["text/event-stream"]
			require.Equal(t, openapi.SSEEventShapeNone, media.XOgenSSEEventShape)
		})
	}
}

func TestParseMediaTypeSSEShapeSumDefault(t *testing.T) {
	// Schemas without an explicit type, e.g. oneOf sums, must stay in SSE mode.
	api, err := parser.Parse(sseSpec(ogen.Media{
		Schema: &ogen.Schema{
			OneOf: []*ogen.Schema{
				{Type: "object"},
				{Type: "string"},
			},
		},
	}), parser.Settings{})
	require.NoError(t, err)

	media := api.Operations[0].Responses.StatusCode[200].Content["text/event-stream"]
	require.Equal(t, openapi.SSEEventShapeDataOnly, media.XOgenSSEEventShape)
}

func TestParseMediaTypeSSEShapeBinaryStreamExplicit(t *testing.T) {
	api, err := parser.Parse(sseSpec(ogen.Media{
		Schema: &ogen.Schema{Type: "string"},
		Common: ogen.OpenAPICommon{
			Extensions: ogen.Extensions{
				"x-ogen-sse-event-shape": extensionString("data-only"),
			},
		},
	}), parser.Settings{})
	require.NoError(t, err)

	media := api.Operations[0].Responses.StatusCode[200].Content["text/event-stream"]
	require.Equal(t, openapi.SSEEventShapeDataOnly, media.XOgenSSEEventShape)
}

func TestParseMediaTypeSSEShapeFull(t *testing.T) {
	api, err := parser.Parse(sseSpec(ogen.Media{
		Schema: &ogen.Schema{Type: "object"},
		Common: ogen.OpenAPICommon{
			Extensions: ogen.Extensions{
				"x-ogen-sse-event-shape": extensionString("full"),
			},
		},
	}), parser.Settings{})
	require.NoError(t, err)

	media := api.Operations[0].Responses.StatusCode[200].Content["text/event-stream"]
	require.Equal(t, openapi.SSEEventShapeFull, media.XOgenSSEEventShape)
}

func TestParseMediaTypeSSEShapeFullArray(t *testing.T) {
	api, err := parser.Parse(sseSpec(ogen.Media{
		Schema: &ogen.Schema{
			Type: "array",
			Items: &ogen.Items{
				Item: &ogen.Schema{
					Type: "object",
				},
			},
		},
		Common: ogen.OpenAPICommon{
			Extensions: ogen.Extensions{
				"x-ogen-sse-event-shape": extensionString("full-array"),
			},
		},
	}), parser.Settings{})
	require.NoError(t, err)

	media := api.Operations[0].Responses.StatusCode[200].Content["text/event-stream"]
	require.Equal(t, openapi.SSEEventShapeFullArray, media.XOgenSSEEventShape)
}

func TestParseMediaTypeSSEShapeRawResponsePriority(t *testing.T) {
	api, err := parser.Parse(sseSpec(ogen.Media{
		Schema: &ogen.Schema{Type: "object"},
		Common: ogen.OpenAPICommon{
			Extensions: ogen.Extensions{
				"x-ogen-sse-event-shape": extensionString("full"),
				"x-ogen-raw-response":    extensionBool(true),
			},
		},
	}), parser.Settings{})
	require.NoError(t, err)

	media := api.Operations[0].Responses.StatusCode[200].Content["text/event-stream"]
	require.Equal(t, openapi.SSEEventShapeNone, media.XOgenSSEEventShape)
	require.True(t, media.XOgenRawResponse)
}

func TestParseMediaTypeSSEShapeNonEventStreamError(t *testing.T) {
	spec := sseSpec(ogen.Media{})
	spec.Paths["/stream"].Get.Responses["200"].Content["application/json"] = ogen.Media{
		Schema: &ogen.Schema{Type: "object"},
		Common: ogen.OpenAPICommon{
			Extensions: ogen.Extensions{
				"x-ogen-sse-event-shape": extensionString("data-only"),
			},
		},
	}
	delete(spec.Paths["/stream"].Get.Responses["200"].Content, "text/event-stream")

	_, err := parser.Parse(spec, parser.Settings{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "x-ogen-sse-event-shape")
	require.Contains(t, err.Error(), "is only allowed for text/event-stream media type")
}

// OpenAPI 3.2 moves the SSE payload schema out of `schema` and into `itemSchema`,
// and reaches the real event type through `data.contentSchema` — a $ref describing
// the PARSED content of the `data` string rather than `data` itself. The document
// built below is the shape @typespec/openapi3 1.15.0 emits for an SSEStream<T>.
func sse32Spec() *ogen.Spec {
	payload := func(field string) *ogen.Schema {
		return &ogen.Schema{
			Type:       "object",
			Properties: ogen.Properties{{Name: field, Schema: &ogen.Schema{Type: "string"}}},
		}
	}
	branch := func(event, ref string) *ogen.Schema {
		return &ogen.Schema{
			Properties: ogen.Properties{
				{Name: "event", Schema: &ogen.Schema{Const: ogen.Const(`"` + event + `"`)}},
				{Name: "data", Schema: &ogen.Schema{
					ContentMediaType: "application/json",
					ContentSchema:    &ogen.Schema{Ref: ref},
				}},
			},
		}
	}

	spec := sseSpec(ogen.Media{
		ItemSchema: &ogen.Schema{
			Type: "object",
			Properties: ogen.Properties{
				{Name: "event", Schema: &ogen.Schema{Type: "string"}},
				{Name: "data", Schema: &ogen.Schema{Type: "string"}},
			},
			Required: []string{"event"},
			OneOf: []*ogen.Schema{
				branch("chunk", "#/components/schemas/StreamChunk"),
				branch("done", "#/components/schemas/StreamDone"),
			},
		},
	})
	spec.Components = &ogen.Components{
		Schemas: map[string]*ogen.Schema{
			"StreamChunk": payload("content"),
			"StreamDone":  payload("result"),
		},
	}
	return spec
}

// Today itemSchema is invisible to the parser: MediaType.Schema comes out nil,
// isBinaryStreamSchema(nil) reports true, SSE never auto-enables, and the response
// degrades to io.Reader — which is why 3.1 and 3.2 generate byte-identical Go.
func TestParseMediaTypeSSEItemSchemaEnablesFullShape(t *testing.T) {
	api, err := parser.Parse(sse32Spec(), parser.Settings{})
	require.NoError(t, err)

	media := api.Operations[0].Responses.StatusCode[200].Content["text/event-stream"]
	require.Equal(t, openapi.SSEEventShapeFull, media.XOgenSSEEventShape,
		"itemSchema describes the full event envelope, not just the data payload")
	require.NotNil(t, media.Schema, "itemSchema must populate MediaType.Schema")
	require.Len(t, media.Schema.OneOf, 2, "the discriminated branches must survive parsing")
}

// Losing the contentSchema indirection is the difference between a typed event
// and an opaque string, so assert the payload resolves through to its object.
func TestParseMediaTypeSSEItemSchemaResolvesContentSchema(t *testing.T) {
	api, err := parser.Parse(sse32Spec(), parser.Settings{})
	require.NoError(t, err)

	media := api.Operations[0].Responses.StatusCode[200].Content["text/event-stream"]
	require.NotNil(t, media.Schema)

	for i, branch := range media.Schema.OneOf {
		var data *jsonschema.Schema
		for _, prop := range branch.Properties {
			if prop.Name == "data" {
				data = prop.Schema
			}
		}
		require.NotNilf(t, data, "branch %d must carry a data property", i)
		require.Equalf(t, "application/json", data.ContentMediaType, "branch %d", i)
		require.NotNilf(t, data.ContentSchema, "branch %d data.contentSchema must resolve", i)
		require.Equalf(t, jsonschema.Object, data.ContentSchema.Type, "branch %d payload", i)
	}
}
