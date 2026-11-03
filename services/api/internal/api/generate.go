// Package api serves api's contract (contracts/openapi/api.yaml) to the editor: it parses each
// request, calls the sign in or deploy service, and maps the answer onto the contract.
package api

//go:generate go tool oapi-codegen -config oapi-codegen.yaml ../../../../contracts/openapi/api.yaml
