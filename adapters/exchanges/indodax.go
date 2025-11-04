package exchanges

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"quant-x/entity"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/gorilla/websocket"
)

type indodax struct {
	apiKey    string
	secretKey string
	baseUrl   string

	publicWsToken     string
	wsScheme          string
	wsHost            string
	wsPath            string
	orderBookChanName string

	updateOrderBookChan chan entity.Orderbook
	// tradeActivityCh

	client *resty.Client
}

func NewIndodaxAdapter(apiKey, secretKey, baseUrl, wsScheme, wsHost, wsPath, publicWsToken, orderBookChanName string, updateOrderBookChan chan entity.Orderbook) *indodax {
	return &indodax{
		apiKey:    apiKey,
		secretKey: secretKey,
		baseUrl:   baseUrl,

		wsScheme:          wsScheme,
		wsHost:            wsHost,
		wsPath:            wsPath,
		publicWsToken:     publicWsToken,
		orderBookChanName: orderBookChanName,

		updateOrderBookChan: updateOrderBookChan,

		client: resty.New(),
	}
}

func (i *indodax) convertOrderbook(indodaxOrderbook entity.IndodaxOrderBook, coin, base string) entity.Orderbook {
	var bid, ask []entity.BookLevel

	var (
		priceField = "price"
		qtyField   = coin + "_volume"
		cumField   = base + "_volume"
	)

	for _, b := range indodaxOrderbook.Bid {
		bid = append(bid, entity.BookLevel{
			Price: b[priceField],
			Qty:   b[qtyField],
			Cum:   b[cumField],
		})
	}

	for _, a := range indodaxOrderbook.Ask {
		ask = append(ask, entity.BookLevel{
			Price: a[priceField],
			Qty:   a[qtyField],
			Cum:   a[cumField],
		})
	}

	return entity.Orderbook{
		Pair: indodaxOrderbook.Pair,
		Bid:  bid,
		Ask:  ask,
	}
}

func (i *indodax) GetOrderbook(ctx context.Context, coin, base string) (entity.Orderbook, error) {
	var indodaxOrderbookResponse entity.IndodaxResponse[entity.IndodaxOrderBook]

	resp, err := i.client.R().
		SetResult(&indodaxOrderbookResponse).
		SetError(&indodaxOrderbookResponse).
		Get(i.baseUrl + "/api/orderbook/" + coin + base)
	if err != nil {
		return entity.Orderbook{}, fmt.Errorf("[adapters][exchanges][indodax][GetOrderbook][Get] Error: %w", err)
	}

	if resp.IsError() {
		return entity.Orderbook{}, fmt.Errorf("[adapters][exchanges][indodax][GetOrderbook][resp.IsError()] Error: %w [status_code: %v][raw_body: %s]", err, resp.StatusCode(), string(resp.Body()))
	}

	return i.convertOrderbook(indodaxOrderbookResponse.Data, coin, base), nil
}

type marketDataListenerEvent struct {
	Close bool
	Error error
	Info  string
}

func (i *indodax) subscribeChannels(wsConn *websocket.Conn, clientId int64, coin, base string) {
	subscribeOrderbookMsg := entity.IndodaxWsMessage{
		Method: 1,
		Params: entity.IndodaxWsMessageParams{
			Channel: i.orderBookChanName + coin + base,
		},
		Id: clientId,
	}

	wsConn.WriteJSON(subscribeOrderbookMsg)
}

func (i *indodax) ListenMarketData(ctx context.Context, coin, base string) error {
	u := url.URL{Scheme: i.wsScheme, Host: i.wsHost, Path: i.wsPath}

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("[adapters][exchanges][indodax][ListenMarketData][websocket.DefaultDialer.Dial] Error: %w", err)
	}
	defer c.Close()

	quit := make(chan marketDataListenerEvent, 10)
	defer close(quit)

	clientId := time.Now().Unix()

	var mut sync.Mutex
	var subscribed bool

	go func() {
	loop:
		for {
			messageType, data, err := c.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err) {
					quit <- marketDataListenerEvent{
						Close: true,
						Error: err,
						Info:  "[adapters][exchanges][indodax][ListenMarketData][c.ReadMessage()]",
					}
					break
				}

				continue
			}

			switch messageType {
			case websocket.TextMessage:
				if !subscribed {
					i.subscribeChannels(c, clientId, coin, base)

					mut.Lock()
					subscribed = true
					mut.Unlock()

					continue
				}

				var indodaxWsResponse entity.IndodaxWsResponse

				err := json.Unmarshal(data, &indodaxWsResponse)
				if err != nil {
					quit <- marketDataListenerEvent{
						Close: false,
						Error: err,
						Info:  fmt.Sprintf("[adapters][exchanges][indodax][ListenMarketData][json.Unmarshal] Unmarshal Response [raw: %s]", string(data)),
					}
					break loop
				}

				switch indodaxWsResponse.Result.Channel {
				case i.orderBookChanName + coin + base:
					var indodaxOrderbook entity.IndodaxOrderBook

					err = json.Unmarshal(indodaxWsResponse.Result.Data.Data, &indodaxOrderbook)
					if err != nil {
						quit <- marketDataListenerEvent{
							Close: false,
							Error: err,
							Info:  "[adapters][exchanges][indodax][ListenMarketData][json.Unmarshal] Unmarshal orderbook",
						}
					}

					i.updateOrderBookChan <- i.convertOrderbook(indodaxOrderbook, coin, base)
				}
			}
		}
	}()

	authMsg := entity.IndodaxWsMessage{
		Params: entity.IndodaxWsMessageParams{
			Token: i.publicWsToken,
		},
		Id: clientId,
	}

	c.WriteJSON(authMsg)

	e := <-quit

	if e.Close {
		return fmt.Errorf("%s Error: %w", e.Info, e.Error)
	}

	fmt.Printf("[%s] %s Error: %v\n", time.Now().String(), e.Info, e.Error)

	return i.ListenMarketData(ctx, coin, base)
}
