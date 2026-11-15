package cache

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MagnetHelper is the executable, looked up on PATH, that fetches magnet
// links. It is a separate binary so the BitTorrent stack never links into
// hapiq; see helpers/hapiq-fetch-magnet/README.md for the line protocol.
const MagnetHelper = "hapiq-fetch-magnet"

// magnetProtocol is the helper protocol version this build speaks.
const magnetProtocol = 1

// cancelWait bounds how long a cancelled fetch waits for the helper to
// confirm it has let go of the destination file.
const cancelWait = 5 * time.Second

// ErrNoMagnetHelper means no usable helper is installed. It is the quiet
// failure: magnets are optional and the HTTP blob is always there.
var ErrNoMagnetHelper = errors.New("no " + MagnetHelper + " helper on PATH")

// Magnet returns the magnet link the seeding helper published for a blob, or
// "" when there is none. The helper drops one file per blob into
// <cache>/magnets/<sha256>; that directory is the whole interface between the
// cache server and the seeder.
func (c *Cache) Magnet(sha256hex string) string {
	if !isSHA256Hex(sha256hex) {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(c.cfg.Dir, "magnets", sha256hex)) // #nosec G304 -- validated hash under cache dir
	if err != nil {
		return ""
	}
	return validMagnet(strings.TrimSpace(string(b)))
}

// validMagnet returns s if it looks like a magnet link, else "".
func validMagnet(s string) string {
	if !strings.HasPrefix(s, "magnet:?") || strings.ContainsAny(s, " \t\r\n") {
		return ""
	}
	return s
}

// pinMagnet rebuilds an untrusted peer's magnet from the parts hapiq needs:
// the v1 infohash, plus the trackers and peer addresses (x.pe) on the host of
// the peer base URL that answered. Anything else (other hosts, xs/as/ws
// sources) would make the helper contact, and announce the user to, hosts
// the user never configured. "" if nothing usable is left.
//
// The tracker is meant to run next to the cache, so a downloading node still
// finds the swarm with no configuration. ponytail: exact host-string match,
// so a peer configured by name whose seeder advertises an IP loses its x.pe
// and is found through its tracker instead; resolve names if that matters.
func pinMagnet(s, base string) string {
	u, err := url.Parse(base)
	if validMagnet(s) == "" || err != nil {
		return ""
	}
	host := u.Hostname()
	q, err := url.ParseQuery(strings.TrimPrefix(s, "magnet:?"))
	if err != nil || len(q["xt"]) != 1 {
		return ""
	}
	ih, ok := strings.CutPrefix(q.Get("xt"), "urn:btih:")
	if b, err := hex.DecodeString(ih); !ok || err != nil || len(b) != 20 {
		return ""
	}

	keep := url.Values{}
	for _, tr := range q["tr"] {
		if t, err := url.Parse(tr); err == nil && (t.Scheme == "http" || t.Scheme == "https") && strings.EqualFold(t.Hostname(), host) {
			keep.Add("tr", tr)
		}
	}
	for _, pe := range q["x.pe"] {
		if h, _, err := net.SplitHostPort(pe); err == nil && strings.EqualFold(h, host) {
			keep.Add("x.pe", pe)
		}
	}
	if len(keep) == 0 {
		return ""
	}
	return "magnet:?xt=urn:btih:" + ih + "&" + keep.Encode()
}

// FetchMagnet downloads hit's magnet to dest through the helper, starting the
// helper on first use. dest is NOT verified here: the caller must check the
// sha256 exactly as it does for an HTTP peer.
func (p *Peers) FetchMagnet(ctx context.Context, hit PeerHit, dest string) error {
	p.helperOnce.Do(func() { p.helper = startMagnetHelper() })
	if p.helper == nil {
		return ErrNoMagnetHelper
	}
	err := p.helper.fetch(ctx, hit, dest)
	// A timeout means the swarm behind this peer's magnets is dead (a seeder
	// with a tracker leaves its magnets behind on exit); waiting it out again
	// for every file would cost the stall timeout each time. An immediate
	// refusal or a hash mismatch costs no wait and concerns one blob only.
	// ponytail: for the whole run; retry after a while if runs get long.
	var timeout magnetTimeoutError
	if errors.As(err, &timeout) {
		p.deadSwarms.Store(hit.Base, true)
	}
	return err
}

// magnetUsable reports whether a magnet from base is worth trying: a helper
// is installed and still running (started here on first use), and no magnet
// from base has timed out. Resolve drops the magnet otherwise, so a run
// without a usable helper prepares no download and warns at most once.
func (p *Peers) magnetUsable(base string) bool {
	p.helperOnce.Do(func() { p.helper = startMagnetHelper() })
	if p.helper == nil || p.helper.exited() {
		return false
	}
	_, dead := p.deadSwarms.Load(base)
	return !dead
}

// magnetTimeoutError is a helper error flagged as a timeout: no metadata or
// no progress within its stall timeout.
type magnetTimeoutError string

func (e magnetTimeoutError) Error() string { return string(e) }

// Close stops the magnet helper, if one was started.
func (p *Peers) Close() {
	if p == nil {
		return
	}
	// Run the Once so a helper cannot be started after Close.
	p.helperOnce.Do(func() {})
	if p.helper != nil {
		p.helper.close()
	}
}

type helperMsg struct {
	Error    string `json:"error,omitempty"`
	ID       int    `json:"id,omitempty"`
	Protocol int    `json:"protocol,omitempty"`
	Done     bool   `json:"done,omitempty"`
	Timeout  bool   `json:"timeout,omitempty"`
}

// magnetHelper is one long-lived helper process speaking JSON lines on its
// stdin/stdout. Long-lived because a torrent client is slow to start and a
// dataset is many files.
//
// ponytail: a helper that dies is not restarted; the rest of the run uses
// HTTP. Restart it if helpers turn out to crash in practice.
type magnetHelper struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	pending map[int]chan helperMsg
	nextID  int
	mu      sync.Mutex
	dead    bool
}

func startMagnetHelper() *magnetHelper {
	path, err := exec.LookPath(MagnetHelper)
	if err != nil {
		return nil
	}

	cmd := exec.Command(path) // #nosec G204 -- fixed helper name resolved on PATH
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil
	}
	if err := cmd.Start(); err != nil {
		return nil
	}

	// The first line is the version handshake. A helper that stays silent is
	// killed rather than allowed to hang the download.
	watchdog := time.AfterFunc(10*time.Second, func() { _ = cmd.Process.Kill() })
	sc := bufio.NewScanner(stdout)
	scanned := sc.Scan()
	var hello helperMsg
	var why string
	switch {
	case !watchdog.Stop():
		why = "no handshake within 10s"
	case !scanned:
		why = "it exited before its handshake" // its stderr says why
	case json.Unmarshal(sc.Bytes(), &hello) != nil || hello.Protocol == 0:
		why = "its first line is not a handshake"
	case hello.Protocol != magnetProtocol:
		why = fmt.Sprintf("it speaks protocol %d, this hapiq speaks %d", hello.Protocol, magnetProtocol)
	}
	if why != "" {
		_, _ = fmt.Fprintf(os.Stderr, "cache: ignoring %s: %s\n", path, why)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil
	}

	h := &magnetHelper{cmd: cmd, stdin: stdin, pending: map[int]chan helperMsg{}}
	go h.read(sc)
	return h
}

// read dispatches helper replies to the fetch waiting on each id, and fails
// every outstanding fetch when the helper exits.
func (h *magnetHelper) read(sc *bufio.Scanner) {
	for sc.Scan() {
		var m helperMsg
		if json.Unmarshal(sc.Bytes(), &m) != nil || (!m.Done && m.Error == "") {
			continue
		}
		h.mu.Lock()
		ch := h.pending[m.ID]
		delete(h.pending, m.ID)
		h.mu.Unlock()
		if ch != nil {
			ch <- m
		}
	}

	h.mu.Lock()
	h.dead = true
	for id, ch := range h.pending {
		ch <- helperMsg{ID: id, Error: "helper exited"}
		delete(h.pending, id)
	}
	h.mu.Unlock()
}

func (h *magnetHelper) exited() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.dead
}

func (h *magnetHelper) send(v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = h.stdin.Write(append(line, '\n'))
	return err
}

func (h *magnetHelper) fetch(ctx context.Context, hit PeerHit, dest string) error {
	ch := make(chan helperMsg, 1)

	h.mu.Lock()
	if h.dead {
		h.mu.Unlock()
		return errors.New("magnet helper exited")
	}
	h.nextID++
	id := h.nextID
	h.pending[id] = ch
	err := h.send(map[string]any{"op": "fetch", "id": id, "magnet": hit.Magnet, "dest": dest, "size": hit.Size})
	if err != nil {
		delete(h.pending, id)
	}
	h.mu.Unlock()
	if err != nil {
		return fmt.Errorf("magnet helper: %w", err)
	}

	select {
	case m := <-ch:
		if m.Timeout {
			return magnetTimeoutError(m.Error)
		}
		if m.Error != "" {
			return errors.New(m.Error)
		}
		return nil
	case <-ctx.Done():
		h.mu.Lock()
		_ = h.send(map[string]any{"op": "cancel", "id": id})
		h.mu.Unlock()
		// The helper answers the fetch once it has dropped the torrent. Until
		// then it may still write dest, which the caller removes next.
		select {
		case <-ch:
		case <-time.After(cancelWait):
			// Still busy with dest: kill the helper so nothing can write the
			// file once we return. read() answers ch and marks the helper dead
			// when its stdout closes, which is when the process is gone.
			_ = h.cmd.Process.Kill()
			<-ch
		}
		return ctx.Err()
	}
}

// close ends the helper by closing its stdin, which it treats as "hapiq is
// gone", and reaps it.
func (h *magnetHelper) close() {
	_ = h.stdin.Close()
	done := make(chan struct{})
	go func() { _ = h.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = h.cmd.Process.Kill()
		<-done
	}
}
