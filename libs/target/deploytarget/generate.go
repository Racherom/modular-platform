// Package deploytarget is the deploy target contract (contracts/openapi/deploy-target.yaml): its
// types and the client api uses. A deployer serves the same types through its own generated server.
package deploytarget

//go:generate go tool oapi-codegen -config oapi-codegen.yaml ../../../contracts/openapi/deploy-target.yaml
