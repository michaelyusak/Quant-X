package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"quant-x/adapters/exchanges"
	"quant-x/config"
	"quant-x/entity"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var (
	orderBook   entity.Orderbook
	replaySpeed float64

	mut sync.Mutex

	marketDataListeners map[int]chan entity.OrderbookReplayEvent
)

func main() {
	conf, err := config.Init()
	if err != nil {
		panic(err)
	}

	replaySpeed = conf.Replay.ReplaySpeed
	marketDataListeners = map[int]chan entity.OrderbookReplayEvent{}

	chUpdateOrderbook := make(chan entity.Orderbook)
	defer close(chUpdateOrderbook)

	indodax := exchanges.NewIndodaxAdapter(
		conf.Exchange.Indodax.ApiKey,
		conf.Exchange.Indodax.SecretKey,
		conf.Exchange.Indodax.BaseUrl,
		conf.Exchange.Indodax.WsScheme,
		conf.Exchange.Indodax.WsHost,
		conf.Exchange.Indodax.WsPath,
		conf.Exchange.Indodax.PublicWsToken,
		conf.Exchange.Indodax.OrderbookWsChannel,
		chUpdateOrderbook,
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
			ob := <-chUpdateOrderbook

			if !last.IsZero() {
				diff = time.Since(last)
			}
			last = time.Now()

			go func(d time.Duration, orderbook entity.Orderbook) {
				if len(marketDataListeners) > 0 {
					for _, ch := range marketDataListeners {
						ch <- entity.OrderbookReplayEvent{
							Orderbook: orderbook,
							Diff:      d,
						}
					}
				}
			}(diff, ob)

			orderBook = ob

			fmt.Printf("[%s] orderbook updated [diff: %v]\n", time.Now().String(), diff)
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
			fmt.Printf("[%s] Replay speed updated to %v\n", time.Now().String(), replaySpeed)
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

	router.GET("/market-data/order-book", func(ctx *gin.Context) {
		conn, err := upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
		if err != nil {
			fmt.Printf("[%s] Failed to upgrade conn: %v\n", time.Now().String(), err)
			return
		}
		defer conn.Close()

		clientId := len(marketDataListeners)

		ch := make(chan entity.OrderbookReplayEvent, 20)
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

				err := conn.WriteJSON(event.Orderbook)
				if err != nil {
					if errors.Is(err, websocket.ErrCloseSent) {
						break
					}

					fmt.Printf("[%s] failed to publish replayed orderbook: %v \n", time.Now().String(), err)
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
