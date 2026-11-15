package common

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/textproto"

	"github.com/jlaffaye/ftp"
)

// Teach the default HTTP transport the ftp scheme, so every http.Client in
// hapiq (and so Fetch, the cache and each downloader) can take an ftp:// URL.
func init() {
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		t.RegisterProtocol("ftp", ftpTransport{})
	}
}

// ftpTransport answers GET (RETR) and HEAD (SIZE) for ftp:// URLs with an
// http.Response: 200 with the file, or 404 when the server says 550.
// ponytail: one connection per request, files only (no directory listing),
// no Range. Pool connections or add REST if many small files get slow.
type ftpTransport struct{}

func (ftpTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return nil, errors.New("ftp: only GET and HEAD are supported")
	}
	host := req.URL.Host
	if req.URL.Port() == "" {
		host = net.JoinHostPort(host, "21")
	}
	conn, err := ftp.Dial(host, ftp.DialWithContext(req.Context()))
	if err != nil {
		return nil, err
	}
	user, pass := "anonymous", "anonymous"
	if u := req.URL.User; u != nil {
		user = u.Username()
		pass, _ = u.Password()
	}
	if err := conn.Login(user, pass); err != nil {
		_ = conn.Quit()
		return nil, err
	}

	resp := &http.Response{
		Status: "200 OK", StatusCode: http.StatusOK,
		Proto: "FTP", Header: http.Header{}, Request: req,
		Body: http.NoBody, ContentLength: -1,
	}
	notFound := func(err error) (*http.Response, error) {
		_ = conn.Quit()
		var te *textproto.Error
		if errors.As(err, &te) && te.Code == ftp.StatusFileUnavailable {
			resp.Status, resp.StatusCode = "404 Not Found", http.StatusNotFound
			return resp, nil
		}
		return nil, err
	}

	// SIZE is optional on the server side; without it the length stays unknown.
	size, sizeErr := conn.FileSize(req.URL.Path)
	if sizeErr == nil {
		resp.ContentLength = size
	}
	if req.Method == http.MethodHead {
		if sizeErr != nil {
			return notFound(sizeErr)
		}
		_ = conn.Quit()
		return resp, nil
	}

	data, err := conn.Retr(req.URL.Path)
	if err != nil {
		return notFound(err)
	}
	resp.Body = ftpBody{data, conn}
	return resp, nil
}

// ftpBody closes the control connection along with the data stream.
type ftpBody struct {
	io.ReadCloser
	conn *ftp.ServerConn
}

func (b ftpBody) Close() error {
	err := b.ReadCloser.Close()
	_ = b.conn.Quit()
	return err
}
