package common

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/btraven00/hapiq/internal/version"
)

// UserAgent is how hapiq names itself to every server. The Mozilla/5.0
// (compatible; ...) form is what crawlers use: some hosts (SharePoint) serve
// only clients that start with it, and it still says who is asking.
func UserAgent() string {
	return "Mozilla/5.0 (compatible; hapiq/" + version.String() + ")"
}

// extraHeaders are the user's --header values. Set once at startup, before any
// request is made.
var extraHeaders http.Header

// SetExtraHeaders parses curl-style "Name: value" strings and sends them with
// every request made through a HeaderTransport. They replace headers of the
// same name, the User-Agent included.
func SetExtraHeaders(lines []string) error {
	h := http.Header{}
	for _, line := range lines {
		name, value, ok := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return fmt.Errorf("invalid --header %q, want \"Name: value\"", line)
		}
		h.Add(name, strings.TrimSpace(value))
	}
	extraHeaders = h
	return nil
}

// HeaderTransport wraps base so that every request carries hapiq's User-Agent
// (unless the request sets its own) and the user's --header values.
func HeaderTransport(base http.RoundTripper) http.RoundTripper {
	return headerTransport{base}
}

type headerTransport struct{ base http.RoundTripper }

func (t headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context()) // a RoundTripper must not modify the caller's request
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", UserAgent())
	}
	for name, values := range extraHeaders {
		req.Header[name] = values
	}
	return t.base.RoundTrip(req)
}
