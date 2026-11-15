package common

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHeaderTransport(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Header }))
	defer srv.Close()
	client := &http.Client{Transport: HeaderTransport(http.DefaultTransport)}
	defer func() { extraHeaders = nil }()

	get := func() {
		t.Helper()
		resp, err := client.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}

	get()
	if ua := got.Get("User-Agent"); !strings.HasPrefix(ua, "Mozilla/5.0 (compatible; hapiq/") {
		t.Errorf("default User-Agent = %q", ua)
	}

	if err := SetExtraHeaders([]string{"user-agent: mine/1", "X-Token:abc"}); err != nil {
		t.Fatal(err)
	}
	get()
	if got.Get("User-Agent") != "mine/1" || got.Get("X-Token") != "abc" {
		t.Errorf("headers with --header = %v", got)
	}

	if err := SetExtraHeaders([]string{"no-colon"}); err == nil {
		t.Error("SetExtraHeaders accepted a line without a colon")
	}
}
