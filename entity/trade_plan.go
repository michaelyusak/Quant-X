package entity

type TradePlan struct {
	Action     string  `json:"action"`
	Entry      float64 `json:"entry"`
	TakeProfit float64 `json:"take_profit"`
	StopLoss   float64 `json:"stop_loss"`
	SizeUnits  float64 `json:"size_units"`
}
