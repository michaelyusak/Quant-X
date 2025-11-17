package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"quant-x/common"
	"quant-x/entity"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/gorilla/websocket"
)

type indodax struct {
	identity string

	apiKey    string
	secretKey string
	baseUrl   string

	publicWsToken     string
	wsScheme          string
	wsHost            string
	wsPath            string
	orderBookChanName string
	ticketChanName    string

	updateOrderBookChan chan entity.Orderbook
	updateTickerChan    chan entity.Ticker

	client       *resty.Client
	tradeTimeout time.Duration
}

func NewIndodaxAdapter(
	identity,
	apiKey,
	secretKey,
	baseUrl,
	wsScheme,
	wsHost,
	wsPath,
	publicWsToken,
	orderBookChanName,
	tickerChanName string,
	tradeTimeout time.Duration,
	updateOrderBookChan chan entity.Orderbook,
	updateTickerChan chan entity.Ticker,
) *indodax {
	return &indodax{
		identity:  identity,
		apiKey:    apiKey,
		secretKey: secretKey,
		baseUrl:   baseUrl,

		wsScheme:          wsScheme,
		wsHost:            wsHost,
		wsPath:            wsPath,
		publicWsToken:     publicWsToken,
		orderBookChanName: orderBookChanName,
		ticketChanName:    tickerChanName,

		updateOrderBookChan: updateOrderBookChan,
		updateTickerChan:    updateTickerChan,

		client:       resty.New(),
		tradeTimeout: tradeTimeout,
	}
}

func (i *indodax) convertOrderbook(indodaxOrderbook entity.IndodaxOrderBook, coin string) entity.Orderbook {
	var bid, ask []entity.BookLevel

	priceField := "price"
	qtyField := coin + "_volume"

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		for _, b := range indodaxOrderbook.Bid {
			priceFloat, _ := strconv.ParseFloat(b[priceField], 64)
			qtyFloat, _ := strconv.ParseFloat(b[qtyField], 64)

			bid = append(bid, entity.BookLevel{
				Price: priceFloat,
				Qty:   qtyFloat,
			})
		}

		sort.Slice(bid, func(i, j int) bool {
			return bid[i].Price < bid[j].Price
		})
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		for _, a := range indodaxOrderbook.Ask {
			priceFloat, _ := strconv.ParseFloat(a[priceField], 64)
			qtyFloat, _ := strconv.ParseFloat(a[qtyField], 64)

			ask = append(ask, entity.BookLevel{
				Price: priceFloat,
				Qty:   qtyFloat,
			})
		}

		sort.Slice(ask, func(i, j int) bool {
			return ask[i].Price < ask[j].Price
		})
	}()

	wg.Wait()

	return entity.Orderbook{
		Pair: indodaxOrderbook.Pair,
		Bid:  bid,
		Ask:  ask,
	}
}

func (i *indodax) convertTicker(pair string, tickerData []any) entity.Ticker {
	volFloat, _ := strconv.ParseFloat(tickerData[3].(string), 64)

	return entity.Ticker{
		Pair:         pair,
		TradedPrice:  tickerData[2].(float64),
		TradedVolume: volFloat,
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

	return i.convertOrderbook(indodaxOrderbookResponse.Data, coin), nil
}

type marketDataListenerEvent struct {
	Close bool
	Error error
	Info  string
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

	orderbookChanName := i.orderBookChanName + coin + base
	tickerChanName := i.ticketChanName + coin + base

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

				quit <- marketDataListenerEvent{
					Close: false, // restart
					Error: err,
					Info:  "[adapters][exchanges][indodax][ListenMarketData][c.ReadMessage()]",
				}
				break
			}

			switch messageType {
			case websocket.TextMessage:
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
				case orderbookChanName:
					var indodaxOrderbook entity.IndodaxOrderBook

					err = json.Unmarshal(indodaxWsResponse.Result.Data.Data, &indodaxOrderbook)
					if err != nil {
						quit <- marketDataListenerEvent{
							Close: false,
							Error: err,
							Info:  "[adapters][exchanges][indodax][ListenMarketData][json.Unmarshal] Unmarshal orderbook",
						}
					}

					i.updateOrderBookChan <- i.convertOrderbook(indodaxOrderbook, coin)

				case tickerChanName:
					var indodaxTickerData [][]any

					err = json.Unmarshal(indodaxWsResponse.Result.Data.Data, &indodaxTickerData)
					if err != nil {
						quit <- marketDataListenerEvent{
							Close: false,
							Error: err,
							Info:  "[adapters][exchanges][indodax][ListenMarketData][json.Unmarshal] Unmarshal ticker",
						}
					}

					seen := map[string]int{}

					for _, tic := range indodaxTickerData {
						ts := int64(tic[0].(float64))
						seq := int64(tic[1].(float64))
						key := fmt.Sprintf("%d-%d", ts, seq)
						if _, ok := seen[key]; ok {
							continue
						}

						i.updateTickerChan <- i.convertTicker(coin+base, tic)

						seen[key] = 1
					}
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

	subscribeOrderbookMsg := entity.IndodaxWsMessage{
		Method: 1,
		Params: entity.IndodaxWsMessageParams{
			Channel: orderbookChanName,
		},
		Id: clientId,
	}
	c.WriteJSON(subscribeOrderbookMsg)

	subscribeTickerMsg := entity.IndodaxWsMessage{
		Method: 1,
		Params: entity.IndodaxWsMessageParams{
			Channel: tickerChanName,
		},
		Id: clientId,
	}
	c.WriteJSON(subscribeTickerMsg)

	e := <-quit

	if e.Close {
		return fmt.Errorf("[%s] %s CLOSING Indodax market data listener. Error: %w", time.Now().String(), e.Info, e.Error)
	}

	fmt.Printf("[%s] %s RESTARTING Indodax market data listener. Error: %v\n", time.Now().String(), e.Info, e.Error)

	return i.ListenMarketData(ctx, coin, base)
}

func (i *indodax) CreateLimitOrder(ctx context.Context, price, amount float64, action, coin, base string) (string, error) {
	pair := fmt.Sprintf("%s_%s", coin, base)

	var qty string
	action = strings.ToLower(action)

	switch action {
	case "buy":
		qty = fmt.Sprintf("%s=%v", base, amount)
	case "sell":
		qty = fmt.Sprintf("%s=%v", coin, amount)
	}

	clientOrderId := fmt.Sprintf("%s-%s%s-%v", i.identity, coin, base, time.Now().UnixMilli())

	reqBodyStr := fmt.Sprintf("method=trade&nonce=%v&pair=%s&type=%s&price=%v&order_type=limit&client_order_id=%s&%s",
		common.GetNonce(),
		pair,
		action,
		price,
		clientOrderId,
		qty,
	)

	reqBody := strings.NewReader(reqBodyStr)

	sign := common.HmacHash(reqBodyStr, i.secretKey)

	headers := map[string]string{
		"Key":          i.apiKey,
		"Sign":         sign,
		"Content-Type": "application/x-www-form-urlencoded",
	}

	var res entity.TapiResponse[any]

	ctx, cancel := context.WithTimeout(ctx, i.tradeTimeout)
	defer cancel()

	r, err := i.client.R().
		SetBody(reqBody).
		SetHeaders(headers).
		SetResult(&res).
		SetError(&res).
		Post(fmt.Sprintf("%s/tapi/", i.baseUrl))
	if err != nil {
		return "", fmt.Errorf("[adapters][exchanges][indodax][CreateOrder][Post] Error: %w", err)
	}
	if r.StatusCode() >= http.StatusBadRequest {
		return "", fmt.Errorf("[adapters][exchanges][indodax][CreateOrder][StatusNotOk] [code: %v][body: %s]", r.StatusCode(), string(r.Body()))
	}
	if res.Success != 1 {
		return "", fmt.Errorf("[adapters][exchanges][indodax][CreateOrder][StatusNotOk] [code: %v][res: %+v]", r.StatusCode(), res)
	}

	return clientOrderId, nil
}

func (i *indodax) CreateMarketOrder(ctx context.Context, amount float64, action, coin, base string) (string, error) {
	pair := fmt.Sprintf("%s_%s", coin, base)

	var qty string
	action = strings.ToLower(action)

	switch action {
	case "buy":
		qty = fmt.Sprintf("%s=%v", base, amount)
	case "sell":
		qty = fmt.Sprintf("%s=%v", coin, amount)
	}

	clientOrderId := fmt.Sprintf("%s-%s%s-%v", i.identity, coin, base, time.Now().UnixMilli())

	reqBodyStr := fmt.Sprintf("method=trade&nonce=%v&pair=%s&type=%s&order_type=market&client_order_id=%s&%s",
		common.GetNonce(),
		pair,
		action,
		clientOrderId,
		qty,
	)

	reqBody := strings.NewReader(reqBodyStr)

	sign := common.HmacHash(reqBodyStr, i.secretKey)

	headers := map[string]string{
		"Key":          i.apiKey,
		"Sign":         sign,
		"Content-Type": "application/x-www-form-urlencoded",
	}

	var res entity.TapiResponse[any]

	ctx, cancel := context.WithTimeout(ctx, i.tradeTimeout)
	defer cancel()

	r, err := i.client.R().
		SetBody(reqBody).
		SetHeaders(headers).
		SetResult(&res).
		SetError(&res).
		Post(fmt.Sprintf("%s/tapi/", i.baseUrl))
	if err != nil {
		return "", fmt.Errorf("[adapters][exchanges][indodax][CreateOrder][Post] Error: %w", err)
	}
	if r.StatusCode() >= http.StatusBadRequest {
		return "", fmt.Errorf("[adapters][exchanges][indodax][CreateOrder][StatusNotOk] [code: %v][body: %s]", r.StatusCode(), string(r.Body()))
	}
	if res.Success != 1 {
		return "", fmt.Errorf("[adapters][exchanges][indodax][CreateOrder][StatusNotOk] [code: %v][res: %+v]", r.StatusCode(), res)
	}

	return clientOrderId, nil
}

func (i *indodax) GetOrder(ctx context.Context, clientOrderId string) (map[string]any, error) {
	reqBodyStr := fmt.Sprintf("method=getOrderByClientOrderId&nonce=%v&client_order_id=%s",
		common.GetNonce(),
		clientOrderId,
	)

	reqBody := strings.NewReader(reqBodyStr)

	sign := common.HmacHash(reqBodyStr, i.secretKey)

	headers := map[string]string{
		"Key":          i.apiKey,
		"Sign":         sign,
		"Content-Type": "application/x-www-form-urlencoded",
	}

	var res entity.TapiResponse[map[string]any]

	ctx, cancel := context.WithTimeout(ctx, i.tradeTimeout)
	defer cancel()

	r, err := i.client.R().
		SetBody(reqBody).
		SetHeaders(headers).
		SetResult(&res).
		SetError(&res).
		Post(fmt.Sprintf("%s/tapi/", i.baseUrl))
	if err != nil {
		return nil, fmt.Errorf("[adapters][exchanges][indodax][GetOrder][Post] Error: %w", err)
	}
	if r.StatusCode() >= http.StatusBadRequest {
		return nil, fmt.Errorf("[adapters][exchanges][indodax][GetOrder][StatusNotOk] [code: %v][body: %s]", r.StatusCode(), string(r.Body()))
	}
	if res.Success != 1 {
		return nil, fmt.Errorf("[adapters][exchanges][indodax][GetOrder][StatusNotOk] [code: %v][res: %+v]", r.StatusCode(), res)
	}

	order, ok := res.Return["order"]
	if !ok {
		return nil, fmt.Errorf("[adapters][exchanges][indodax][GetOrder][ReturnEmpty] [code: %v][res: %+v]", r.StatusCode(), res)
	}

	orderMap, ok := order.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("[adapters][exchanges][indodax][GetOrder][InvalidOrder] [code: %v][res: %+v]", r.StatusCode(), order)
	}

	return orderMap, nil
}

func (i *indodax) CancelOrder(ctx context.Context, clientOrderId string) error {
	reqBodyStr := fmt.Sprintf("method=cancelByClientOrderId&nonce=%v&client_order_id=%s",
		common.GetNonce(),
		clientOrderId,
	)

	reqBody := strings.NewReader(reqBodyStr)

	sign := common.HmacHash(reqBodyStr, i.secretKey)

	headers := map[string]string{
		"Key":          i.apiKey,
		"Sign":         sign,
		"Content-Type": "application/x-www-form-urlencoded",
	}

	var res entity.TapiResponse[any]

	ctx, cancel := context.WithTimeout(ctx, i.tradeTimeout)
	defer cancel()

	r, err := i.client.R().
		SetBody(reqBody).
		SetHeaders(headers).
		SetResult(&res).
		SetError(&res).
		Post(fmt.Sprintf("%s/tapi/", i.baseUrl))
	if err != nil {
		return fmt.Errorf("[adapters][exchanges][indodax][CancelOrder][Post] Error: %w", err)
	}
	if r.StatusCode() >= http.StatusBadRequest {
		return fmt.Errorf("[adapters][exchanges][indodax][CancelOrder][StatusNotOk] [code: %v][body: %s]", r.StatusCode(), string(r.Body()))
	}
	if res.Success != 1 {
		return fmt.Errorf("[adapters][exchanges][indodax][CancelOrder][StatusNotOk] [code: %v][res: %+v]", r.StatusCode(), res)
	}

	return nil
}
