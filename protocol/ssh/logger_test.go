package ssh

import (
	"testing"

	"github.com/k0sproject/rig/v2/log"
	"github.com/k0sproject/rig/v2/rigtest"
)

// The injection is only observable through a Config method that logs, and none
// does; this asserts the wiring directly instead. Passing a Config value to
// InjectLoggerTo silently does nothing, because log's injectable interface
// needs the pointer-receiver SetLogger.
func TestNewConnectionInjectsLoggerIntoConfig(t *testing.T) {
	logger := &rigtest.MockLogger{}
	conn, err := NewConnection(Config{
		Address:         "10.0.0.1",
		User:            "root",
		IgnoreSSHConfig: true,
	}, WithLogger(logger))
	if err != nil {
		t.Fatalf("NewConnection() error = %v", err)
	}

	if !log.HasLogger(&conn.Config) {
		t.Error("the connection's Config has no logger, want the one passed to WithLogger")
	}
	if !log.HasLogger(conn) {
		t.Error("the connection has no logger, want the one passed to WithLogger")
	}
}
