package rig

import (
	"testing"
)

func TestOSVersionExtraFields(t *testing.T) {
	osv := NewOSVersion()

	if _, ok := osv.ExtraFields["one"]; ok {
		t.Error("OSVersion get extra field thinks that a value exists when it shouldn't")
	}

	osv.ExtraFields["one"] = "one"
	if val, ok := osv.ExtraFields["one"]; !ok {
		t.Error("OSVersion get extra field thinks that a value doesn't exists when it should")
	} else if val != "one" {
		t.Errorf("OSVersion get extra field return an unexpected value: one != %s", val)
	}

	if _, ok := osv.ExtraFields["two"]; ok {
		t.Error("OSVersion get extra field thinks that a value exists when it shouldn't")
	}
}
