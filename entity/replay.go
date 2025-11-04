package entity

import "time"

type UpdateReplaySpeedReq struct {
	ReplaySpeed float64 `json:"replay_speed"`
}

type OrderbookReplayEvent struct {
	Orderbook Orderbook     `json:"orderbook"`
	Diff      time.Duration `json:"diff"`
}
