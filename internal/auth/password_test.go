package auth

import (
	"os"
	"testing"
)

func TestEmailAllowed(t *testing.T) {
	os.Setenv("ADMIN_EMAIL", "james67@gmail.com")
	defer os.Unsetenv("ADMIN_EMAIL")

	cases := []struct {
		email string
		want  bool
	}{
		{"sally@ecomist.com.au", true},
		{"SALLY@ECOMIST.COM.AU", true},
		{"  bob@ecomist.com.au ", true},
		{"james67@gmail.com", true},        // the admin
		{"James67@Gmail.com", true},        // case-insensitive
		{"someone@gmail.com", false},       // wrong domain
		{"sally@ecomist.com", false},       // near miss
		{"sally@notecomist.com.au", false}, // suffix trick
		{"", false},
	}
	for _, c := range cases {
		if got := EmailAllowed(c.email); got != c.want {
			t.Errorf("EmailAllowed(%q) = %v, want %v", c.email, got, c.want)
		}
	}
}
