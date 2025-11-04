package config

import (
	"fmt"

	hConfig "github.com/michaelyusak/go-helper/config"
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
}

type ExchangeConfig struct {
	Indodax IndodaxConfig `json:"indodax"`
}

type TradeConfig struct {
	Coin string `json:"coin"`
	Base string `json:"base"`
}

type ReplayConfig struct {
	Port        string  `json:"port"`
	ReplaySpeed float64 `json:"replay_speed"`
}

type AppConfig struct {
	Exchange ExchangeConfig `json:"exchange"`
	Trade    TradeConfig    `json:"trade"`
	Replay   ReplayConfig   `json:"replay"`
}

func Init() (AppConfig, error) {
	conf, err := hConfig.InitFromJson[AppConfig]("./config/config.json")
	if err != nil {
		return AppConfig{}, fmt.Errorf("[config][Init][hConfig.InitFromJson] Error: %w", err)
	}

	return conf, nil
}
