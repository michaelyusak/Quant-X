package entity

type Orderbook struct {
	Pair string      `json:"pair"`
	Bid  []BookLevel `json:"bid"`
	Ask  []BookLevel `json:"ask"`
}

type BookLevel struct {
	Price float64 `json:"price"`
	Qty   float64 `json:"qty"`
}

type Ticker struct {
	Pair         string  `json:"pair"`
	TradedPrice  float64 `json:"traded_price"`
	TradedVolume float64 `json:"traded_volume"`
}

type OrderFill struct {
	Price float64
	Qty   float64
}

type TradeExecution struct {
	ClientOrderID string
	Base          string
	Asset         string
	Action        string      // buy or sell
	Price         float64     // limit price of the order
	Status        string      // canceled or open. "canceled" if canceled, "done" if filled, "" if open
	Fills         []OrderFill // chronological fills
	TotalFilled   float64     // sum of filled qty
	AvgPrice      float64     // volume-weighted average price
	SlippagePct   float64     // percent (see note below)
	Remaining     float64     // amount left unfilled
	Filled        bool        // true if fully filled
}

func (t *TradeExecution) Update() {
	t.TotalFilled = 0
	sumPrice := float64(0)

	for _, f := range t.Fills {
		t.TotalFilled += f.Qty
		sumPrice += f.Price
	}

	t.AvgPrice = sumPrice / float64(len(t.Fills))

	switch t.Action {
	case "buy":
		t.SlippagePct = 100 * (t.AvgPrice - t.Price) / t.Price
	case "sell":
		t.SlippagePct = 100 * (t.Price - t.AvgPrice) / t.Price
	}

	if t.Remaining == 0 {
		t.Filled = true
		t.Status = "done"
	}
}
