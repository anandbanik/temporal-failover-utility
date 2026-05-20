package docs

import "testing"

// TestSwaggerInfoRegistered verifies that importing the docs package triggers
// the init() registration and the SwaggerInfo is populated.
func TestSwaggerInfoRegistered(t *testing.T) {
	if SwaggerInfo.Title == "" {
		t.Error("expected SwaggerInfo.Title to be set by init()")
	}
}
