// Package tlsmodule is the module the sdk's TLS tests run in a container against pebble: it
// declares nothing, so it starts on the bootstrap names alone.
package tlsmodule

//go:generate go run ../../cmd/sdkgen -entry tlsmodule.yaml -capabilities . -package tlsmodule -out config.gen.go
