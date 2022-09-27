package env

import (
	"github.com/codingconcepts/env"
)

type Env struct {
	NameserverEndpoint string `env:"NAMESERVER_ENDPOINT" required:"true"`
	WgDomain           string `env:"WG_DOMAIN" required:"true"`
}

func GetEnvOrDie() *Env {
	var ev Env
	if err := env.Set(&ev); err != nil {
		panic(err)
	}
	return &ev
}
