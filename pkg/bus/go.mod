module github.com/your-org/cluster-intel/pkg/bus

go 1.25.0

require (
	github.com/nats-io/nats.go v1.50.0
	github.com/your-org/cluster-intel/pkg/config v0.0.0
)

require (
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/your-org/cluster-intel/pkg/config => ../config
