package pubsub

import (
	"testing"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
)

func TestSubscribePublish(t *testing.T) {
	b := NewBroker()
	topic := "test:topic"
	userID := model.NewUUID()

	sub := b.Subscribe(topic, "sub-1", userID)

	go func() {
		b.Publish(topic, model.WSEvent{
			Type:  "test.event",
			Topic: topic,
		})
	}()

	select {
	case event := <-sub.Ch:
		if event.Type != "test.event" {
			t.Errorf("expected test.event, got %s", event.Type)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for event")
	}
}

func TestUnsubscribe(t *testing.T) {
	b := NewBroker()
	topic := "test:topic"
	userID := model.NewUUID()

	b.Subscribe(topic, "sub-1", userID)
	b.Unsubscribe(topic, "sub-1")

	// Should not panic - topic removed
	b.Publish(topic, model.WSEvent{Type: "test"})
}

func TestUnsubscribeAll(t *testing.T) {
	b := NewBroker()
	userID := model.NewUUID()

	b.Subscribe("topic-a", "sub-1", userID)
	b.Subscribe("topic-b", "sub-1", userID)
	b.UnsubscribeAll("sub-1")

	// Should not panic
	b.Publish("topic-a", model.WSEvent{Type: "test"})
	b.Publish("topic-b", model.WSEvent{Type: "test"})
}

func TestPublishScoped(t *testing.T) {
	b := NewBroker()
	topic := "test:topic"
	user1 := model.NewUUID()
	user2 := model.NewUUID()

	sub1 := b.Subscribe(topic, "sub-1", user1)
	_ = b.Subscribe(topic, "sub-2", user2)

	b.PublishScoped(topic, user1, model.WSEvent{Type: "private"})

	select {
	case event := <-sub1.Ch:
		if event.Type != "private" {
			t.Errorf("expected private, got %s", event.Type)
		}
	case <-time.After(1 * time.Second):
		t.Error("user1 should receive scoped event")
	}
}

func TestPublishWithoutSubscribers(t *testing.T) {
	b := NewBroker()
	// Should not panic when publishing to topic with no subscribers
	b.Publish("empty:topic", model.WSEvent{Type: "test"})
	b.PublishScoped("empty:topic", model.NewUUID(), model.WSEvent{Type: "test"})
}

func TestTopicHelpers(t *testing.T) {
	jobID := model.NewUUID()
	agentID := model.NewUUID()
	deviceID := model.NewUUID()
	skillID := model.NewUUID()

	if JobTopic(jobID) != "job:"+jobID.String() {
		t.Error("job topic mismatch")
	}
	if AgentTopic(agentID) != "agent:"+agentID.String() {
		t.Error("agent topic mismatch")
	}
	if DeviceTopic(deviceID) != "device:"+deviceID.String() {
		t.Error("device topic mismatch")
	}
	if SkillTopic(skillID) != "skill:"+skillID.String() {
		t.Error("skill topic mismatch")
	}
}
