package config

import (
	"fmt"

	hConfig "github.com/michaelyusak/go-helper/config"
	"github.com/michaelyusak/go-helper/entity"
)

type IndodaxConfig struct {
	BaseUrl   string `json:"base_url"`
	ApiKey    string `json:"api_key"`
	SecretKey string `json:"secret_key"`

	PublicWsToken      string `json:"public_ws_token"`
	WsScheme           string `json:"ws_scheme"`
	WsHost             string `json:"ws_host"`
	WsPath             string `json:"ws_path"`
	OrderbookWsChannel string `json:"orderbook_ws_channel"`
	TickerWsChannel    string `json:"ticker_ws_channel"`

	Timeout entity.Duration `json:"timeout"`
}

type ExchangeConfig struct {
	Indodax IndodaxConfig `json:"indodax"`
}

type TradeConfig struct {
	Coin          string `json:"coin"`
	Base          string `json:"base"`
	MaxLenTickers int    `json:"max_len_tickers"`
}

type ReplayConfig struct {
	Port         string  `json:"port"`
	ReplaySpeed  float64 `json:"replay_speed"`
	Record       bool    `json:"record"`
	RecordingDir string  `json:"recording_directory"`
	TradesDir    string  `json:"trades_directory"`
}

type AppConfig struct {
	Identity string         `json:"identity"`
	LogDir   string         `json:"log_dir"`
	Exchange ExchangeConfig `json:"exchange"`
	Trade    TradeConfig    `json:"trade"`
	Replay   ReplayConfig   `json:"replay"`
}

func Init(conf *AppConfig) error {
	c, err := hConfig.InitFromJson[AppConfig]("./config/config.json")
	if err != nil {
		return fmt.Errorf("[config][Init][hConfig.InitFromJson] Error: %w", err)
	}

	*conf = c

	return nil
}
