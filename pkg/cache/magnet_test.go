package cache_test

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/btraven00/hapiq/pkg/cache"
)

// TestResolveMagnet covers what the cache server says about magnets: it passes
// on the link the seeder published, never when a token guards the cache (a
// magnet would let anyone fetch the blob without it), and never garbage or
// hosts other than the peer's own.
func TestResolveMagnet(t *testing.T) {
	const xt = "magnet:?xt=urn:btih:c12fe1c06bba254a9dc9f519b335aa7c1367a88a"
	const magnet = xt + "&x.pe=127.0.0.1%3A1"
	// The test server answers on 127.0.0.1; everything pointing elsewhere is
	// a host the user never configured.
	const mixed = xt + "&tr=http%3A%2F%2F169.254.169.254%2Fannounce&tr=http%3A%2F%2F127.0.0.1%3A6969%2Fannounce" +
		"&x.pe=10.0.0.1%3A1&x.pe=127.0.0.1%3A1&xs=http%3A%2F%2F127.0.0.1%2Fx.torrent&ws=http%3A%2F%2F127.0.0.1%2F"

	cases := []struct {
		name, token, sidecar, want string
	}{
		{"published", "", magnet + "\n", magnet},
		{"token set", "s3cret", magnet + "\n", ""},
		{"not a magnet", "", "http://example.org/evil\n", ""},
		{"no sidecar", "", "", ""},
		{"foreign hosts dropped", "", mixed + "\n", xt + "&tr=http%3A%2F%2F127.0.0.1%3A6969%2Fannounce&x.pe=127.0.0.1%3A1"},
		{"only foreign hosts", "", xt + "&tr=http%3A%2F%2Finternal.example%2Fannounce&x.pe=10.0.0.1%3A1\n", ""},
		{"bad infohash", "", "magnet:?xt=urn:btih:zz&x.pe=127.0.0.1%3A1\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakeHelper(t, "")
			hit := resolveMagnet(t, magnetPeer(t, tc.token, tc.sidecar))
			if hit.Magnet != tc.want {
				t.Errorf("magnet = %q, want %q", hit.Magnet, tc.want)
			}
		})
	}
}

const magnetOrigin = "https://example.org/data.h5ad"

// magnetPeer serves a cache holding one blob whose seeder sidecar is sidecar
// ("" for none), and returns a peer client pointed at it.
func magnetPeer(t *testing.T, token, sidecar string) *cache.Peers {
	t.Helper()
	c := openTestCache(t)
	tmpPath, hash := writeTmp(t, c, []byte("some dataset bytes"))
	if err := c.Put(context.Background(), magnetOrigin, tmpPath, hash); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if sidecar != "" {
		dir := filepath.Join(c.Dir(), "magnets")
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, hash), []byte(sidecar), 0o640); err != nil {
			t.Fatal(err)
		}
	}

	cfg := cache.ServerConfig{Token: token}
	srv := httptest.NewServer(cache.NewServer(c, cfg).Handler())
	t.Cleanup(srv.Close)
	cfg.Peers = []string{srv.URL}
	p := cache.NewPeers(cfg)
	t.Cleanup(p.Close)
	return p
}

func resolveMagnet(t *testing.T, p *cache.Peers) cache.PeerHit {
	t.Helper()
	hit, ok := p.Resolve(context.Background(), magnetOrigin)
	if !ok {
		t.Fatal("resolve missed")
	}
	return hit
}

// fakeHelper puts a shell-script hapiq-fetch-magnet on PATH. It handshakes,
// then runs body for each request $line, with $n counting fetch requests.
func fakeHelper(t *testing.T, body string) {
	t.Helper()
	fakeHelperScript(t, "echo '{\"protocol\":1}'\nn=0\nwhile read -r line; do\n"+
		"case $line in *'\"fetch\"'*) n=$((n+1)) ;; esac\n"+body+"\ndone\n")
}

// fakeHelperScript puts a hapiq-fetch-magnet running script under sh on PATH.
func fakeHelperScript(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, cache.MagnetHelper), []byte("#!/bin/sh\n"+script), 0o755); err != nil { // #nosec G306 -- test executable
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

const testMagnet = "magnet:?xt=urn:btih:c12fe1c06bba254a9dc9f519b335aa7c1367a88a&x.pe=127.0.0.1%3A1\n"

// Without a helper on PATH a magnet is useless, so Resolve does not return
// one and the caller prepares no magnet download.
func TestNoHelperNoMagnet(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if m := resolveMagnet(t, magnetPeer(t, "", testMagnet)).Magnet; m != "" {
		t.Errorf("magnet = %q with no helper installed", m)
	}
}

// A helper that dies mid-run costs one failed fetch; after that Resolve stops
// handing out magnets.
func TestDeadHelperNoMagnet(t *testing.T) {
	fakeHelper(t, "exit 1")
	p := magnetPeer(t, "", testMagnet)
	if err := p.FetchMagnet(context.Background(), resolveMagnet(t, p), filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("fetch succeeded")
	}
	if m := resolveMagnet(t, p).Magnet; m != "" {
		t.Errorf("magnet = %q after the helper died", m)
	}
}

// A helper that fails its handshake is ignored with the actual reason.
func TestHelperHandshakeErrors(t *testing.T) {
	for _, tc := range []struct{ script, want string }{
		{"echo 'bind: address already in use' >&2; exit 1\n", "exited before its handshake"},
		{"echo '{\"protocol\":2}'; cat >/dev/null\n", "speaks protocol 2, this hapiq speaks 1"},
		{"echo hello; cat >/dev/null\n", "first line is not a handshake"},
	} {
		fakeHelperScript(t, tc.script)
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		stderr := os.Stderr
		os.Stderr = w
		err = magnetPeer(t, "", "").FetchMagnet(context.Background(), cache.PeerHit{}, "/nonexistent")
		os.Stderr = stderr
		_ = w.Close()
		out, _ := io.ReadAll(r)
		if !errors.Is(err, cache.ErrNoMagnetHelper) || !strings.Contains(string(out), tc.want) {
			t.Errorf("err = %v, stderr = %q, want %q", err, out, tc.want)
		}
	}
}

// A helper that does not confirm a cancel in time is killed before FetchMagnet
// returns: nothing may write the destination after that.
func TestUnacknowledgedCancelKillsHelper(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	t.Setenv("OUT", out)
	fakeHelper(t, `case $line in *'"fetch"'*) while :; do echo x >> "$OUT"; sleep 0.05; done ;; esac`)
	p := magnetPeer(t, "", testMagnet)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := p.FetchMagnet(ctx, resolveMagnet(t, p), out); err == nil {
		t.Fatal("cancelled fetch succeeded")
	}
	before, _ := os.ReadFile(out)
	time.Sleep(300 * time.Millisecond)
	if after, _ := os.ReadFile(out); len(after) != len(before) {
		t.Errorf("dest grew from %d to %d bytes after FetchMagnet returned", len(before), len(after))
	}
	if m := resolveMagnet(t, p).Magnet; m != "" {
		t.Errorf("magnet = %q after the helper was killed", m)
	}
}

// After a magnet times out, the rest of the run skips that peer's magnets
// instead of paying the stall timeout for every file. Other failures cost no
// wait and do not count.
func TestMagnetTimeoutDisablesPeerMagnets(t *testing.T) {
	const magnet = "magnet:?xt=urn:btih:c12fe1c06bba254a9dc9f519b335aa7c1367a88a&x.pe=127.0.0.1%3A1\n"
	for _, tc := range []struct {
		name, reply string
		skipped     bool
	}{
		{"timeout", `{"id":%d,"error":"timeout: no metadata after 30s","timeout":true}`, true},
		{"refused", `{"id":%d,"error":"torrent is 3 bytes, expected 18"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeHelper(t, `printf '`+tc.reply+`\n' "$n"`)
			p := magnetPeer(t, "", magnet)

			hit := resolveMagnet(t, p)
			if err := p.FetchMagnet(context.Background(), hit, filepath.Join(t.TempDir(), "out")); err == nil {
				t.Fatal("fetch succeeded")
			}
			if got := resolveMagnet(t, p).Magnet == ""; got != tc.skipped {
				t.Errorf("magnet skipped after %s = %v, want %v", tc.name, got, tc.skipped)
			}
		})
	}
}

// A cancelled fetch returns only once the helper has confirmed the cancel:
// before that the helper may still write the file the caller removes next.
func TestMagnetCancelWaitsForHelper(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "acked")
	t.Setenv("MARK", mark)
	fakeHelper(t, `case $line in *cancel*) sleep 0.3; : > "$MARK"; printf '{"id":%d,"error":"context canceled"}\n' "$n" ;; esac`)
	p := magnetPeer(t, "", "magnet:?xt=urn:btih:c12fe1c06bba254a9dc9f519b335aa7c1367a88a&x.pe=127.0.0.1%3A1\n")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := p.FetchMagnet(ctx, resolveMagnet(t, p), filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("cancelled fetch succeeded")
	}
	if _, err := os.Stat(mark); err != nil {
		t.Error("FetchMagnet returned before the helper acknowledged the cancel")
	}
}
