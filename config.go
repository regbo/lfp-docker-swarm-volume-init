package main

import (
	"github.com/caarlos0/env/v6"
	"sync"
	"time"
)

type Config struct {
	ContainerID  string        `env:"CONTAINER_ID"`
	PollInterval time.Duration `env:"POLL_INTERVAL" envDefault:"5s"`
	LogLevel     string        `env:"LOG_LEVEL" envDefault:"INFO"`
}

var (
	_configInstance     Config
	_configInstanceOnce sync.Once
)

func GetConfig() *Config {
	_configInstanceOnce.Do(func() {
		v := Config{}
		if err := env.Parse(&v); err != nil {
			panic(err)
		}
		_configInstance = v
	})
	return &_configInstance
}
