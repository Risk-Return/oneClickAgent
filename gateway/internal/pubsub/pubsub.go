// Package pubsub provides an in-process topic broker for real-time event fan-out.
// Topics are keyed by job/agent/device IDs; subscribers are scoped to user_id
// for tenant isolation. Used by web WS to stream job.progress, agent.status, etc.
package pubsub

import (
	"sync"

	"github.com/oneClickAgent/gateway/internal/model"
)

// Subscriber receives events pushed to its channel.
type Subscriber struct {
	UserID model.UUID
	Ch     chan model.WSEvent
}

// Broker is an in-process pub/sub broker for real-time events.
type Broker struct {
	mu    sync.RWMutex
	subs  map[string]map[string]*Subscriber // topic -> subscriberID -> subscriber
}

// NewBroker creates a new pub/sub broker.
func NewBroker() *Broker {
	return &Broker{
		subs: make(map[string]map[string]*Subscriber),
	}
}

// Subscribe adds a subscriber to a topic.
// subscriberID should uniquely identify the connection (e.g., user_id + session).
func (b *Broker) Subscribe(topic string, subscriberID string, userID model.UUID) *Subscriber {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.subs[topic]; !ok {
		b.subs[topic] = make(map[string]*Subscriber)
	}

	// Replace existing subscription for this ID
	sub := &Subscriber{
		UserID: userID,
		Ch:     make(chan model.WSEvent, 64),
	}
	b.subs[topic][subscriberID] = sub
	return sub
}

// Unsubscribe removes a subscriber from a topic.
func (b *Broker) Unsubscribe(topic string, subscriberID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if subs, ok := b.subs[topic]; ok {
		if sub, ok := subs[subscriberID]; ok {
			close(sub.Ch)
			delete(subs, subscriberID)
		}
		if len(subs) == 0 {
			delete(b.subs, topic)
		}
	}
}

// UnsubscribeAll removes a subscriber from all topics.
func (b *Broker) UnsubscribeAll(subscriberID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for topic, subs := range b.subs {
		if sub, ok := subs[subscriberID]; ok {
			close(sub.Ch)
			delete(subs, subscriberID)
		}
		if len(subs) == 0 {
			delete(b.subs, topic)
		}
	}
}

// Publish sends an event to all subscribers of a topic.
func (b *Broker) Publish(topic string, event model.WSEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	subs, ok := b.subs[topic]
	if !ok {
		return
	}

	for _, sub := range subs {
		select {
		case sub.Ch <- event:
		default:
			// Drop if subscriber buffer is full
		}
	}
}

// PublishScoped publishes an event to subscribers of a topic, scoped to a specific user.
func (b *Broker) PublishScoped(topic string, userID model.UUID, event model.WSEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	subs, ok := b.subs[topic]
	if !ok {
		return
	}

	for _, sub := range subs {
		if sub.UserID == userID {
			select {
			case sub.Ch <- event:
			default:
			}
		}
	}
}

// Topic helpers for consistency.
func JobTopic(jobID model.UUID) string {
	return "job:" + jobID.String()
}

func AgentTopic(agentID model.UUID) string {
	return "agent:" + agentID.String()
}

func DeviceTopic(deviceID model.UUID) string {
	return "device:" + deviceID.String()
}

func SkillTopic(skillID model.UUID) string {
	return "skill:" + skillID.String()
}
