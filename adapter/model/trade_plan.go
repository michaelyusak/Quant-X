package model

import (
	"context"
	"fmt"
	"log"
	"quant-x/adapter/exchange"
	"quant-x/entity"
	"time"
)

type tradePlanManager struct {
	ctx context.Context

	buyOrderId          string
	sellTpOrderId       string
	sellSlOrderId       string
	sellReactiveOrderId string

	coin string
	base string

	plan          entity.TradePlan
	exchange      exchange.ExchangeAdapter
	tickerCh      chan entity.Ticker
	emergencySlCh chan bool
}

func NewTradePlanManager(plan entity.TradePlan, exchange exchange.ExchangeAdapter, tickerCh chan entity.Ticker, coin, base string, emergencySlCh chan bool) *tradePlanManager {
	return &tradePlanManager{
		ctx: context.Background(),

		coin: coin,
		base: base,

		plan:          plan,
		exchange:      exchange,
		tickerCh:      tickerCh,
		emergencySlCh: emergencySlCh,
	}
}

// Execute runs the trade plan:
//  1. Stops if context is done or timeout occurs.
//  2. Executes TP or SL based on ticker updates.
//  3. Handles emergency sell signals.
func (p *tradePlanManager) Execute() {
	buyOrderId, err := p.exchange.CreateLimitOrder(p.ctx, p.plan.Entry, p.plan.SizeUnits*p.plan.Entry, "buy", p.coin, p.base)
	if err != nil {
		log.Printf("[%s][tradePlanManager][Execute][exchange.CreateLimitOrder][Buy] Error: %v [orderId: %s]", time.Now().String(), err, buyOrderId)
		return
	}
	p.buyOrderId = buyOrderId

	for {
		buyOrder, err := p.exchange.GetOrder(p.ctx, buyOrderId)
		if err != nil {
			log.Printf("[%s][tradePlanManager][Execute][exchange.GetOrder][Buy] Error: %v [orderId: %s]", time.Now().String(), err, buyOrderId)
			return
		}

		fmt.Println(buyOrder["remain_rp"])

		if buyOrder["remain_rp"] == 0 {
			break
		}

		time.Sleep(500 * time.Millisecond)
	}

	for {
		select {
		case <-p.ctx.Done():
			return

		case tic := <-p.tickerCh:
			if tic.TradedPrice >= p.plan.TakeProfit {
				sellTpOrderId, err := p.exchange.CreateMarketOrder(p.ctx, p.plan.SizeUnits, "sell", p.coin, p.base)
				if err != nil {
					log.Printf("[%s][tradePlanManager][Execute][exchange.CreateMarketOrder][TP] Error: %v [orderId: %s]", time.Now().String(), err, sellTpOrderId)
					return
				}
				p.sellTpOrderId = sellTpOrderId
				return
			}

			if tic.TradedPrice <= p.plan.StopLoss {
				sellSlOrderId, err := p.exchange.CreateMarketOrder(p.ctx, p.plan.SizeUnits, "sell", p.coin, p.base)
				if err != nil {
					log.Printf("[%s][tradePlanManager][Execute][exchange.CreateMarketOrder][SL] Error: %v [orderId: %s]", time.Now().String(), err, sellSlOrderId)
					return
				}
				p.sellSlOrderId = sellSlOrderId
				return
			}

		case <-p.emergencySlCh:
			log.Printf("[%s][tradePlanManager][Execute][EmergencySl]", time.Now().String())
			emergencySlOrderId, err := p.exchange.CreateMarketOrder(p.ctx, p.plan.SizeUnits, "sell", p.coin, p.base)
			if err != nil {
				log.Printf("[%s][tradePlanManager][Execute][EmergencySl][exchange.CreateMarketOrder] Error: %v", time.Now().String(), err)
				return
			}
			log.Printf("[%s][tradePlanManager][Execute][EmergencySl] Done [orderId: %s]", time.Now().String(), emergencySlOrderId)
			return

		case <-time.After(6 * time.Hour):
			log.Printf("[%s][tradePlanManager][Execute][Timeout] Exceeds timeout of 6 hours", time.Now().String())
			return
		}
	}
}
