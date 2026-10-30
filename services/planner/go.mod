module github.com/racherom/modular-platform/services/planner

go 1.26.0

toolchain go1.27.3

require (
	github.com/oapi-codegen/runtime v1.8.0
	github.com/racherom/modular-platform/catalog v0.0.0
	github.com/racherom/modular-platform/libs v0.0.0
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/compose-spec/compose-go/v2 v2.16.2 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-shellwords v1.0.12 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/sirupsen/logrus v1.10.1 // indirect
	github.com/xhit/go-str2duration/v2 v2.1.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	go.yaml.in/yaml/v4 v4.0.0-rc.4 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.39.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)

replace github.com/racherom/modular-platform/libs => ../../libs

replace github.com/racherom/modular-platform/catalog => ../../catalog
