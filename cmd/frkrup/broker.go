package main

import (
	"fmt"
	"strings"

	"github.com/segmentio/kafka-go"
)

// BrokerManager handles broker operations
type BrokerManager struct {
	brokerURL string
}

// NewBrokerManager creates a new BrokerManager
func NewBrokerManager(brokerURL string) *BrokerManager {
	return &BrokerManager{brokerURL: brokerURL}
}

// CreateTopic creates a topic for a stream (Kafka Protocol compliant).
// Security: This function is only called after:
// 1. Stream has been created in the database (validated)
// 2. Topic name comes from the database (not user input)
func (bm *BrokerManager) CreateTopic(topicName string) error {
	topicConfigs := []kafka.TopicFrkrupConfig{
		{
			Topic:             topicName,
			NumPartitions:     1,
			ReplicationFactor: 1,
		},
	}

	// 1. Try to use the bootstrap broker connection directly first.
	// In single-node environments like ours, the bootstrap broker is usually the controller.
	conn, err := kafka.Dial("tcp", bm.brokerURL)
	if err != nil {
		return fmt.Errorf("failed to connect to broker: %w", err)
	}
	defer conn.Close()

	err = conn.CreateTopics(topicConfigs...)
	if err == nil {
		return nil
	}
	// If it already exists, that's fine
	if strings.Contains(err.Error(), "already exists") {
		return nil
	}

	// 2. If it failed with NotController, we need to find the controller.
	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("failed to get controller: %w", err)
	}

	controllerAddr := fmt.Sprintf("%s:%d", controller.Host, controller.Port)

	// PORT-FORWARD HANDLING:
	// If we are connecting via localhost (port-forward), the broker might advertise its
	// internal Kubernetes hostname (e.g., 'frkr-redpanda') which we can't resolve on the host.
	isLocal := strings.HasPrefix(bm.brokerURL, "localhost:") || strings.HasPrefix(bm.brokerURL, "127.0.0.1:")
	isControllerInternal := controller.Host != "localhost" && controller.Host != "127.0.0.1"

	if isLocal && isControllerInternal {
		// Fallback: assume the controller is reachable via the same port-forwarded address
		controllerAddr = bm.brokerURL
	}

	controllerConn, err := kafka.Dial("tcp", controllerAddr)
	if err != nil {
		return fmt.Errorf("failed to connect to controller: %w", err)
	}
	defer controllerConn.Close()

	err = controllerConn.CreateTopics(topicConfigs...)
	if err != nil {
		// Topic might already exist, which is fine
		if strings.Contains(err.Error(), "already exists") {
			return nil
		}
		return fmt.Errorf("failed to create topic: %w", err)
	}

	return nil
}
