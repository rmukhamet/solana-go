package ws2

import "github.com/rmukhamet/solana-go"

type MessageWS struct {
	Account            string
	SubscriptionID     uint64
	Data               []byte
	ConnectionID       uint64
	SubscriptionMethod string
	Encoding           solana.EncodingType
}

type MessageError struct {
	err           error
	connectionID  uint64
	subscriptions []uint64
}

type decoderFunc func([]byte) (interface{}, error)
