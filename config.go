package main

import (
	"github.com/caarlos0/env/v6"
	"time"
)

type Config struct {
	PollInterval time.Duration `env:"POLL_INTERVAL" envDefault:"5s"`
	LogLevel     string        `env:"LOG_LEVEL" envDefault:"INFO"`
}

func NewConfig() *Config {
	cfg := Config{}
	if err := env.Parse(&cfg); err != nil {
		panic(err)
	}
	return &cfg
}
