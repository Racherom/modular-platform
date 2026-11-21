module github.com/racherom/modular-platform/services/renderer

go 1.26.8

toolchain go1.27.3

require (
	github.com/oapi-codegen/runtime v1.8.0
	github.com/racherom/modular-platform/catalog v0.0.0
	github.com/racherom/modular-platform/libs v0.0.0
)

require (
	github.com/Microsoft/go-winio v0.6.3 // indirect
	github.com/dlclark/regexp2 v1.11.4 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/go-jose/go-jose/v4 v4.1.7 // indirect
	github.com/klauspost/compress v1.20.1 // indirect
	github.com/moby/moby/client v0.6.2 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.72.0 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/metric v1.47.0 // indirect
	go.opentelemetry.io/otel/sdk v1.47.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20261005182115-fad411399dd8 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261005182115-fad411399dd8 // indirect
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/compose-spec/compose-go/v2 v2.16.2 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/go-connections v0.8.2 // indirect
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
