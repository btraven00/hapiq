package sra

import (
	"context"
	"crypto/md5" // #nosec G501 -- matches ENA-provided checksum format
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func fastRetry(t *testing.T) {
	t.Helper()
	b, m := retryBackoff, retryMaxBackoff
	retryBackoff, retryMaxBackoff = time.Millisecond, time.Millisecond
	t.Cleanup(func() { retryBackoff, retryMaxBackoff = b, m })
}

func md5hex(b []byte) string {
	s := md5.Sum(b) // #nosec G401 -- test fixture md5
	return hex.EncodeToString(s[:])
}

// rangeServer serves content, honouring "Range: bytes=N-" unless
// ignoreRange, and cuts the body short (declared length, half written) on the
// first cuts requests. It records every request's Range header.
type rangeServer struct {
	content     []byte
	cuts        int
	ignoreRange bool
	status      []int // status to send instead, per request, while non-empty

	mu     sync.Mutex
	ranges []string
}

func (s *rangeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	n := len(s.ranges)
	s.ranges = append(s.ranges, r.Header.Get("Range"))
	s.mu.Unlock()

	if n < len(s.status) {
		w.WriteHeader(s.status[n])
		return
	}
	body, code := s.content, http.StatusOK
	if spec, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes="); ok && !s.ignoreRange {
		from, _ := strconv.Atoi(strings.TrimSuffix(spec, "-"))
		if from >= len(s.content) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		body, code = s.content[from:], http.StatusPartialContent
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, len(s.content)-1, len(s.content)))
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(code)
	if n < s.cuts {
		body = body[:len(body)/2]
	}
	_, _ = w.Write(body)
}

func (s *rangeServer) got() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ranges...)
}

func content(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 7)
	}
	return b
}

func serve(t *testing.T, s *rangeServer) string {
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return srv.URL + "/SRR1.fastq.gz"
}

func checkFile(t *testing.T, dest string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(want) {
		t.Fatalf("dest content wrong (len %d, want %d, err %v)", len(got), len(want), err)
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatalf(".part left behind (stat err=%v)", err)
	}
}

func TestSRAResumeAfterCuts(t *testing.T) {
	fastRetry(t)
	want := content(1000)
	s := &rangeServer{content: want, cuts: 2}
	dest := filepath.Join(t.TempDir(), "out.fastq.gz")
	fi, err := NewSRADownloader().downloadWithMD5(context.Background(), serve(t, s), dest, md5hex(want))
	if err != nil {
		t.Fatal(err)
	}
	checkFile(t, dest, want)
	if fi.Size != 1000 {
		t.Fatalf("size %d", fi.Size)
	}
	if got := s.got(); strings.Join(got, ",") != ",bytes=500-,bytes=750-" {
		t.Fatalf("ranges %q", got)
	}
}

func TestSRAResumeServerIgnoresRange(t *testing.T) {
	fastRetry(t)
	want := content(1000)
	s := &rangeServer{content: want, cuts: 1, ignoreRange: true}
	dest := filepath.Join(t.TempDir(), "out.fastq.gz")
	if _, err := NewSRADownloader().downloadWithMD5(context.Background(), serve(t, s), dest, md5hex(want)); err != nil {
		t.Fatal(err)
	}
	checkFile(t, dest, want)
	if got := s.got(); strings.Join(got, ",") != ",bytes=500-" {
		t.Fatalf("ranges %q", got)
	}
}

func TestSRARetryStatus(t *testing.T) {
	fastRetry(t)
	want := content(100)

	s := &rangeServer{content: want, status: []int{http.StatusForbidden}}
	dest := filepath.Join(t.TempDir(), "out.fastq.gz")
	if _, err := NewSRADownloader().downloadWithMD5(context.Background(), serve(t, s), dest, md5hex(want)); err != nil {
		t.Fatalf("403 then 200: %v", err)
	}
	checkFile(t, dest, want)

	s = &rangeServer{content: want, status: []int{http.StatusNotFound, http.StatusNotFound}}
	dest = filepath.Join(t.TempDir(), "out.fastq.gz")
	if _, err := NewSRADownloader().downloadWithMD5(context.Background(), serve(t, s), dest, md5hex(want)); err == nil {
		t.Fatal("404: want an error")
	}
	if n := len(s.got()); n != 1 {
		t.Fatalf("404: %d requests, want 1", n)
	}
}

func TestSRAMD5MismatchRestartsOnce(t *testing.T) {
	fastRetry(t)
	s := &rangeServer{content: content(100)}
	dest := filepath.Join(t.TempDir(), "out.fastq.gz")
	_, err := NewSRADownloader().downloadWithMD5(context.Background(), serve(t, s), dest, strings.Repeat("0", 32))
	if err == nil || !strings.Contains(err.Error(), "MD5 mismatch") {
		t.Fatalf("err = %v, want MD5 mismatch", err)
	}
	if got := s.got(); strings.Join(got, ",") != "," {
		t.Fatalf("ranges %q, want two clean requests", got)
	}
	for _, p := range []string{dest, dest + ".part"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s left behind (stat err=%v)", p, err)
		}
	}
}

func TestSRAResumesPartFromEarlierRun(t *testing.T) {
	fastRetry(t)
	want := content(1000)
	s := &rangeServer{content: want}
	dest := filepath.Join(t.TempDir(), "out.fastq.gz")
	if err := os.WriteFile(dest+".part", want[:400], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSRADownloader().downloadWithMD5(context.Background(), serve(t, s), dest, md5hex(want)); err != nil {
		t.Fatal(err)
	}
	checkFile(t, dest, want)
	if got := s.got(); strings.Join(got, ",") != "bytes=400-" {
		t.Fatalf("ranges %q", got)
	}
}
