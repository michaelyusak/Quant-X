package entity

type Orderbook struct {
	Pair string      `json:"pair"`
	Bid  []BookLevel `json:"bid"`
	Ask  []BookLevel `json:"ask"`
}

type BookLevel struct {
	Price string `json:"price"`
	Qty   string `json:"qty"`
	Cum   string `json:"cum"`
}
