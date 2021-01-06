package connection

import (
	"testing"

	"github.com/k0sproject/rig/connection/local"
)

func TestConnect(t *testing.T) {

	c := local.NewConnection()

	err := c.Connect()

	if err != nil {
		t.Errorf(err.Error())
	}
}
