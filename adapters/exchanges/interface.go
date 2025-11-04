package exchanges

import (
	"context"
	"quant-x/entity"
)

type ExchangeAdapter interface {
	GetOrderbook(ctx context.Context, coin, base string) (entity.Orderbook, error)
	// WS Market Event
	// REST Order
	// REST Cancel
}
