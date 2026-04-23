module github.com/hellodk/hetu/pkg/bus

go 1.25.0

require (
	github.com/hellodk/hetu/pkg/config v0.0.0
	github.com/hellodk/hetu/pkg/logger v0.0.0
	github.com/nats-io/nats.go v1.50.0
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/rs/zerolog v1.35.0 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/hellodk/hetu/pkg/config => ../config
	github.com/hellodk/hetu/pkg/logger => ../logger
)
