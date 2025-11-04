package entity

import "encoding/json"

type IndodaxResponse[T any] struct {
	Code    int  `json:"code"`
	Success bool `json:"success"`
	Data    T    `json:"data"`
}

type IndodaxOrderBook struct {
	Pair string                `json:"pair"`
	Bid  []IndodaxRawBookLevel `json:"bid"`
	Ask  []IndodaxRawBookLevel `json:"ask"`
}

type IndodaxRawBookLevel map[string]string

type IndodaxWsMessageParams struct {
	Channel string `json:"channel,omitempty"`
	Token   string `json:"token,omitempty"`
}

type IndodaxWsMessage struct {
	Method int                    `json:"method,omitempty"`
	Params IndodaxWsMessageParams `json:"params"`
	Id     int64                  `json:"id"`
}

type IndodaxWsResponseResultData struct {
	Data   json.RawMessage `json:"data"`
	Offset int64           `json:"offset"`
}

type IndodaxWsResponseResult struct {
	Channel string                      `json:"channel"`
	Data    IndodaxWsResponseResultData `json:"data"`
	Client  string                      `json:"client"`
	Version string                      `json:"version"`
	Expires bool                        `json:"expires"`
	Ttl     int64                       `json:"ttl"`
}

type IndodaxWsResponse struct {
	Result IndodaxWsResponseResult `json:"result"`
}
