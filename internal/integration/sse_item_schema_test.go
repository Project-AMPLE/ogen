package integration

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	api "github.com/ogen-go/ogen/internal/integration/test_sse_item_schema"
)

type sseItemSchemaHandler struct {
	events []api.StreamEventsOKEvent
}

func (h sseItemSchemaHandler) StreamEvents(context.Context) (*api.StreamEventsOK, error) {
	return &api.StreamEventsOK{
		Events: func(ctx context.Context, s api.StreamEventsOKSender) error {
			for _, event := range h.events {
				if err := s.Send(ctx, event); err != nil {
					return err
				}
			}
			return nil
		},
	}, nil
}

// TestSSEItemSchemaTypedPayloads covers the OpenAPI 3.2 SSE shape: an
// `itemSchema` envelope `{event: string, data: string}` refined by one oneOf
// branch per event, each pinning `event` to a const and typing `data` through
// `contentMediaType: application/json` + `contentSchema: $ref`.
//
// What regresses if it fails:
//   - the const `event` (declared without a type, the envelope supplies it)
//     falls back to jx.Raw and the package stops compiling;
//   - `data` falls back to jx.Raw, so a handler can no longer send a typed
//     payload and the referenced component is only reachable as raw JSON;
//   - the payload is double-encoded (a JSON string inside `data:`) instead of
//     being the JSON text itself, which is what contentSchema describes.
func TestSSEItemSchemaTypedPayloads(t *testing.T) {
	events := []api.StreamEventsOKEvent{
		api.NewStreamEventsOKEventChunkStreamEventsOKEvent(api.StreamEventsOKEventChunk{
			ID:   "1",
			Data: api.StreamChunk{Content: "hel"},
		}),
		api.NewStreamEventsOKEventChunkStreamEventsOKEvent(api.StreamEventsOKEventChunk{
			ID:   "2",
			Data: api.StreamChunk{Content: "lo"},
		}),
		// contentMediaType text/plain: data stays the envelope's string and
		// goes out unquoted.
		api.NewStreamEventsOKEventLogStreamEventsOKEvent(api.StreamEventsOKEventLog{
			ID:   "3",
			Data: "plain line",
		}),
		api.NewStreamEventsOKEventDoneStreamEventsOKEvent(api.StreamEventsOKEventDone{
			ID:   "4",
			Data: api.StreamDone{Result: api.NewOptString("ok")},
		}),
		api.NewStreamEventsOKEventErrorStreamEventsOKEvent(api.StreamEventsOKEventError{
			ID:   "5",
			Data: api.StreamError{Message: "boom"},
		}),
	}

	srv, err := api.NewServer(sseItemSchemaHandler{events: events})
	require.NoError(t, err)
	s := httptest.NewServer(srv)
	t.Cleanup(s.Close)

	t.Run("Wire", func(t *testing.T) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.URL+"/stream", nil)
		require.NoError(t, err)
		resp, err := s.Client().Do(req)
		require.NoError(t, err)
		defer func() { require.NoError(t, resp.Body.Close()) }()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
		require.Equal(t, ""+
			"id: 1\nevent: chunk\ndata: {\"content\":\"hel\"}\n\n"+
			"id: 2\nevent: chunk\ndata: {\"content\":\"lo\"}\n\n"+
			"id: 3\nevent: log\ndata: plain line\n\n"+
			"id: 4\nevent: done\ndata: {\"result\":\"ok\"}\n\n"+
			"id: 5\nevent: error\ndata: {\"message\":\"boom\"}\n\n",
			string(body))
	})
	t.Run("Client", func(t *testing.T) {
		client, err := api.NewClient(s.URL, api.WithClient(s.Client()))
		require.NoError(t, err)
		stream, err := client.StreamEvents(t.Context())
		require.NoError(t, err)
		defer func() { require.NoError(t, stream.Close()) }()

		got := make([]api.StreamEventsOKEvent, 0, len(events))
		for range events {
			event, err := stream.Next(t.Context())
			require.NoError(t, err)
			got = append(got, event)
		}
		// The handler never sets the const `event` field (the encoder writes
		// the const), while the decoder keeps what it read.
		want := slices.Clone(events)
		for i := range want {
			switch v := &want[i]; v.Type {
			case api.StreamEventsOKEventChunkStreamEventsOKEvent:
				v.StreamEventsOKEventChunk.Event = "chunk"
			case api.StreamEventsOKEventDoneStreamEventsOKEvent:
				v.StreamEventsOKEventDone.Event = "done"
			case api.StreamEventsOKEventErrorStreamEventsOKEvent:
				v.StreamEventsOKEventError.Event = "error"
			case api.StreamEventsOKEventLogStreamEventsOKEvent:
				v.StreamEventsOKEventLog.Event = "log"
			}
		}
		require.Equal(t, want, got)

		chunk, ok := got[0].GetStreamEventsOKEventChunk()
		require.True(t, ok)
		require.Equal(t, "hel", chunk.Data.Content)
	})
}
