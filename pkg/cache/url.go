package cache

import (
	"net/url"
	"strings"
)

// canonicalizeURL normalizes a URL for use as a cache key.
// Rules: lowercase scheme+host, strip default ports, drop fragment, preserve query.
func canonicalizeURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	u.Scheme = strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()

	switch u.Scheme {
	case "http":
		if port == "80" {
			port = ""
		}
	case "https":
		if port == "443" {
			port = ""
		}
	case "ftp":
		if port == "21" {
			port = ""
		}
	}

	if port == "" {
		u.Host = host
	} else {
		u.Host = host + ":" + port
	}

	u.Fragment = ""
	u.RawFragment = ""

	return u.String(), nil
}
