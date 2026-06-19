package api

import (
	"crypto/subtle"
	"errors"
	"net/http"
)

type Credentials struct {
	Username string
	Password string
}

type basicAuth struct {
	username []byte
	password []byte
}

func newBasicAuth(credentials Credentials) (*basicAuth, error) {
	if credentials.Username == "" {
		return nil, errors.New("username is required")
	}
	if credentials.Password == "" {
		return nil, errors.New("password is required")
	}
	return &basicAuth{
		username: []byte(credentials.Username),
		password: []byte(credentials.Password),
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
	usernameOK := subtle.ConstantTimeCompare([]byte(username), a.username) == 1
	passwordOK := subtle.ConstantTimeCompare([]byte(password), a.password) == 1
	return usernameOK && passwordOK
}
