package api

import "testing"

func TestBasicAuthMatchesCredentials(t *testing.T) {
	auth, err := newBasicAuth(Credentials{
		Username: "admin",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("newBasicAuth() error = %v", err)
	}

	tests := []struct {
		name     string
		username string
		password string
		want     bool
	}{
		{
			name:     "matching credentials",
			username: "admin",
			password: "secret",
			want:     true,
		},
		{
			name:     "wrong username",
			username: "operator",
			password: "secret",
		},
		{
			name:     "wrong password",
			username: "admin",
			password: "password",
		},
		{
			name:     "wrong password length",
			username: "admin",
			password: "secrets",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := auth.matches(tt.username, tt.password); got != tt.want {
				t.Fatalf("matches(%q, %q) = %t, want %t", tt.username, tt.password, got, tt.want)
			}
		})
	}
}
