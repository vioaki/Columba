package server

import (
	"net/http"
	"testing"
)

func TestProtocolSchemaLibraryHTTPAndCLI(t *testing.T) {
	s, _ := newProtocolAdminTestServer(t)
	c, rec := adminProtocolContext(http.MethodGet, "/api/admin/protocols/schema", "")
	s.adminProtocolSchemaV2(contextForTest(c), c)
	flushTestResponse(c)
	data := decodeAdminData(t, rec)
	definition := data["definitionSchema"].(map[string]any)
	if definition["$ref"] != "#/$defs/Definition" || definition["$id"] != nil {
		t.Fatalf("unexpected root: %v", definition)
	}
	if definition["$defs"].(map[string]any)["CapabilitySet"] == nil {
		t.Fatal("named map definition missing over HTTP")
	}
}
