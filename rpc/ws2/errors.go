package ws2

import "fmt"

var ErrReachedMaxSubscriptionsPerClientLimit = fmt.Errorf("reached max subscriptions per client limit")
var ErrReachedMaxSubscriptionsLimit = fmt.Errorf("reached max subscriptions limit")
var ErrReachedMaxConnectionsLimit = fmt.Errorf("reached max connections limit")
