package relay

import (
	"columba.local/backend/protocol"
	"columba.local/backend/protocol/builtin"
)

// NewProtocolCompiler installs wire adapters that operate on the ordered model
// directly. The compiler owns neither HTTP clients nor credentials.
func NewProtocolCompiler(limits protocol.Limits) (*protocol.Compiler, error) {
	return protocol.NewCompiler(limits, builtin.Modules(), []string{"transport.websocket", "tasks.async"})
}
