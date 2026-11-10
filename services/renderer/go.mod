module github.com/racherom/modular-platform/services/renderer

go 1.26.8

toolchain go1.27.3

require (
	github.com/oapi-codegen/runtime v1.8.0
	github.com/racherom/modular-platform/catalog v0.0.0
	github.com/racherom/modular-platform/libs v0.0.0
)

require (
	github.com/go-jose/go-jose/v4 v4.1.7 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/compose-spec/compose-go/v2 v2.16.2 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/go-connections v0.8.1 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-shellwords v1.0.15 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/racherom/modular-platform/sdk v0.0.0
	github.com/sirupsen/logrus v1.10.2 // indirect
	github.com/xhit/go-str2duration/v2 v2.1.0 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	go.yaml.in/yaml/v4 v4.0.0-rc.6 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)

replace github.com/racherom/modular-platform/libs => ../../libs

replace github.com/racherom/modular-platform/catalog => ../../catalog

replace github.com/racherom/modular-platform/sdk => ../../sdk

tool github.com/racherom/modular-platform/sdk/cmd/sdkgen
