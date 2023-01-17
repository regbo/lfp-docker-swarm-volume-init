package main

import (
	"encoding/json"
	"github.com/caarlos0/env/v6"
	"github.com/sirupsen/logrus"
	"sync"
	"time"
)

type Config struct {
	ContainerID   string        `env:"CONTAINER_ID,notEmpty"`
	PollInterval  time.Duration `env:"POLL_INTERVAL" envDefault:"5s"`
	MkdirInterval time.Duration `env:"MKDIR_INTERVAL" envDefault:"30s"`
	LogLevel      string        `env:"LOG_LEVEL" envDefault:"INFO"`
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

func (x *Config) LogFields() logrus.Fields {
	if buf, err := json.Marshal(x); err != nil {
		panic(err)
	} else {
		fields := logrus.Fields{}
		if err = json.Unmarshal(buf, &fields); err != nil {
			panic(err)
		}
		return fields
	}
}
