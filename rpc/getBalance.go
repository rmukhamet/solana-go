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
package rpc

import (
	"context"
)

// GetBalance returns the balance of the account of provided publicKey.
func (cl *Client) GetBalance(
	ctx context.Context,
	publicKey string, // Pubkey of account to query, as base-58 encoded string
	commitment CommitmentType,
) (out *GetBalanceResult, err error) {
	params := []interface{}{publicKey}
	if commitment != "" {
		params = append(params, string(commitment))
	}

	err = cl.rpcClient.CallFor(&out, "getBalance", params)

	return
}
