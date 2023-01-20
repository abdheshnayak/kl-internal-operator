package agent

import (
	"github.com/kloudlite/internal_operator_v2/agent/internal/app"
	"go.uber.org/fx"
)

func App() fx.Option {
	return app.Module
}
