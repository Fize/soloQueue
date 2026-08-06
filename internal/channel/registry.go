package channel

import (
	"context"
	"sync"
)

// ChannelSenderFactory is a function capable of sending a message using serialized metadata.
type ChannelSenderFactory func(ctx context.Context, data []byte, text string) error

var (
	senderFactoriesMu sync.RWMutex
	senderFactories   = make(map[string]ChannelSenderFactory)
)

// RegisterSenderFactory registers a factory function for a specific channel type.
func RegisterSenderFactory(channelType string, factory ChannelSenderFactory) {
	senderFactoriesMu.Lock()
	defer senderFactoriesMu.Unlock()
	senderFactories[channelType] = factory
}

// GetSenderFactory retrieves the factory function for a specific channel type.
func GetSenderFactory(channelType string) ChannelSenderFactory {
	senderFactoriesMu.RLock()
	defer senderFactoriesMu.RUnlock()
	return senderFactories[channelType]
}
