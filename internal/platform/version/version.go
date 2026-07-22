// Package version owns the CampusOS application version exposed by runtime
// metadata and generated contracts. Component packages may keep independent
// package versions, but must not duplicate the application version string.
package version

const (
	Number  = "0.13.0"
	Display = "v" + Number
	OpenAPI = Number + "-experimental"
)
