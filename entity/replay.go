package entity

import "time"

type marketDataEventType string

const (
	EventTypeOrderbook = marketDataEventType("orderbook")
	EventTypeTicker    = marketDataEventType("ticker")
)

type UpdateReplaySpeedReq struct {
	ReplaySpeed float64 `json:"replay_speed"`
}

type ReplayMessage struct {
	Type marketDataEventType `json:"type"`
	Data any                 `json:"data"`
}

type MarketDataReplayEvent struct {
	Msg  ReplayMessage `json:"msg"`
	Diff time.Duration `json:"diff"`
}
