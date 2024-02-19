package rig

import (
	"testing"
)

func TestOSVersionExtraFields(t *testing.T) {
	osv := OSVersion{}

	if _, ok := osv.GetExtraField("one"); ok {
		t.Error("OSVersion get extra field thinks that a value exists when it shouldn't")
	}

	osv.SetExtraField("one", "one")
	if val, ok := osv.GetExtraField("one"); !ok {
		t.Error("OSVersion get extra field thinks that a value doesn't exists when it should")
	} else if val != "one" {
		t.Errorf("OSVersion get extra field return an unexpected value: one != %s", val)
	}

	if _, ok := osv.GetExtraField("two"); ok {
		t.Error("OSVersion get extra field thinks that a value exists when it shouldn't")
	}
}
