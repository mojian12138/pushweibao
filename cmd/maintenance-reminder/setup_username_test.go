package main

import "testing"

func TestValidateUsername(t *testing.T) {
	for _, value := range []string{"admin", "operator1", "老板账号"} {
		if err := validateUsername(value); err != nil {
			t.Errorf("validateUsername(%q): %v", value, err)
		}
	}
	for _, value := range []string{"has space", "tab\tname", "line\nname", "12345678901234567890123456789012345678901234567890123456789012345"} {
		if err := validateUsername(value); err == nil {
			t.Errorf("validateUsername(%q) should fail", value)
		}
	}
}
