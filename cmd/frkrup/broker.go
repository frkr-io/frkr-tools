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
	conn, err := kafka.Dial("tcp", bm.brokerURL)
	if err != nil {
		return fmt.Errorf("failed to connect to broker: %w", err)
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("failed to get controller: %w", err)
	}

	controllerConn, err := kafka.Dial("tcp", fmt.Sprintf("%s:%d", controller.Host, controller.Port))
	if err != nil {
		return fmt.Errorf("failed to connect to controller: %w", err)
	}
	defer controllerConn.Close()

	topicConfigs := []kafka.TopicConfig{
		{
			Topic:             topicName,
			NumPartitions:     1,
			ReplicationFactor: 1,
		},
	}

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
