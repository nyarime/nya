package main

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type basicAuth struct {
	user string
	pass string
}

func (a basicAuth) enabled() bool {
	return a.user != ""
}

func parseSendBasicAuth(user, password string) (basicAuth, error) {
	user = strings.TrimSpace(user)
	if user == "" {
		return basicAuth{}, nil
	}
	if password == "" {
		return basicAuth{}, fmt.Errorf("send: -password is required when -user is set")
	}
	return basicAuth{user: user, pass: password}, nil
}

// resolveGetAuth merges -user/-password with optional userinfo in the URL.
// Credentials are stripped from the returned URL so logs do not echo secrets.
func resolveGetAuth(rawURL, flagUser, flagPass string) (cleanURL string, auth basicAuth, err error) {
	rawURL = strings.TrimSpace(rawURL)
	flagUser = strings.TrimSpace(flagUser)
	if rawURL == "" {
		if flagUser == "" {
			return "", basicAuth{}, nil
		}
		return "", basicAuth{user: flagUser, pass: flagPass}, nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", basicAuth{}, err
	}
	user, pass := flagUser, flagPass
	if u.User != nil {
		if user == "" {
			user = u.User.Username()
		}
		if pass == "" {
			pass, _ = u.User.Password()
		}
		u.User = nil
	}
	cleanURL = u.String()
	if user != "" {
		auth = basicAuth{user: user, pass: pass}
	}
	return cleanURL, auth, nil
}

func (a basicAuth) wrapHandler(next http.Handler) http.Handler {
	if !a.enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok ||
			subtle.ConstantTimeCompare([]byte(u), []byte(a.user)) != 1 ||
			subtle.ConstantTimeCompare([]byte(p), []byte(a.pass)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="nya send"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type basicAuthRoundTripper struct {
	user string
	pass string
	base http.RoundTripper
}

func (t *basicAuthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.SetBasicAuth(t.user, t.pass)
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req2)
}

func formatGetAuthHint(indexURL, user string) string {
	if user == "" {
		return fmt.Sprintf("nya get --url %s", indexURL)
	}
	return fmt.Sprintf("nya get --url %s -user %s -password <secret>", indexURL, shellQuote(user))
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t'\"\\$") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
