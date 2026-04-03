package eventservice

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/ImSingee/git-plus/pkg/eventbus"
	eventv1 "github.com/ImSingee/git-plus/pkg/rpc/gitplus/event/v1"
	"github.com/ImSingee/git-plus/pkg/rpc/gitplus/event/v1/eventv1connect"
	"github.com/ImSingee/git-plus/pkg/task"
)

func TestSubscribeOmitsUnsupportedMetadataWithoutClosingStream(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	mux := http.NewServeMux()
	mux.Handle("/api/", NewHandler(bus))
	server := httptest.NewServer(mux)
	defer server.Close()

	client := eventv1connect.NewEventServiceClient(http.DefaultClient, server.URL+"/api")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	streamResult := make(chan *connect.ServerStreamForClient[eventv1.SubscribeResponse], 1)
	streamError := make(chan error, 1)
	go func() {
		stream, err := client.Subscribe(
			ctx,
			connect.NewRequest(&eventv1.SubscribeRequest{Channel: stringPtr(task.TaskChannel)}),
		)
		if err != nil {
			streamError <- err
			return
		}
		streamResult <- stream
	}()

	var stream *connect.ServerStreamForClient[eventv1.SubscribeResponse]
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for stream == nil {
		select {
		case err := <-streamError:
			t.Fatalf("subscribe: %v", err)
		case stream = <-streamResult:
		case <-ticker.C:
			bus.Publish(task.TaskChannel, map[string]any{
				"event_name": "task.progress",
				"channel":    task.TaskChannel,
				"data": map[string]any{
					"task": map[string]any{
						"task_id": "task-1",
						"progress": map[string]any{
							"summary": "Running",
							"meta": map[string]any{
								"good": "value",
								"bad":  time.Now(),
							},
						},
					},
				},
			})
		case <-deadline:
			t.Fatal("timed out waiting for event stream")
		}
	}

	if !stream.Receive() {
		t.Fatalf("expected first event, got error: %v", stream.Err())
	}

	event := stream.Msg().GetEvent()
	if event.GetFields()["event_name"].GetStringValue() != "task.progress" {
		t.Fatalf("unexpected event name: %q", event.GetFields()["event_name"].GetStringValue())
	}

	metaFields := event.
		GetFields()["data"].GetStructValue().
		GetFields()["task"].GetStructValue().
		GetFields()["progress"].GetStructValue().
		GetFields()["meta"].GetStructValue().
		GetFields()

	if metaFields["good"].GetStringValue() != "value" {
		t.Fatalf("expected good metadata to remain, got %#v", metaFields["good"])
	}
	if _, exists := metaFields["bad"]; exists {
		t.Fatalf("expected unsupported metadata to be omitted, got %#v", metaFields["bad"])
	}

	bus.Publish(task.TaskChannel, map[string]any{
		"event_name": "task.finished",
		"channel":    task.TaskChannel,
	})

	if !stream.Receive() {
		t.Fatalf("expected second event, got error: %v", stream.Err())
	}
	if stream.Msg().GetEvent().GetFields()["event_name"].GetStringValue() != "task.finished" {
		t.Fatalf("unexpected second event name: %q", stream.Msg().GetEvent().GetFields()["event_name"].GetStringValue())
	}
}

func stringPtr(value string) *string {
	return &value
}
