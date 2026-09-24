// Package version owns the CampusOS application version exposed by runtime
// metadata and generated contracts. Component packages may keep independent
// package versions, but must not duplicate the application version string.
package version

const (
	Number  = "1.1.0-dev"
	Display = "v" + Number
	OpenAPI = Number + "-experimental"
)
