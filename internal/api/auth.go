package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
)

type Credentials struct {
	Username string
	Password string
}

type basicAuth struct {
	usernameHash [sha256.Size]byte
	passwordHash [sha256.Size]byte
}

func newBasicAuth(credentials Credentials) (*basicAuth, error) {
	if credentials.Username == "" {
		return nil, errors.New("username is required")
	}
	if credentials.Password == "" {
		return nil, errors.New("password is required")
	}
	return &basicAuth{
		usernameHash: sha256.Sum256([]byte(credentials.Username)),
		passwordHash: sha256.Sum256([]byte(credentials.Password)),
	}, nil
}

func (a *basicAuth) require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || !a.matches(username, password) {
			w.Header().Set("WWW-Authenticate", `Basic realm="logthing", charset="UTF-8"`)
			writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "authentication required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *basicAuth) matches(username, password string) bool {
	usernameHash := sha256.Sum256([]byte(username))
	passwordHash := sha256.Sum256([]byte(password))

	usernameOK := subtle.ConstantTimeCompare(usernameHash[:], a.usernameHash[:]) == 1
	passwordOK := subtle.ConstantTimeCompare(passwordHash[:], a.passwordHash[:]) == 1
	return usernameOK && passwordOK
}
