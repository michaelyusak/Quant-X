package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"quant-x/entity"
	"strings"
	"sync"
	"time"
)

type mockExchange struct {
	trades       []entity.TradeExecution
	activeTrades []entity.TradeExecution
	orderbook    *entity.Orderbook
	closeCh      chan bool

	mut sync.Mutex

	outFile *os.File
}

func NewMockExchange(orderbook *entity.Orderbook, tradesDirectory string) *mockExchange {
	outFile, err := os.OpenFile(
		fmt.Sprintf("%s/%s.log", tradesDirectory, time.Now().Format("20060102_150405")),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0644,
	)
	if err != nil {
		panic(err)
	}

	m := mockExchange{
		trades:       []entity.TradeExecution{},
		activeTrades: []entity.TradeExecution{},
		orderbook:    orderbook,

		closeCh: make(chan bool),

		outFile: outFile,
	}

	m.start()

	return &m
}

func (m *mockExchange) start() {
	if m.orderbook == nil || m.trades == nil {
		panic("mockExchange hasn't been initialised")
	}

	go func() {
	mainLoop:
		for {
			select {
			case <-m.closeCh:
				m.writeTrades()
				break mainLoop
			default:
				if len(m.activeTrades) > 0 {
					m.executeTrades(m.orderbook)
					time.Sleep(10 * time.Millisecond)
					continue
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()

	log.Printf("[%s] mockExchange started", time.Now().String())
}

func (m *mockExchange) writeTrades() {
	outData := struct {
		Active  []entity.TradeExecution
		History []entity.TradeExecution
	}{
		Active:  m.activeTrades,
		History: m.trades,
	}

	b, err := json.Marshal(outData)
	if err != nil {
		panic(err)
	}

	_, err = m.outFile.Write(b)
	if err != nil {
		panic(err)
	}

	_, err = m.outFile.Write([]byte("\n"))
	if err != nil {
		panic(err)
	}
}

func (m *mockExchange) executeTrades(orderbook *entity.Orderbook) {
	for i := range m.activeTrades {
		trade := &m.trades[i]

		if trade.Filled || trade.Status != "" {
			continue
		}

		var side []entity.BookLevel

		switch trade.Action {
		case "buy":
			side = orderbook.Ask
		case "sell":
			side = orderbook.Bid
		}

		for _, level := range side {
			canMatch := false

			if trade.Price == 0 {
				canMatch = true
			} else {
				switch trade.Action {
				case "buy":
					canMatch = level.Price <= trade.Price
				case "sell":
					canMatch = level.Price >= trade.Price
				}
			}

			if !canMatch {
				continue
			}

			partial := math.Min(trade.Remaining, level.Qty)
			trade.Remaining -= partial

			orderFill := entity.OrderFill{
				Price: level.Price,
				Qty:   partial,
			}

			trade.Fills = append(trade.Fills, orderFill)

			// TODO: logic to update orderbook. Perhaps need to listen for market activity, not only the orderbook

			trade.Update()
		}
	}

	newActiveTrades := []entity.TradeExecution{}

	for i := range m.activeTrades {
		if m.activeTrades[i].Filled {
			m.trades = append(m.trades, m.activeTrades[i])
			continue
		}

		newActiveTrades = append(newActiveTrades, m.activeTrades[i])
	}

	m.activeTrades = newActiveTrades
}

func (m *mockExchange) CreateLimitOrder(ctx context.Context, price, amount float64, action, coin, base string) (string, error) {
	clientOrderId := fmt.Sprintf("%s%s-%v", coin, base, time.Now().UnixMilli())

	m.mut.Lock()
	m.trades = append(m.activeTrades, entity.TradeExecution{
		ClientOrderID: clientOrderId,
		Action:        strings.ToLower(action),
		Price:         price,
		Remaining:     amount,
		Base:          base,
		Asset:         coin,
	})
	defer m.mut.Unlock()

	return clientOrderId, nil
}

func (m *mockExchange) CreateMarketOrder(ctx context.Context, amount float64, action, coin, base string) (string, error) {
	clientOrderId := fmt.Sprintf("%s%s-%v", coin, base, time.Now().UnixMilli())

	m.mut.Lock()
	m.trades = append(m.activeTrades, entity.TradeExecution{
		ClientOrderID: clientOrderId,
		Action:        strings.ToLower(action),
		Remaining:     amount,
		Base:          base,
		Asset:         coin,
	})
	defer m.mut.Unlock()

	return clientOrderId, nil
}

func (m *mockExchange) GetOrder(ctx context.Context, clientOrderId string) (map[string]any, error) {
	trades := append(m.activeTrades, m.trades...)

	for i := range trades {
		if trades[i].ClientOrderID == clientOrderId {
			b, err := json.Marshal(trades[i])
			if err != nil {
				return nil, err
			}

			var dataMap map[string]any
			err = json.Unmarshal(b, &dataMap)
			if err != nil {
				return nil, err
			}

			var remainingKey string

			switch trades[i].Action {
			case "buy":
				remainingKey = "remain_rp"
			case "sell":
				remainingKey = fmt.Sprintf("remain_%s", trades[i].Asset)
			}

			dataMap[remainingKey] = trades[i].Remaining

			return dataMap, nil
		}
	}

	return nil, nil
}

func (m *mockExchange) CancelOrder(ctx context.Context, clientOrderId string) error {
	m.mut.Lock()
	defer m.mut.Unlock()

	found := -1

	for i := range m.activeTrades {
		if m.activeTrades[i].ClientOrderID == clientOrderId {
			found = i
			break
		}
	}

	if found == -1 {
		return fmt.Errorf("order %s not found", clientOrderId)
	}

	canceledTrade := m.activeTrades[found]
	canceledTrade.Status = "canceled"

	m.activeTrades = append(m.activeTrades[:found], m.activeTrades[found+1:]...)

	m.trades = append(m.trades, canceledTrade)

	return nil
}
