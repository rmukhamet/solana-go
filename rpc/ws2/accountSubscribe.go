// Copyright 2021 github.com/gagliardetto
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

	"github.com/rmukhamet/solana-go"
	"github.com/rmukhamet/solana-go/rpc"
)

type AccountResult struct {
	Context struct {
		Slot uint64
	} `json:"context"`
	Value struct {
		rpc.Account
	} `json:"value"`
}

// AccountSubscribe subscribes to an account to receive notifications
// when the lamports or data for a given account public key changes.
func (c *Client) AccountSubscribe(
	ctx context.Context,
	account string,
	commitment rpc.CommitmentType,
) error {
	return c.AccountSubscribeWithOpts(
		ctx,
		account,
		commitment,
		"",
	)
}

// AccountSubscribe subscribes to an account to receive notifications
// when the lamports or data for a given account public key changes.
func (c *Client) AccountSubscribeWithOpts(
	ctx context.Context,
	account string,
	commitment rpc.CommitmentType,
	encoding solana.EncodingType,
) error {

	params := []interface{}{account}
	conf := map[string]interface{}{
		"encoding": "base64",
	}
	if commitment != "" {
		conf["commitment"] = commitment
	}
	if encoding != "" {
		conf["encoding"] = encoding
	}

	err := c.subscribe(
		ctx,
		params,
		conf,
		"accountSubscribe",
		"accountUnsubscribe",
		func(msg []byte) (interface{}, error) {
			var res AccountResult
			err := decodeResponseFromMessage(msg, &res)
			return &res, err
		},
	)
	if err != nil {
		return err
	}
	return nil
}
