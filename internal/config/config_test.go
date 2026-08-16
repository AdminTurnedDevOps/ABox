package config

import "testing"

func TestValidateRejectsUnknownMode(t *testing.T) {
	c := Defaults()
	c.Connectivity.Mode = "wide-open"
	if err := c.Validate(); err == nil {
		t.Fatal("expected error")
	}
}

func TestDefaultsValid(t *testing.T) {
	if err := Defaults().Validate(); err != nil {
		t.Fatal(err)
	}
}
