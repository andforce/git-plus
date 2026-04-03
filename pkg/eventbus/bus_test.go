package eventbus

import (
	"sync"
	"testing"
	"time"
)

func TestPublishBroadcastsToAllSubscribersOnChannel(t *testing.T) {
	bus := New()
	defer bus.Close()

	firstSubscription, firstEvents := bus.Subscribe("task")
	secondSubscription, secondEvents := bus.Subscribe("task")
	defer bus.Unsubscribe(firstSubscription)
	defer bus.Unsubscribe(secondSubscription)

	bus.Publish("task", map[string]any{"event_name": "task.started"})

	assertEventField(t, receiveEvent(t, firstEvents), "event_name", "task.started")
	assertEventField(t, receiveEvent(t, secondEvents), "event_name", "task.started")
}

func TestPublishDropsEventsWithoutSubscribers(t *testing.T) {
	bus := New()
	defer bus.Close()

	bus.Publish("task", map[string]any{"event_name": "task.started"})

	subscription, events := bus.Subscribe("task")
	defer bus.Unsubscribe(subscription)

	select {
	case event := <-events:
		t.Fatalf("expected no replayed event, got %#v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestChannelsAreIsolated(t *testing.T) {
	bus := New()
	defer bus.Close()

	taskSubscription, taskEvents := bus.Subscribe("task")
	otherSubscription, otherEvents := bus.Subscribe("other")
	defer bus.Unsubscribe(taskSubscription)
	defer bus.Unsubscribe(otherSubscription)

	bus.Publish("task", map[string]any{"event_name": "task.started"})

	assertEventField(t, receiveEvent(t, taskEvents), "event_name", "task.started")

	select {
	case event := <-otherEvents:
		t.Fatalf("expected no event on unrelated channel, got %#v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	bus := New()
	defer bus.Close()

	subscription, events := bus.Subscribe("task")
	bus.Unsubscribe(subscription)

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected unsubscribed channel to be closed")
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("expected unsubscribed channel to close")
	}
}

func TestSlowSubscriberIsClosedWhenBufferIsFull(t *testing.T) {
	bus := New()
	defer bus.Close()

	subscription, events := bus.Subscribe("task")
	for i := 0; i < defaultSubscriberBuffer+1; i++ {
		bus.Publish("task", map[string]any{"event_name": "task.progress"})
	}

	drainUntilClosed(t, events)

	bus.Publish("task", map[string]any{"event_name": "task.finished"})
	bus.Unsubscribe(subscription)
}

func TestPayloadIsClonedPerDelivery(t *testing.T) {
	bus := New()
	defer bus.Close()

	subscription, events := bus.Subscribe("task")
	defer bus.Unsubscribe(subscription)

	payload := map[string]any{
		"data": map[string]any{
			"task_id": "task-1",
		},
	}
	bus.Publish("task", payload)

	event := receiveEvent(t, events)
	event.Payload["data"].(map[string]any)["task_id"] = "changed"

	nextPayload := map[string]any{
		"data": map[string]any{
			"task_id": "task-2",
		},
	}
	bus.Publish("task", nextPayload)
	nextEvent := receiveEvent(t, events)
	assertEventNestedField(t, nextEvent, "data", "task_id", "task-2")
}

func TestPublishDoesNotPanicWhenConcurrentUnsubscribeClosesChannel(t *testing.T) {
	bus := New()
	defer bus.Close()

	subscription, events := bus.Subscribe("task")
	releaseSend := make(chan struct{})
	sendStarted := make(chan struct{})
	go func() {
		subscription.mu.Lock()
		close(sendStarted)
		<-releaseSend
		close(subscription.ch)
		subscription.closed = true
		subscription.mu.Unlock()
	}()

	<-sendStarted

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Errorf("publish panicked: %v", recovered)
			}
		}()
		bus.Publish("task", map[string]any{"event_name": "task.started"})
	}()

	close(releaseSend)
	wg.Wait()

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("expected subscription channel to be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for closed subscription")
	}
}

func receiveEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()

	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return Event{}
	}
}

func drainUntilClosed(t *testing.T, events <-chan Event) {
	t.Helper()

	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for slow subscriber to close")
		}
	}
}

func assertEventField(t *testing.T, event Event, key string, expected any) {
	t.Helper()

	if got := event.Payload[key]; got != expected {
		t.Fatalf("unexpected event payload %q: got=%#v want=%#v", key, got, expected)
	}
}

func assertEventNestedField(t *testing.T, event Event, key string, nestedKey string, expected any) {
	t.Helper()

	nested, ok := event.Payload[key].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map at %q, got %#v", key, event.Payload[key])
	}
	if got := nested[nestedKey]; got != expected {
		t.Fatalf("unexpected nested payload %q.%q: got=%#v want=%#v", key, nestedKey, got, expected)
	}
}
