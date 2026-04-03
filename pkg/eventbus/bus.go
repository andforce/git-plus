package eventbus

import (
	"sync"
)

const defaultSubscriberBuffer = 16

type Event struct {
	Channel string
	Payload map[string]any
}

type Subscription struct {
	id      uint64
	channel string
	ch      chan Event
	mu      sync.RWMutex
	closed  bool
}

type Bus struct {
	mu             sync.Mutex
	nextID         uint64
	closed         bool
	subscriberSize int
	channels       map[string]map[uint64]*Subscription
}

func New() *Bus {
	return &Bus{
		subscriberSize: defaultSubscriberBuffer,
		channels:       make(map[string]map[uint64]*Subscription),
	}
}

func (bus *Bus) Publish(channel string, payload map[string]any) {
	bus.mu.Lock()
	if bus.closed {
		bus.mu.Unlock()
		return
	}

	subscribers := make([]*Subscription, 0)
	if channelSubscribers := bus.channels[channel]; len(channelSubscribers) > 0 {
		for _, subscription := range channelSubscribers {
			subscribers = append(subscribers, subscription)
		}
	}
	bus.mu.Unlock()

	if len(subscribers) == 0 {
		return
	}

	event := Event{
		Channel: channel,
		Payload: clonePayload(payload),
	}

	for _, subscription := range subscribers {
		if !subscription.tryDeliver(event) {
			bus.removeSubscription(subscription)
		}
	}
}

func (bus *Bus) Subscribe(channel string) (*Subscription, <-chan Event) {
	bus.mu.Lock()
	defer bus.mu.Unlock()

	if bus.closed {
		closedChannel := make(chan Event)
		close(closedChannel)
		return &Subscription{channel: channel, ch: closedChannel}, closedChannel
	}

	bus.nextID++
	subscription := &Subscription{
		id:      bus.nextID,
		channel: channel,
		ch:      make(chan Event, bus.subscriberSize),
	}

	channelSubscribers := bus.channels[channel]
	if channelSubscribers == nil {
		channelSubscribers = make(map[uint64]*Subscription)
		bus.channels[channel] = channelSubscribers
	}
	channelSubscribers[subscription.id] = subscription

	return subscription, subscription.ch
}

func (bus *Bus) Unsubscribe(subscription *Subscription) {
	if subscription == nil {
		return
	}

	bus.removeSubscription(subscription)
}

func (bus *Bus) Close() {
	bus.mu.Lock()
	defer bus.mu.Unlock()

	if bus.closed {
		return
	}
	bus.closed = true

	for channel, channelSubscribers := range bus.channels {
		for _, subscription := range channelSubscribers {
			subscription.close()
		}
		delete(bus.channels, channel)
	}
}

func (bus *Bus) removeSubscription(subscription *Subscription) {
	bus.mu.Lock()
	defer bus.mu.Unlock()

	channelSubscribers := bus.channels[subscription.channel]
	if channelSubscribers == nil {
		return
	}

	if _, exists := channelSubscribers[subscription.id]; !exists {
		return
	}

	delete(channelSubscribers, subscription.id)
	subscription.close()

	if len(channelSubscribers) == 0 {
		delete(bus.channels, subscription.channel)
	}
}

func (subscription *Subscription) tryDeliver(event Event) bool {
	subscription.mu.RLock()
	defer subscription.mu.RUnlock()

	if subscription.closed {
		return false
	}

	select {
	case subscription.ch <- event:
		return true
	default:
		return false
	}
}

func (subscription *Subscription) close() {
	subscription.mu.Lock()
	defer subscription.mu.Unlock()

	if subscription.closed {
		return
	}

	subscription.closed = true
	close(subscription.ch)
}

func clonePayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return nil
	}

	cloned := make(map[string]any, len(payload))
	for key, value := range payload {
		cloned[key] = cloneValue(value)
	}

	return cloned
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return clonePayload(typed)
	case []any:
		cloned := make([]any, 0, len(typed))
		for _, item := range typed {
			cloned = append(cloned, cloneValue(item))
		}
		return cloned
	default:
		return value
	}
}
