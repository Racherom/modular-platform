// Package api serves the deploy target contract (contracts/openapi/deploy-target.yaml) over the
// deploy engine; it parses and maps, and leaves every decision to package deploy.
package api

//go:generate go tool oapi-codegen -config oapi-codegen.yaml ../../../../contracts/openapi/deploy-target.yaml
