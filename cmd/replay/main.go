package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"quant-x/adapter/exchange"
	"quant-x/adapter/model"
	"quant-x/config"
	"quant-x/entity"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var (
	conf config.AppConfig

	orderBook   entity.Orderbook
	tickers     []entity.Ticker
	replaySpeed float64

	mut sync.Mutex

	marketDataListeners map[int]chan entity.MarketDataReplayEvent

	recording *os.File

	multiWriter io.Writer
)

func recordEvent(msg entity.MarketDataReplayEvent) {
	if recording != nil {
		data, _ := json.Marshal(msg)
		recording.Write(data)
		recording.Write([]byte("\n"))
	}
}

func main() {
	err := config.Init(&conf)
	if err != nil {
		panic(err)
	}

	now := time.Now()

	logFile, err := os.OpenFile(
		fmt.Sprintf("%s/%s.log", conf.LogDir, now.Format("20060102_150405")),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0644,
	)
	if err != nil {
		panic(err)
	}

	multiWriter = io.MultiWriter(os.Stdout, logFile)

	log.SetOutput(multiWriter)

	if conf.Replay.Record {
		recording, err = os.OpenFile(
			fmt.Sprintf("%s/%s.log", conf.Replay.RecordingDir, now.Format("20060102_150405")),
			os.O_CREATE|os.O_WRONLY|os.O_APPEND,
			0644,
		)
		if err != nil {
			panic(err)
		}
	}

	replaySpeed = conf.Replay.ReplaySpeed
	marketDataListeners = map[int]chan entity.MarketDataReplayEvent{}

	chUpdateOrderbook := make(chan entity.Orderbook)
	defer close(chUpdateOrderbook)

	chUpdateTicker := make(chan entity.Ticker)
	defer close(chUpdateTicker)

	indodax := exchange.NewIndodaxAdapter(
		conf.Identity,
		conf.Exchange.Indodax.ApiKey,
		conf.Exchange.Indodax.SecretKey,
		conf.Exchange.Indodax.BaseUrl,
		conf.Exchange.Indodax.WsScheme,
		conf.Exchange.Indodax.WsHost,
		conf.Exchange.Indodax.WsPath,
		conf.Exchange.Indodax.PublicWsToken,
		conf.Exchange.Indodax.OrderbookWsChannel,
		conf.Exchange.Indodax.TickerWsChannel,
		time.Duration(conf.Exchange.Indodax.Timeout),
		chUpdateOrderbook,
		chUpdateTicker,
	)

	ctx := context.Background()

	ob, err := indodax.GetOrderbook(ctx, conf.Trade.Coin, conf.Trade.Base)
	if err != nil {
		panic(err)
	}

	orderBook = ob

	go func() {
		var last time.Time
		var diff time.Duration

		for {
			select {
			case ob := <-chUpdateOrderbook:
				if !last.IsZero() {
					diff = time.Since(last)
				}
				last = time.Now()

				event := entity.MarketDataReplayEvent{
					Msg: entity.ReplayMessage{
						Type: entity.EventTypeOrderbook,
						Data: ob,
					},
					Diff: diff,
				}

				go func(e entity.MarketDataReplayEvent) {
					if len(marketDataListeners) > 0 {
						for _, ch := range marketDataListeners {
							ch <- e
						}
					}
				}(event)

				orderBook = ob

				recordEvent(event)

			case ticker := <-chUpdateTicker:
				if !last.IsZero() {
					diff = time.Since(last)
				}
				last = time.Now()

				if len(tickers) == conf.Trade.MaxLenTickers {
					tickers = tickers[1:]
				}

				tickers = append(tickers, ticker)

				event := entity.MarketDataReplayEvent{
					Msg: entity.ReplayMessage{
						Type: entity.EventTypeTicker,
						Data: tickers,
					},
					Diff: diff,
				}

				go func(e entity.MarketDataReplayEvent) {
					if len(marketDataListeners) > 0 {
						for _, ch := range marketDataListeners {
							ch <- e
						}
					}
				}(event)

				recordEvent(event)
			}
		}
	}()

	go func() {
		err = indodax.ListenMarketData(ctx, conf.Trade.Coin, conf.Trade.Base)
		if err != nil {
			panic(err)
		}
	}()

	runHttpServer(conf.Replay.Port)
}

func runHttpServer(port string) {
	router := gin.Default()

	router.GET("/orderbook", func(ctx *gin.Context) {
		ctx.Header("Content-Type", "application/json")

		ctx.JSON(http.StatusOK, entity.Response{
			Success: true,
			Code:    http.StatusOK,
			Message: http.StatusText(http.StatusOK),
			Data:    orderBook,
		})
	})

	router.POST("/replay-speed", func(ctx *gin.Context) {
		ctx.Header("Content-Type", "application/json")

		var req entity.UpdateReplaySpeedReq

		err := ctx.ShouldBindJSON(&req)
		if err != nil {
			ctx.JSON(http.StatusBadRequest, entity.Response{
				Code:    http.StatusBadRequest,
				Message: http.StatusText(http.StatusBadRequest),
				Error:   err.Error(),
			})
			return
		}

		if req.ReplaySpeed > 0 {
			replaySpeed = req.ReplaySpeed
		}

		ctx.JSON(http.StatusOK, entity.Response{
			Success: true,
			Code:    http.StatusOK,
			Message: http.StatusText(http.StatusOK),
		})
	})

	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	router.GET("/market-data", func(ctx *gin.Context) {
		conn, err := upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
		if err != nil {
			fmt.Printf("[%s][%s] Failed to upgrade conn: %v\n", time.Now().String(), ctx.Request.URL.String(), err)
			return
		}
		defer conn.Close()

		clientId := len(marketDataListeners)

		ch := make(chan entity.MarketDataReplayEvent, 100)
		chClose := make(chan bool)
		defer close(ch)

		mut.Lock()
		marketDataListeners[clientId] = ch
		mut.Unlock()

		go func() {
			for {
				msgType, _, err := conn.ReadMessage()
				if err != nil {
					if websocket.IsUnexpectedCloseError(err) {
						chClose <- true
						break
					}

					continue
				}

				if msgType == websocket.CloseMessage {
					chClose <- true
					break
				}
			}
		}()

	loop:
		for {
			select {
			case <-chClose:
				break loop
			default:
				event := <-ch

				time.Sleep(time.Duration(float64(event.Diff) / replaySpeed))

				err := conn.WriteJSON(event.Msg)
				if err != nil {
					if errors.Is(err, websocket.ErrCloseSent) {
						break
					}

					fmt.Printf("[%s][%s] failed to publish market data: %v \n", time.Now().String(), ctx.Request.URL.String(), err)
					continue
				}
			}
		}

		mut.Lock()
		defer mut.Unlock()

		delete(marketDataListeners, clientId)

	drainLoop:
		for {
			select {
			case <-ch:
			default:
				break drainLoop
			}
		}
	})

	router.GET("/market-data/replay/:recording_name", func(ctx *gin.Context) {
		recordingName := ctx.Param("recording_name")

		data, err := os.ReadFile(fmt.Sprintf("%s/%s.log", conf.Replay.RecordingDir, recordingName))
		if err != nil {
			fmt.Printf("[%s][%s] Failed to open recording: %v\n", time.Now().String(), ctx.Request.URL.String(), err)
			return
		}

		conn, err := upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
		if err != nil {
			fmt.Printf("[%s][%s] Failed to upgrade conn: %v\n", time.Now().String(), ctx.Request.URL.String(), err)
			return
		}
		defer conn.Close()

		ch := make(chan entity.MarketDataReplayEvent, 100)
		chClose := make(chan bool)
		defer close(ch)

		var replayMut sync.Mutex

		sellTimes := []time.Time{}
		replayedOrderbook := entity.Orderbook{}
		emergencySellChSlice := []chan bool{}
		tickerChSlice := []chan entity.Ticker{}

		mockExchange := exchange.NewMockExchange(
			&replayedOrderbook,
			conf.Replay.TradesDir,
		)

		go func() {
			for {
				time.Sleep(50 * time.Millisecond)

				replayMut.Lock()

				now := time.Now()
				windowStart := now.Add(-5 * time.Second)
				i := 0
				for _, t := range sellTimes {
					if t.After(windowStart) {
						sellTimes[i] = t
						i++
					}
				}
				sellTimes = sellTimes[:i]

				replayMut.Unlock()
			}
		}()

		go func() {
		listenLoop:
			for {
				msgType, incomongMsg, err := conn.ReadMessage()
				if err != nil {
					if websocket.IsUnexpectedCloseError(err) {
						chClose <- true
						break listenLoop
					}

					continue listenLoop
				}

				switch msgType {
				case websocket.CloseMessage:
					chClose <- true
					break listenLoop
				case websocket.TextMessage:
					var tradePlan entity.TradePlan
					err := json.Unmarshal(incomongMsg, &tradePlan)
					if err != nil {
						continue
					}

					if tradePlan.Action == "hold" {
						continue listenLoop
					}

					if tradePlan.Action == "buy" {
						emergencySellCh := make(chan bool)
						emergencySellChSlice = append(emergencySellChSlice, emergencySellCh)

						tickerCh := make(chan entity.Ticker)
						tickerChSlice = append(tickerChSlice, tickerCh)

						planManager := model.NewTradePlanManager(
							tradePlan,
							mockExchange,
							tickerCh,
							conf.Trade.Coin,
							conf.Trade.Base,
							emergencySellCh,
						)

						go planManager.Execute()

						continue listenLoop
					}

					if tradePlan.Action == "sell" {
						mut.Lock()
						sellTimes = append(sellTimes, time.Now())

						// 25 count per 5 secs
						if len(sellTimes) >= 25 {
							for _, ech := range emergencySellChSlice {
								select {
								case ech <- true:
									close(ech)
								default:
									continue
								}
							}
							emergencySellChSlice = []chan bool{}
						}

						mut.Unlock()

						continue listenLoop
					}
				}
			}
		}()

		events := []entity.MarketDataReplayEvent{}

		for entry := range strings.SplitSeq(string(data), "\n") {
			if strings.TrimSpace(entry) == "" {
				continue
			}

			var event entity.MarketDataReplayEvent

			err = json.Unmarshal([]byte(entry), &event)
			if err != nil {
				fmt.Printf("[%s][%s] Failed to parse entry: %v\n", time.Now().String(), ctx.Request.URL.String(), err)
				continue
			}

			events = append(events, event)
		}

	loop:
		for {
			select {
			case <-chClose:
				break loop
			default:
				for _, event := range events {
					var replayWg sync.WaitGroup

					replayWg.Add(1)
					go func() {
						defer replayWg.Done()

						time.Sleep(time.Duration(float64(event.Diff) / replaySpeed))

						err := conn.WriteJSON(event.Msg)
						if err != nil {
							if errors.Is(err, websocket.ErrCloseSent) {
								chClose <- true
								return
							}

							fmt.Printf("[%s][%s] failed to publish replayed market data: %v \n", time.Now().String(), ctx.Request.URL.String(), err)
							return
						}
					}()

					replayWg.Add(1)
					go func() {
						defer replayWg.Done()

						rawData, err := json.Marshal(event.Msg.Data)
						if err != nil {
							return
						}

						switch event.Msg.Type {
						case entity.EventTypeOrderbook:
							var updatedReplayedOrderbook entity.Orderbook
							err = json.Unmarshal(rawData, &updatedReplayedOrderbook)
							if err != nil {
								return
							}
							replayedOrderbook = updatedReplayedOrderbook
						case entity.EventTypeTicker:
							var updatedReplayedTickers []entity.Ticker
							err = json.Unmarshal(rawData, &updatedReplayedTickers)
							if err != nil {
								return
							}

							newTickerChSlice := []chan entity.Ticker{}

							for _, t := range updatedReplayedTickers {
								for _, tch := range tickerChSlice {
									select {
									case tch <- t:
										newTickerChSlice = append(newTickerChSlice, tch)
									default:
										close(tch)
										continue
									}
								}
							}

							tickerChSlice = newTickerChSlice
						}
					}()

					replayWg.Wait()
				}
			}
		}

	drainLoop:
		for {
			select {
			case <-ch:
			default:
				break drainLoop
			}
		}
	})

	srv := http.Server{
		Handler: router,
		Addr:    port,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	}()

	quit := make(chan os.Signal, 10)

	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	<-quit

	if err := srv.Shutdown(context.Background()); err != nil {
		panic(err)
	}
}
