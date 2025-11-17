package exchange

import "context"

type ExchangeAdapter interface {
	CreateLimitOrder(ctx context.Context, price, amount float64, action, coin, base string) (string, error)
	CreateMarketOrder(ctx context.Context, amount float64, action, coin, base string) (string, error)
	GetOrder(ctx context.Context, clientOrderId string) (map[string]any, error)
	CancelOrder(ctx context.Context, clientOrderId string) error
}
