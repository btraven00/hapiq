package sharepoint

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsShareURL(t *testing.T) {
	cases := map[string]bool{
		"https://hopebio2020.sharepoint.com/:f:/s/PublicSharedfiles/IgBlEJ72": true,
		"https://tenant.sharepoint.com/:b:/s/Site/AbCdEf":                      true,
		"https://tenant.sharepoint.com/sites/Site/Shared%20Documents/x.bin":   false,
		"https://example.com/:f:/s/Site/token":                                false,
		"not a url ::::":                                                       false,
	}
	for in, want := range cases {
		if got := IsShareURL(in); got != want {
			t.Errorf("IsShareURL(%q) = %v, want %v", in, got, want)
		}
	}
}

// fakeSharePoint stands in for the tenant: the share-link path 302-redirects to
// the AllItems listing page carrying the folder's server-relative id.
func fakeSharePoint(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/:f:/s/PublicSharedfiles/TOKEN" {
			http.Redirect(w, r,
				"/sites/PublicSharedfiles/Shared%20Documents/Forms/AllItems.aspx"+
					"?id=%2Fsites%2FPublicSharedfiles%2FShared%20Documents%2FPublic%20Shared%20files&p=true",
				http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("<html>listing</html>"))
	}))
}

func TestResolveFolderLinkWithFragment(t *testing.T) {
	srv := fakeSharePoint(t)
	defer srv.Close()

	share := srv.URL + "/:f:/s/PublicSharedfiles/TOKEN#models.ckpt"
	dl, name, err := Resolve(context.Background(), srv.Client(), share)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if name != "models.ckpt" {
		t.Errorf("filename = %q, want models.ckpt", name)
	}
	want := srv.URL + "/sites/PublicSharedfiles/Shared%20Documents/Public%20Shared%20files/models.ckpt?download=1"
	if dl != want {
		t.Errorf("downloadURL =\n  %q\nwant\n  %q", dl, want)
	}
}
