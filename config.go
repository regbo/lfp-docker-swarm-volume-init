package main

import (
	"encoding/json"
	"github.com/caarlos0/env/v6"
	"sync"
	"time"
)

type Config struct {
	ContainerID  string        `env:"CONTAINER_ID,notEmpty"`
	PollInterval time.Duration `env:"POLL_INTERVAL" envDefault:"3s"`
	PollSince    time.Duration `env:"POLL_SINCE" envDefault:"1m"`
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

func (x *Config) Map() map[string]any {
	if buf, err := json.Marshal(x); err != nil {
		panic(err)
	} else {
		var result map[string]any
		if err = json.Unmarshal(buf, &result); err != nil {
			panic(err)
		}
		return result
	}
}
