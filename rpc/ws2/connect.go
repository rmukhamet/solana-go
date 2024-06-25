package ws2

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/rpc/v2/json2"
	"github.com/gorilla/websocket"
	jsoniter "github.com/json-iterator/go"
	"github.com/rmukhamet/solana-go"
)

type SubscriptionMeta struct {
	UnsubscribeMethod  string
	SubscriptionMethod string
	Encoding           solana.EncodingType
}

type Connection struct {
	id uint64
	*websocket.Conn
	maxSubscriptions int
	subscriptions    map[uint64]SubscriptionMeta //subscriptionID
	requestIDSubID   map[uint64]SubscriptionMeta
	lock             sync.RWMutex
	logger           Logger
}

func (c *Connection) ID() uint64 {
	if c.id == 0 {
		c.id = connectionID.Add(1)
	}

	return c.id
}

func (c *Connection) sendPing(errorCh chan MessageError) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.SetWriteDeadline(time.Now().Add(writeWait))
	if err := c.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
		subscriptions := make([]uint64, 0, len(c.subscriptions))
		for sID := range c.subscriptions {
			subscriptions = append(subscriptions, sID)
		}

		errorCh <- MessageError{
			err:           err,
			connectionID:  c.ID(),
			subscriptions: subscriptions,
		}

		return
	}
}

func (c *Connection) Close() {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.subscriptions = make(map[uint64]SubscriptionMeta, c.maxSubscriptions)
	c.Conn.Close()
}
func (c *Connection) closeSubscription(subID uint64) {
	c.lock.Lock()
	defer c.lock.Unlock()

	err := c.unsubscribe(subID)
	if err != nil {
		c.logger.Error("unable to unsubscribe %s", err.Error())
	}

	delete(c.subscriptions, subID)
}

func (c *Connection) unsubscribe(subID uint64) error {
	sub, ok := c.subscriptions[subID]
	if ok != true {
		return fmt.Errorf("subscription with subID %d not found", subID)

	}

	method := sub.UnsubscribeMethod

	req := newRequest([]interface{}{subID}, method, nil)
	data, err := req.encode()
	if err != nil {
		return fmt.Errorf("unable to encode unsubscription message for subID %d and method %s", subID, method)
	}

	c.SetWriteDeadline(time.Now().Add(writeWait))

	err = c.WriteMessage(websocket.TextMessage, data)
	if err != nil {
		return fmt.Errorf("unable to send unsubscription message for subID %d and method %s", subID, method)
	}

	return nil
}

func (c *Connection) receiveMessages(ctx context.Context, receivedMessagesCh chan MessageWS, errorCh chan MessageError) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			_, messageData, err := c.ReadMessage()
			if err != nil {
				// gen errror
				subscriptions := make([]uint64, 0, len(c.subscriptions))
				for sID := range c.subscriptions {
					subscriptions = append(subscriptions, sID)
				}

				errorCh <- MessageError{
					err:           err,
					connectionID:  c.ID(),
					subscriptions: subscriptions,
				}

				return
			}

			/*
				{
					"jsonrpc": "2.0",
					"result": 4649979757127370,
					"id": 1
				}
			*/

			// when receiving message with id. the result will be a subscription number.
			// that number will be associated to all future message destine to this request

			requestID, ok := getUint64WithOk(messageData, "id")
			if ok {
				subID, _ := getUint64WithOk(messageData, "result")
				// old handler
				// c.handleNewSubscriptionMessage(requestID, subID)
				c.mapRequestID(uint64(requestID), uint64(subID))
				continue
			}

			data, err := c.preParseMessage(messageData)
			if err != nil {
				c.logger.Error("unable to preparse ws message %s with error: %s\n", string(messageData), err.Error())
				continue
			}

			subID, _ := getUint64WithOk(messageData, "params", "subscription")

			subscriptionMethod := c.subscriptions[subID].SubscriptionMethod

			encoding := c.subscriptions[subID].Encoding

			message := MessageWS{
				SubscriptionID:     subID,
				Data:               data,
				ConnectionID:       c.ID(),
				SubscriptionMethod: subscriptionMethod,
				Encoding:           encoding,
			}

			receivedMessagesCh <- message
		}
	}
}
func (c *Connection) mapRequestID(reqID, subID uint64) {
	c.lock.Lock()
	defer c.lock.Unlock()
	connectionM, ok := c.requestIDSubID[reqID]
	if ok {
		c.subscriptions[subID] = connectionM
		return
	}
}

func (c *Connection) Subscribe(
	params []interface{},
	conf map[string]interface{},
	subscriptionMethod string,
	unsubscribeMethod string,
	decoderFunc decoderFunc,
) (reqID uint64, err error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	req := newRequest(params, subscriptionMethod, conf)
	data, err := req.encode()
	if err != nil {
		return 0, fmt.Errorf("subscribe: unable to encode subsciption request: %w", err)
	}

	c.SetWriteDeadline(time.Now().Add(writeWait))

	err = c.WriteMessage(websocket.TextMessage, data)
	if err != nil {
		return 0, fmt.Errorf("unable to write request: %w", err)
	}

	encodingType, ok := conf["encoding"].(solana.EncodingType)
	if !ok {
		encodingType = solana.EncodingBase64
	}

	c.requestIDSubID[req.ID] = SubscriptionMeta{
		SubscriptionMethod: subscriptionMethod,
		UnsubscribeMethod:  unsubscribeMethod,
		Encoding:           encodingType,
	}

	return req.ID, nil
}

func (c *Connection) preParseMessage(data []byte) (json.RawMessage, error) {
	var rpcMessage *response

	if err := jsoniter.Unmarshal(data, &rpcMessage); err != nil {
		return nil, fmt.Errorf("unable to unmarshal message: %w", err)
	}

	if rpcMessage.Error != nil {
		jsonErr := &json2.Error{}
		if err := json.Unmarshal(*rpcMessage.Error, jsonErr); err != nil {
			return nil, &json2.Error{
				Code:    json2.E_SERVER,
				Message: string(*rpcMessage.Error),
			}
		}
		return nil, jsonErr
	}

	if rpcMessage.Params == nil || (rpcMessage.Params.Result == nil && rpcMessage.Params.Error == nil) {
		return nil, json2.ErrNullResult
	}

	if rpcMessage.Params.Error != nil {
		var errMessage string
		json.Unmarshal(*rpcMessage.Params.Error, &errMessage)

		return nil, fmt.Errorf("rpc error: %s", errMessage)
	}
	return *rpcMessage.Params.Result, nil
}
