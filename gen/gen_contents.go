package gen

import (
	"fmt"
	"mime"
	"path"
	"strings"

	"github.com/go-faster/errors"
	"go.uber.org/zap"

	"github.com/ogen-go/ogen/gen/ir"
	"github.com/ogen-go/ogen/internal/xmaps"
	"github.com/ogen-go/ogen/jsonschema"
	"github.com/ogen-go/ogen/openapi"
)

// normalizeContentEncoding parses the media type, applies aliases and
// normalizes +json suffix media types as JSON per RFC 6838.
// This allows types like application/json-patch+json,
// application/vnd.api+json, application/merge-patch+json, etc.
// to work without explicit ContentTypeAliases.
// Note: application/problem+json has special handling via EncodingProblemJSON.
func normalizeContentEncoding(contentType string, aliases ContentTypeAliases,
) (parsedContentType string, encoding ir.Encoding, err error) {
	parsedContentType, _, err = mime.ParseMediaType(contentType)
	if err != nil {
		return "", "", err
	}

	encoding = ir.Encoding(parsedContentType)
	if override, ok := aliases[parsedContentType]; ok {
		encoding = override
	}

	if strings.HasSuffix(string(encoding), "+json") &&
		encoding != ir.EncodingJSON &&
		encoding != ir.EncodingProblemJSON {
		encoding = ir.EncodingJSON
	}

	return parsedContentType, encoding, nil
}

func isJSONLikeEncoding(encoding ir.Encoding) bool {
	return encoding.JSON() || encoding.ProblemJSON()
}

func filterMostSpecific(contents map[string]*openapi.MediaType, log *zap.Logger) error {
	keep := func(current, mask string) (string, bool) {
		// Special case for "*", "**", etc.
		var notOnlyStar bool
		for _, c := range mask {
			if c != '*' {
				notOnlyStar = true
				break
			}
		}
		if !notOnlyStar {
			for k := range contents {
				if k == current {
					continue
				}
				// There is at least one another media type, so delete "*".
				return k, false
			}
			// There is no other media type, so keep "*".
			return "", true
		}

		for contentType := range contents {
			// Do not try to match mask against itself.
			if contentType == current {
				continue
			}
			// Found more specific media type that matches the mask, so delete the mask.
			if matched, _ := path.Match(mask, contentType); matched {
				return contentType, false
			}
		}
		// Found no more specific media type, so keep the mask.
		return "", true
	}

	for k := range contents {
		contentType, _, err := mime.ParseMediaType(k)
		if err != nil {
			return errors.Wrapf(err, "parse content type %q", k)
		}

		if replacement, keep := keep(k, contentType); !keep {
			log.Info("Filter common content type",
				zap.String("mask", k),
				zap.String("replacement", replacement),
			)
			delete(contents, k)
		}
	}
	return nil
}

func (g *Generator) wrapContent(ctx *genctx, name string, t *ir.Type) (ret *ir.Type, rerr error) {
	defer func() {
		if rerr != nil {
			return
		}

		if err := ctx.saveType(ret); err != nil {
			rerr = err
			ret = nil
		}
	}()

	if t.Name != "" {
		name = t.Name
	}
	wrapper := &ir.Type{
		Kind: ir.KindStruct,
		Name: name + "WithContentType",
		Doc:  fmt.Sprintf("%sWithContentType wraps %s with Content-Type.", name, t.Go()),
		Fields: []*ir.Field{
			{Name: "ContentType", Type: ir.Primitive(ir.String, nil)},
			{Name: "Content", Type: t},
		},
	}
	return wrapper, nil
}

func (g *Generator) generateFormContent(
	ctx *genctx,
	typeName string,
	media *openapi.MediaType,
	optional bool,
	encoding ir.Encoding,
) (*ir.Type, error) {
	if s := media.Schema; s != nil &&
		((s.AdditionalProperties != nil && s.Item != nil) ||
			len(s.PatternProperties) > 0) ||
		len(s.Items) > 0 {
		return nil, &ErrNotImplemented{"complex form schema"}
	}

	getEncoding := func(f *ir.Field) (ct ir.Encoding) {
		if e, ok := media.Encoding[f.Tag.JSON]; ok {
			ct = ir.Encoding(e.ContentType)
		}
		if ct == "" && encoding.MultipartForm() && isComplexMultipartType(f.Spec.Schema) {
			ct = ir.EncodingJSON
		}
		return ct
	}

	var override generateSchemaOverride
	switch encoding {
	case ir.EncodingFormURLEncoded:
		override.fieldMut = func(f *ir.Field) error {
			f.Type.AddFeature("uri")
			return nil
		}
	case ir.EncodingMultipart:
		// A funny moment when you have a spec that shares schema between multipart form and JSON request and
		// at some point you made ingenious decision to keep all types in one package at the same time.
		if s := media.Schema; s != nil && !s.Ref.IsZero() {
			override.refEncoding = map[jsonschema.Ref]ir.Encoding{
				s.Ref: encoding,
			}
			override.nameRef = func(ref jsonschema.Ref, def refNamer) (string, error) {
				n, err := def(ref)
				if err == nil && ref == s.Ref {
					n += "Multipart"
				}
				return n, err
			}
		}
		override.fieldMut = func(f *ir.Field) error {
			t, err := isMultipartFile(ctx, f.Type, f.Spec)
			if err != nil {
				return err
			}
			if t != nil {
				f.Type = t
				t.AddFeature("multipart-file")
				return nil
			}
			switch ct := getEncoding(f); ct {
			case "", ir.EncodingFormURLEncoded:
				f.Type.AddFeature("uri")
			case ir.EncodingJSON, ir.EncodingProblemJSON:
				f.Type.AddFeature("json")
			default:
				return errors.Wrapf(
					&ErrNotImplemented{"form content encoding"},
					"%q", ct,
				)
			}
			return nil
		}
	}
	t, err := g.generateSchema(ctx, typeName, media.Schema, optional, &override)
	if err != nil {
		return nil, errors.Wrap(err, "generate schema")
	}

	var (
		complexTypeErr = func(bt *ir.Type) error {
			impl := &ErrNotImplemented{"complex form schema"}
			if bt != t {
				return errors.Wrapf(impl, "%s -> %s", t, bt)
			}
			return errors.Wrapf(impl, "%s", bt)
		}
		structType = t
	)
	switch t.Kind {
	case ir.KindStruct:
	case ir.KindGeneric:
		generic := t.GenericOf
		if v := t.GenericVariant; optional && v.OnlyOptional() && generic.IsStruct() {
			structType = generic
			break
		}
		return nil, complexTypeErr(generic)
	default:
		return nil, complexTypeErr(t)
	}

	for _, f := range structType.Fields {
		tag := f.Tag.JSON

		spec := &openapi.Parameter{
			Name:     tag,
			Schema:   f.Spec.Schema,
			In:       openapi.LocationQuery,
			Style:    openapi.QueryStyleForm,
			Explode:  true,
			Required: f.Spec.Required,
		}

		if err := func() error {
			if e, ok := media.Encoding[tag]; ok {
				spec.Style = e.Style
				spec.Explode = e.Explode
			}
			switch ct := getEncoding(f); ct {
			case "", ir.EncodingFormURLEncoded:
				if err := isSupportedParamStyle(spec); err != nil {
					return err
				}

				if err := isParamAllowed(f.Type, true, map[*ir.Type]struct{}{}); err != nil {
					return err
				}
			case ir.EncodingJSON, ir.EncodingProblemJSON:
				spec.Content = &openapi.ParameterContent{
					Name: ct.String(),
				}
			default:
				return errors.Wrapf(
					&ErrNotImplemented{"form content encoding"},
					"%q", ct,
				)
			}

			return nil
		}(); err != nil {
			return nil, errors.Wrapf(err, "form parameter %q", tag)
		}

		f.Tag.Form = spec
	}
	return t, nil
}

func sseStandardSchema(name string) *jsonschema.Schema {
	switch name {
	case "id", "event":
		return &jsonschema.Schema{Type: jsonschema.String}
	case "data":
		return &jsonschema.Schema{Type: jsonschema.Empty}
	case "retry":
		return &jsonschema.Schema{Type: jsonschema.Integer}
	default:
		panic("unexpected SSE standard field: " + name)
	}
}

func (g *Generator) normalizeFullSSESchema(schema *jsonschema.Schema, media *openapi.MediaType) (*jsonschema.Schema, error) {
	if schema == nil {
		return nil, nil
	}

	switch {
	case schema.Type == jsonschema.Object:
		allowed := map[string]struct{}{
			"id":    {},
			"event": {},
			"data":  {},
			"retry": {},
		}
		seen := map[string]struct{}{}

		clone := *schema
		clone.Properties = nil
		clone.Required = nil
		additionalProperties := false
		clone.AdditionalProperties = &additionalProperties
		clone.Item = nil
		clone.PatternProperties = nil
		for _, prop := range schema.Properties {
			if _, ok := allowed[prop.Name]; !ok {
				g.log.Warn("Ignoring non-standard SSE event schema property",
					zapPosition(media),
					zap.String("property", prop.Name),
				)
				continue
			}
			prop.Required = prop.Name != "retry"
			seen[prop.Name] = struct{}{}
			clone.Properties = append(clone.Properties, prop)
			if prop.Required {
				clone.Required = append(clone.Required, prop.Name)
			}
		}

		for _, name := range []string{"id", "event", "data", "retry"} {
			if _, ok := seen[name]; ok {
				continue
			}
			required := name != "retry"
			clone.Properties = append(clone.Properties, jsonschema.Property{
				Name:     name,
				Schema:   sseStandardSchema(name),
				Required: required,
			})
			if required {
				clone.Required = append(clone.Required, name)
			}
		}

		// A schema can be BOTH `type: object` and a `oneOf` of variants — that
		// is exactly what OpenAPI 3.2 SSE emits: an envelope declaring the
		// common id/event/data/retry fields, refined by one branch per event
		// type. This arm matches first, so without recursing here the variants
		// would keep their un-normalized shape and no discriminator.
		if err := g.normalizeSSEComposition(&clone, media); err != nil {
			return nil, err
		}

		return &clone, nil
	case len(schema.OneOf) > 0:
		clone := *schema
		clone.OneOf = make([]*jsonschema.Schema, len(schema.OneOf))
		for i, variant := range schema.OneOf {
			if variant == nil {
				return nil, errors.Errorf("oneOf[%d] is nil", i)
			}
			normalized, err := g.normalizeFullSSESchema(variant, media)
			if err != nil {
				return nil, errors.Wrapf(err, "oneOf[%d]", i)
			}
			clone.OneOf[i] = normalized
		}
		inferSSEEventDiscriminator(&clone, clone.OneOf)
		return &clone, nil
	case len(schema.AnyOf) > 0:
		clone := *schema
		clone.AnyOf = make([]*jsonschema.Schema, len(schema.AnyOf))
		for i, variant := range schema.AnyOf {
			if variant == nil {
				return nil, errors.Errorf("anyOf[%d] is nil", i)
			}
			normalized, err := g.normalizeFullSSESchema(variant, media)
			if err != nil {
				return nil, errors.Wrapf(err, "anyOf[%d]", i)
			}
			clone.AnyOf[i] = normalized
		}
		inferSSEEventDiscriminator(&clone, clone.AnyOf)
		return &clone, nil
	default:
		return nil, errors.New("must be an object or oneOf/anyOf of objects")
	}
}

// normalizeSSEComposition normalizes any oneOf/anyOf variants hanging off an SSE
// envelope schema in place, then infers the event discriminator across them.
func (g *Generator) normalizeSSEComposition(clone *jsonschema.Schema, media *openapi.MediaType) error {
	for _, group := range []struct {
		name     string
		variants []*jsonschema.Schema
	}{
		{"oneOf", clone.OneOf},
		{"anyOf", clone.AnyOf},
	} {
		if len(group.variants) == 0 {
			continue
		}
		normalized := make([]*jsonschema.Schema, len(group.variants))
		for i, variant := range group.variants {
			if variant == nil {
				return errors.Errorf("%s[%d] is nil", group.name, i)
			}
			n, err := g.normalizeFullSSESchema(variant, media)
			if err != nil {
				return errors.Wrapf(err, "%s[%d]", group.name, i)
			}
			normalized[i] = n
		}
		if group.name == "oneOf" {
			clone.OneOf = normalized
		} else {
			clone.AnyOf = normalized
		}
		inferSSEEventDiscriminator(clone, normalized)
	}
	return nil
}

// inferSSEEventDiscriminator points a sum of SSE events at its `event` field when
// every variant pins that field to a distinct string const.
//
// SSE event unions are discriminated by construction — that is what the `event:`
// line on the wire IS — but the emitted document rarely says so with a
// `discriminator` keyword, and every variant carries the same field NAMES
// (id/event/data/retry). ogen's default inference separates variants by which
// fields they uniquely have, so it sees three identical shapes and gives up with
// "can't infer fields discriminator". The information it needs is present, just
// in the const values rather than the field names.
//
// Scoped deliberately to the SSE path: general oneOf handling is shared by every
// consumer, while this shape is fixed by the SSE spec.
func inferSSEEventDiscriminator(sum *jsonschema.Schema, variants []*jsonschema.Schema) {
	const propName = "event"

	if sum.Discriminator != nil || len(variants) < 2 {
		return
	}

	seen := make(map[string]struct{}, len(variants))
	for _, variant := range variants {
		if variant == nil {
			return
		}
		value, ok := constStringProperty(variant, propName)
		if !ok {
			return
		}
		// A repeated value cannot discriminate — bail rather than emit a
		// mapping that silently resolves two variants to one.
		if _, dup := seen[value]; dup {
			return
		}
		seen[value] = struct{}{}
	}

	sum.Discriminator = &jsonschema.Discriminator{PropertyName: propName}
}

// constStringProperty returns the string const pinned on s's named property.
func constStringProperty(s *jsonschema.Schema, propName string) (string, bool) {
	for _, prop := range s.Properties {
		if prop.Name != propName {
			continue
		}
		if prop.Schema == nil || !prop.Schema.ConstSet {
			return "", false
		}
		v, ok := prop.Schema.Const.(string)
		return v, ok
	}
	return "", false
}

func (g *Generator) generateSSEContent(
	ctx *genctx,
	typeName string,
	media *openapi.MediaType,
) (*ir.Type, error) {
	shape := media.XOgenSSEEventShape
	if !shape.Enabled() {
		panic("generateSSEContent called for non-SSE media")
	}

	schema := media.Schema
	var err error
	switch shape {
	case openapi.SSEEventShapeFull:
		schema, err = g.normalizeFullSSESchema(schema, media)
		if err != nil {
			return nil, fmt.Errorf("SSE schema of %q shape %w", openapi.SSEEventShapeFull, err)
		}
	case openapi.SSEEventShapeFullArray:
		if schema == nil || schema.Type != jsonschema.Array || schema.Item == nil {
			return nil, fmt.Errorf("SSE schema of %q shape must be an array with items",
				openapi.SSEEventShapeFullArray)
		}
		schema, err = g.normalizeFullSSESchema(schema.Item, media)
		if err != nil {
			return nil, fmt.Errorf("SSE schema of %q shape %w", openapi.SSEEventShapeFullArray, err)
		}
	}

	eventSchemaName := typeName + "Event"
	if shape == openapi.SSEEventShapeDataOnly {
		eventSchemaName = typeName + "EventData"
	}

	payload, err := g.generateSchema(ctx, eventSchemaName, schema, false, nil)
	if err != nil {
		return nil, errors.Wrap(err, "generate schema")
	}
	payload.AddFeature("json")

	eventType := payload
	var dataType *ir.Type
	if shape == openapi.SSEEventShapeDataOnly {
		dataType = payload
		retryType, err := g.generateSchema(ctx, typeName+"EventRetry",
			&jsonschema.Schema{Type: jsonschema.Integer}, true, nil)
		if err != nil {
			return nil, errors.Wrap(err, "generate retry schema")
		}

		eventType = &ir.Type{
			Kind: ir.KindStruct,
			Name: typeName + "Event",
			Doc:  fmt.Sprintf("%sEvent is a parsed Server-Sent Event.", typeName),
			Fields: []*ir.Field{
				{Name: "ID", Type: ir.Primitive(ir.String, nil), Tag: ir.Tag{JSON: "id"}},
				{Name: "Type", Type: ir.Primitive(ir.String, nil), Tag: ir.Tag{JSON: "event"}},
				{Name: "Data", Type: payload, Tag: ir.Tag{JSON: "data"}},
				{Name: "Retry", Type: retryType, Tag: ir.Tag{JSON: "retry"}},
			},
		}
		if err := ctx.saveType(eventType); err != nil {
			return nil, errors.Wrap(err, "save event type")
		}
	}

	streamType := &ir.Type{
		Kind: ir.KindStruct,
		Name: typeName,
		SSE: &ir.SSEMetadata{
			Shape:     shape,
			EventType: eventType,
			DataType:  dataType,
		},
	}
	// The concrete SSE stream template provides initSSEStream.
	streamType.DeclareMethod("initSSEStream(sseConnectFunc, sseClientConfig)")
	if err := ctx.saveType(streamType); err != nil {
		return nil, errors.Wrap(err, "save stream type")
	}

	return streamType, nil
}

func isComplexMultipartType(s *jsonschema.Schema) bool {
	if s == nil {
		return true
	}

	switch s.Type {
	case jsonschema.Object, jsonschema.Empty:
		return true
	case jsonschema.Array:
		return len(s.Items) > 0 || isComplexMultipartType(s.Item)
	default:
		return false
	}
}

func (g *Generator) generateContents(
	ctx *genctx,
	name string,
	optional,
	request bool,
	contents map[string]*openapi.MediaType,
) (_ map[ir.ContentType]ir.Media, err error) {
	if err := filterMostSpecific(contents, g.log); err != nil {
		return nil, errors.Wrap(err, "filter most specific")
	}

	var (
		result = make(map[ir.ContentType]ir.Media, len(contents))
		names  = make(map[ir.ContentType]string, len(contents))

		keys        = xmaps.SortedKeys(contents)
		unsupported []string
		lastErr     error
	)

	for _, contentType := range keys {
		media := contents[contentType]

		parsedContentType, encoding, err := normalizeContentEncoding(contentType, g.opt.ContentTypeAliases)
		if err != nil {
			return nil, errors.Wrapf(err, "parse content type %q", contentType)
		}

		typeName := name
		if len(contents) > 1 {
			typeName, err = pascal(name, contentType)
			if err != nil {
				return nil, errors.Wrapf(err, "name for %q", contentType)
			}
		}
		names[ir.ContentType(parsedContentType)] = typeName

		if err := func() error {
			if _, ok := g.opt.ContentTypeAliases[parsedContentType]; ok {
				if ir.Encoding(parsedContentType).MultipartForm() {
					return &ErrNotImplemented{"multipart form CT aliasing"}
				}
			}

			// Handle wildcard content types using configured default
			// unless the schema is binary (which is already handled by isStream check below)
			if strings.ContainsRune(parsedContentType, '*') && g.opt.WildcardContentTypeDefault != "" {
				// Check if this is a binary stream - if so, keep default behavior
				if !isStream(media.Schema) {
					g.log.Info("Mapping wildcard content type",
						zapPosition(media),
						zap.String("contentType", contentType),
						zap.String("mappedTo", string(g.opt.WildcardContentTypeDefault)),
					)
					encoding = g.opt.WildcardContentTypeDefault
					// Use the mapped content type for the result key to avoid wrapping
					parsedContentType = string(encoding)
				}
			}

			if encoding != ir.EncodingJSON && encoding != ir.EncodingProblemJSON && media.XOgenJSONStreaming {
				g.log.Warn(`Extension "x-ogen-json-streaming" will be ignored for non-JSON encoding`,
					zapPosition(media),
					zap.String("contentType", contentType),
				)
			}

			if media.XOgenRawResponse && media.XOgenSSEEventShape.Enabled() {
				g.log.Warn(`Extension "x-ogen-sse-event-shape" will be ignored because "x-ogen-raw-response" is enabled`,
					zapPosition(media),
					zap.String("contentType", contentType),
				)
			} else if media.XOgenSSEEventShape.Enabled() {
				t, err := g.generateSSEContent(ctx, typeName, media)
				if err != nil {
					return errors.Wrap(err, "generate SSE content")
				}
				result[ir.ContentType(parsedContentType)] = ir.Media{
					Encoding:      ir.EncodingEventStream,
					Type:          t,
					JSONStreaming: media.XOgenJSONStreaming,
					RawResponse:   media.XOgenRawResponse,
					SSEEventShape: media.XOgenSSEEventShape,
				}
				return nil
			}

			switch encoding {
			case ir.EncodingJSON:
				t, err := g.generateSchema(ctx, typeName, media.Schema, optional, &generateSchemaOverride{
					request: request,
				})
				if err != nil {
					return errors.Wrap(err, "generate schema")
				}

				t.AddFeature("json")
				result[ir.ContentType(parsedContentType)] = ir.Media{
					Encoding:      encoding,
					Type:          t,
					JSONStreaming: media.XOgenJSONStreaming,
					RawResponse:   media.XOgenRawResponse,
				}
				return nil

			case ir.EncodingProblemJSON:
				// In rfc9457, the only MUST defined for generators is to keep the status field
				// synced with the HTTP status code.
				if media.Schema.Type == jsonschema.Object {
					for _, prop := range media.Schema.Properties {
						if prop.Name == "status" {
							g.log.Warn(`Ensuring "status" matching HTTP status code will not be enforced yet!`,
								zapPosition(media),
								zap.String("contentType", contentType),
							)
							break
						}
					}
				}
				t, err := g.generateSchema(ctx, typeName, media.Schema, optional, &generateSchemaOverride{
					request: request,
				})
				if err != nil {
					return errors.Wrap(err, "generate schema")
				}

				t.AddFeature("json")
				result[ir.ContentType(parsedContentType)] = ir.Media{
					Encoding:      encoding,
					Type:          t,
					JSONStreaming: media.XOgenJSONStreaming,
				}

				return nil

			case ir.EncodingFormURLEncoded:
				t, err := g.generateFormContent(ctx, typeName, media, optional, encoding)
				if err != nil {
					return err
				}

				result[ir.ContentType(parsedContentType)] = ir.Media{
					Encoding:      encoding,
					Type:          t,
					JSONStreaming: media.XOgenJSONStreaming,
					RawResponse:   media.XOgenRawResponse,
				}
				return nil

			case ir.EncodingMultipart:
				t, err := g.generateFormContent(ctx, typeName, media, optional, encoding)
				if err != nil {
					return err
				}

				result[ir.ContentType(parsedContentType)] = ir.Media{
					Encoding:      encoding,
					Type:          t,
					JSONStreaming: media.XOgenJSONStreaming,
					RawResponse:   media.XOgenRawResponse,
				}
				return nil
			default:
				if s := media.Schema; isStream(s) {
					// FIXME(tdakkota): box if optional is true?
					t := ir.Stream(typeName, s)

					switch encoding {
					case ir.EncodingOctetStream, ir.EncodingTextPlain:
					default:
						encoding = ir.EncodingOctetStream
					}
					result[ir.ContentType(parsedContentType)] = ir.Media{
						Encoding:      encoding,
						Type:          t,
						JSONStreaming: media.XOgenJSONStreaming,
						RawResponse:   media.XOgenRawResponse,
					}
					return ctx.saveType(t)
				} else if media.XOgenRawResponse {
					t, err := g.generateSchema(ctx, typeName, media.Schema, optional, nil)
					if err != nil {
						return errors.Wrap(err, "generate schema")
					}

					result[ir.ContentType(parsedContentType)] = ir.Media{
						Encoding:      encoding,
						Type:          t,
						JSONStreaming: media.XOgenJSONStreaming,
						RawResponse:   media.XOgenRawResponse,
					}
					return nil
				}

				g.log.Info(`Content type is unsupported, set "format" to "binary" to use io.Reader`,
					zapPosition(media),
					zap.String("contentType", contentType),
				)
				unsupported = append(unsupported, contentType)
				return nil
			}
		}(); err != nil {
			err = errors.Wrapf(err, "media: %q", contentType)
			if err := g.trySkip(err, "Skipping media", media); err != nil {
				return nil, err
			}
			lastErr = err
			unsupported = append(unsupported, contentType)
			continue
		}
	}

	if len(result) == 0 && len(unsupported) > 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, &ErrUnsupportedContentTypes{ContentTypes: unsupported}
	}

	if request {
		for ct, m := range result {
			if !ct.Mask() {
				continue
			}
			t, err := g.wrapContent(ctx, names[ct], m.Type)
			if err != nil {
				return nil, err
			}
			result[ct] = ir.Media{
				Type:          t,
				Encoding:      m.Encoding,
				JSONStreaming: m.JSONStreaming,
				RawResponse:   m.RawResponse,
				SSEEventShape: m.SSEEventShape,
			}
		}
	}

	return result, nil
}
