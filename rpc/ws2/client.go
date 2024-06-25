// Copyright 2021 github.com/gagliardetto
// This file has been modified by github.com/gagliardetto
//
// Copyright 2020 dfuse Platform Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ws2

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/buger/jsonparser"
	"github.com/gorilla/rpc/v2/json2"
	"github.com/gorilla/websocket"
)

var connectionID atomic.Uint64

type result interface{}

type Client struct {
	rpcURL           string
	connections      map[uint64]*Connection
	maxConnections   int
	maxSubscriptions int
	connCtx          context.Context
	connCtxCancel    context.CancelFunc
	lock             sync.RWMutex

	reconnectOnErr bool

	receivedMessagesCh chan MessageWS
	errorMessageCh     chan MessageError

	subscriptionByID map[uint64]*SubscriptionMeta
	logger           Logger
}

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second
	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second
	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10
)

func New(rpcURL string, logger Logger, maxConnections int, maxSubscriptions int) *Client {
	c := &Client{
		rpcURL:           rpcURL,
		connections:      make(map[uint64]*Connection, maxConnections),
		maxConnections:   maxConnections,
		maxSubscriptions: maxSubscriptions,
		logger:           logger,
	}

	go func() {
		for {
			select {
			case mErr := <-c.errorMessageCh:
				logger.Error("received error from ws connection")

				c.removeConnection(mErr.connectionID, mErr.subscriptions)

				c.logger.Infof("removed connection id: %d:", mErr.connectionID)

				// TODO: remove connect subscriptions from Client
			}
		}
	}()

	return c
}

// Connect creates a new websocket client connecting to the provided endpoint.
func (c *Client) Connect(ctx context.Context) (err error) {
	return c.ConnectWithOptions(ctx, nil)
}

// ConnectWithOptions creates a new websocket client connecting to the provided
// endpoint with a http header if available The http header can be helpful to
// pass basic authentication params as prescribed
// ref https://github.com/gorilla/websocket/issues/209
func (c *Client) ConnectWithOptions(ctx context.Context, opt *Options) (err error) {
	dialer := &websocket.Dialer{
		Proxy:             http.ProxyFromEnvironment,
		HandshakeTimeout:  DefaultHandshakeTimeout,
		EnableCompression: true,
	}

	if opt != nil && opt.HandshakeTimeout > 0 {
		dialer.HandshakeTimeout = opt.HandshakeTimeout
	}

	var httpHeader http.Header = nil

	if opt != nil && opt.HttpHeader != nil && len(opt.HttpHeader) > 0 {
		httpHeader = opt.HttpHeader
	}

	var resp *http.Response

	connWS, resp, err := dialer.DialContext(ctx, c.rpcURL, httpHeader)
	if err != nil {
		if resp != nil {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Errorf("new ws client: dial: %w, status: %s", err, resp.Status)
			}
			err = fmt.Errorf("new ws client: dial: %w, status: %s, body: %q", err, resp.Status, string(body))
		} else {
			err = fmt.Errorf("new ws client: dial: %w", err)
		}

		return err
	}

	conn := &Connection{Conn: connWS, maxSubscriptions: c.maxSubscriptions, logger: c.logger}

	c.addConnection(conn)

	c.connCtx, c.connCtxCancel = context.WithCancel(context.Background())

	go func() {
		conn.SetReadDeadline(time.Now().Add(pongWait))

		conn.SetPongHandler(func(string) error {
			conn.SetReadDeadline(time.Now().Add(pongWait))

			subscriptions := make([]uint64, 0, len(conn.subscriptions))
			for sID := range conn.subscriptions {
				subscriptions = append(subscriptions, sID)
			}

			c.errorMessageCh <- MessageError{
				err:           err,
				connectionID:  conn.ID(),
				subscriptions: subscriptions,
			}

			return nil
		})

		ticker := time.NewTicker(pingPeriod)

		for {
			select {
			case <-c.connCtx.Done():
				return
			case <-ticker.C:
				conn.sendPing(c.errorMessageCh)
			}
		}
	}()

	go conn.receiveMessages(ctx, c.receivedMessagesCh, c.errorMessageCh)

	return nil
}

func (c *Client) addConnection(conn *Connection) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.connections[conn.ID()] = conn
}

func (c *Client) removeConnection(id uint64, subscriptionIDs []uint64) {
	c.lock.Lock()
	defer c.lock.Unlock()

	delete(c.connections, id)

	for _, sID := range subscriptionIDs {
		delete(c.subscriptionByID, sID)
	}
}

func (c *Client) Close() {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.connCtxCancel()
	for _, conn := range c.connections {
		go conn.Close()
	}
}

// GetUint64 returns the value retrieved by `Get`, cast to a uint64 if possible.
// If key data type do not match, it will return an error.
func getUint64(data []byte, keys ...string) (val uint64, err error) {
	v, t, _, e := jsonparser.Get(data, keys...)
	if e != nil {
		return 0, e
	}
	if t != jsonparser.Number {
		return 0, fmt.Errorf("Value is not a number: %s", string(v))
	}
	return strconv.ParseUint(string(v), 10, 64)
}

func getUint64WithOk(data []byte, path ...string) (uint64, bool) {
	val, err := getUint64(data, path...)
	if err == nil {
		return val, true
	}
	return 0, false
}

// func (c *Client) handleMessage(message []byte) {
// 	// when receiving message with id. the result will be a subscription number.
// 	// that number will be associated to all future message destine to this request

// 	requestID, ok := getUint64WithOk(message, "id")
// 	if ok {
// 		subID, _ := getUint64WithOk(message, "result")
// 		//c.handleNewSubscriptionMessage(requestID, subID)
// 		return
// 	}

// 	subID, _ := getUint64WithOk(message, "params", "subscription")
// 	c.handleSubscriptionMessage(subID, message)
// }

// func (c *Client) handleNewSubscriptionMessage(requestID, subID uint64) {
// 	c.lock.Lock()
// 	defer c.lock.Unlock()

// 	callBack, found := c.subscriptionByRequestID[requestID]
// 	if !found {
// 		c.logger.Error("cannot find websocket message handler for a new stream.... this should not happen",
// 			zap.Uint64("request_id", requestID),
// 			zap.Uint64("subscription_id", subID),
// 		)
// 		return
// 	}
// 	callBack.subID = subID
// 	c.subscriptionByWSSubID[subID] = callBack

// 	return
// }

// func (c *Client) handleSubscriptionMessage(subID uint64, message []byte) {

// 	c.lock.RLock()
// 	sub, found := c.subscriptionByWSSubID[subID]
// 	c.lock.RUnlock()
// 	if !found {
// 		c.logger.Error("unable to find subscription for ws message", zap.Uint64("subscription_id", subID))
// 		return
// 	}

// 	// Decode the message using the subscription-provided decoderFunc.
// 	result, err := sub.decoderFunc(message)
// 	if err != nil {
// 		fmt.Println("*****************************")
// 		c.closeSubscription(sub.req.ID, fmt.Errorf("unable to decode client response: %w", err))
// 		return
// 	}

// 	// this cannot be blocking or else
// 	// we  will no read any other message
// 	if len(sub.stream) >= cap(sub.stream) {
// 		c.logger.Error("closing ws client subscription... not consuming fast en ought",
// 			zap.Uint64("request_id", sub.req.ID),
// 		)
// 		c.closeSubscription(sub.req.ID, fmt.Errorf("reached channel max capacity %d", len(sub.stream)))
// 		return
// 	}

// 	sub.stream <- result
// 	return
// }

func (c *Client) subscribe(
	ctx context.Context,
	params []interface{},
	conf map[string]interface{},
	subscriptionMethod string,
	unsubscribeMethod string,
	decoderFunc decoderFunc,
) (err error) {
	if len(c.connections) == 0 {
		err := c.Connect(ctx)
		if err != nil {
			return fmt.Errorf(
				"clients on server: %d  ,error: %w,", len(c.connections), err)
		}
	}

	for connID, conn := range c.connections {
		if conn.maxSubscriptions == len(conn.subscriptions) {
			continue
		}

		reqID, err := conn.Subscribe(params, conf, subscriptionMethod, unsubscribeMethod, decoderFunc)
		if err != nil {
			return fmt.Errorf("failed to subscribe: %w", err)
		}

		c.logger.Info("subscribed to ws", connID, reqID)

		return nil
	}

	if len(c.connections) < c.maxConnections {
		err := c.Connect(ctx)
		if err != nil {
			return fmt.Errorf(
				"clients on server: %d  ,error: %w,", len(c.connections), err)
		}

		return c.subscribe(ctx, params, conf, subscriptionMethod, unsubscribeMethod, decoderFunc)
	}

	return nil
}

func decodeResponseFromReader(r io.Reader, reply interface{}) (err error) {
	var c *response
	if err := json.NewDecoder(r).Decode(&c); err != nil {
		return err
	}

	if c.Error != nil {
		jsonErr := &json2.Error{}
		if err := json.Unmarshal(*c.Error, jsonErr); err != nil {
			return &json2.Error{
				Code:    json2.E_SERVER,
				Message: string(*c.Error),
			}
		}
		return jsonErr
	}

	if c.Params == nil || (c.Params.Result == nil && c.Params.Error == nil) {
		return json2.ErrNullResult
	}

	if c.Params.Error != nil {
		var errMessage string
		json.Unmarshal(*c.Params.Error, &errMessage)

		return fmt.Errorf("rpc error: %s", errMessage)
	}

	return json.Unmarshal(*c.Params.Result, &reply)
}

func decodeResponseFromMessage(r []byte, reply interface{}) (err error) {
	var c *response
	if err := json.Unmarshal(r, &c); err != nil {
		return err
	}

	if c.Error != nil {
		jsonErr := &json2.Error{}
		if err := json.Unmarshal(*c.Error, jsonErr); err != nil {
			return &json2.Error{
				Code:    json2.E_SERVER,
				Message: string(*c.Error),
			}
		}
		return jsonErr
	}

	if c.Params == nil || (c.Params.Result == nil && c.Params.Error == nil) {
		return json2.ErrNullResult
	}

	if c.Params.Error != nil {
		var errMessage string
		json.Unmarshal(*c.Params.Error, &errMessage)

		return fmt.Errorf("rpc error: %s", errMessage)
	}

	return json.Unmarshal(*c.Params.Result, &reply)
}
