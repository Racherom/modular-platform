module github.com/racherom/modular-platform/catalog

go 1.26.0

toolchain go1.27.3

require github.com/racherom/modular-platform/libs v0.0.0

require (
	github.com/distribution/reference v0.6.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)

replace github.com/racherom/modular-platform/libs => ../libs
