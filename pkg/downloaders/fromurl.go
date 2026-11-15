package downloaders

import (
	"net/url"
	"strings"
)

// FromURL maps a repository web page URL to the downloader source and ID that
// fetch it, e.g. https://www.ebi.ac.uk/biostudies/arrayexpress/studies/E-MTAB-14004
// gives ("biostudies", "E-MTAB-14004"). Any other http(s) URL maps to the
// generic "url" source with the URL unchanged; anything that is not an http(s)
// URL gives ok=false. It only parses the string: no network, no Validate.
func FromURL(raw string) (source, id string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", "", false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	// after returns the path segment following key, or "".
	after := func(key string) string {
		for i := 0; i < len(segs)-1; i++ {
			if segs[i] == key {
				return segs[i+1]
			}
		}
		return ""
	}

	switch {
	case host == "ebi.ac.uk" && segs[0] == "biostudies":
		source, id = "biostudies", after("studies")
	case host == "ebi.ac.uk" && segs[0] == "arrayexpress":
		source, id = "biostudies", after("experiments")
	case host == "ebi.ac.uk" && segs[0] == "ena":
		source, id = "sra", after("view")
	case host == "ncbi.nlm.nih.gov" && segs[0] == "geo":
		source, id = "geo", u.Query().Get("acc")
	case host == "ncbi.nlm.nih.gov" && segs[0] == "sra":
		source, id = "sra", u.Query().Get("term")
		if len(segs) > 1 {
			id = segs[1]
		}
	case host == "zenodo.org":
		source, id = "zenodo", after("records")
		if id == "" {
			id = after("record")
		}
		id = digits(id)
	case host == "doi.org" || host == "dx.doi.org":
		n, _ := strings.CutPrefix(u.Path, "/10.5281/zenodo.")
		source, id = "zenodo", digits(n)
	case (host == "figshare.com" || strings.HasSuffix(host, ".figshare.com")) && segs[0] == "articles":
		// /articles/<type>/<slug>/<id>[/<version>]: the first all-digit
		// segment is the article, as the figshare downloader's own cleaning does.
		source = "figshare"
		for _, s := range segs[1:] {
			if digits(s) != "" {
				id = s
				break
			}
		}
	case host == "huggingface.co" && segs[0] == "datasets" && len(segs) >= 3:
		source, id = "hf", segs[1]+"/"+segs[2]
	case host == "cellxgene.cziscience.com":
		source, id = "cellxgene", after("collections")
	}
	if id != "" {
		return source, id, true
	}
	return "url", raw, true
}

// digits returns s if it is a non-empty run of ASCII digits, else "".
func digits(s string) string {
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return ""
	}
	return s
}
