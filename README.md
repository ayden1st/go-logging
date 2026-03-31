# go-logging

A minimal, production-ready logging library for Go built on `log/slog`.

## Features

- JSON and text output
- Configurable log level
- Structured logging
- Contextual attributes (`With`, `WithGroup`)
- Global singleton with safe initialization
- No external dependencies

## Usage

```go
import "github.com/yourname/go-logging"

cfg := logging.DefaultConfig()
cfg.Level = logging.ParseLevel("debug")
cfg.Service = "myapp"
logging.Init(cfg)

logging.Get().Info("Hello!")
