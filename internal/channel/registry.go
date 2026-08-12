package channel

import (
	"context"
	"sync"
)

// ChannelSenderFactory is a function capable of sending a message using serialized metadata.
type ChannelSenderFactory func(ctx context.Context, data []byte, text string) error

// ChannelMediaSenderFactory sends media using serialized immutable route metadata.
type ChannelMediaSenderFactory func(ctx context.Context, data []byte, media []OutboundMedia) error

var (
	senderFactoriesMu sync.RWMutex
	senderFactories   = make(map[string]ChannelSenderFactory)
	mediaFactories    = make(map[string]ChannelMediaSenderFactory)
)

// RegisterSenderFactory registers a factory function for a specific channel type.
func RegisterSenderFactory(channelType string, factory ChannelSenderFactory) {
	senderFactoriesMu.Lock()
	defer senderFactoriesMu.Unlock()
	senderFactories[channelType] = factory
}

// RegisterMediaSenderFactory registers a media factory for a channel type.
func RegisterMediaSenderFactory(channelType string, factory ChannelMediaSenderFactory) {
	senderFactoriesMu.Lock()
	defer senderFactoriesMu.Unlock()
	mediaFactories[channelType] = factory
}

// GetMediaSenderFactory retrieves a media factory for a channel type.
func GetMediaSenderFactory(channelType string) ChannelMediaSenderFactory {
	senderFactoriesMu.RLock()
	defer senderFactoriesMu.RUnlock()
	return mediaFactories[channelType]
}

// GetSenderFactory retrieves the factory function for a specific channel type.
func GetSenderFactory(channelType string) ChannelSenderFactory {
	senderFactoriesMu.RLock()
	defer senderFactoriesMu.RUnlock()
	return senderFactories[channelType]
}
