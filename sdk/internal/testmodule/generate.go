// Package testmodule is a module the sdk's own tests run: its catalog entry testmodule.yaml
// declares one value of every type, and sdkgen writes its getters, as for any module.
package testmodule

//go:generate go run ../../cmd/sdkgen -entry testmodule.yaml -capabilities capabilities -package testmodule -out config.gen.go
