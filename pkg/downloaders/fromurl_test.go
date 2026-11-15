package downloaders

import "testing"

func TestFromURL(t *testing.T) {
	tests := []struct {
		in, source, id string
		ok             bool
	}{
		{"https://www.ebi.ac.uk/biostudies/arrayexpress/studies/E-MTAB-14004", "biostudies", "E-MTAB-14004", true},
		{"https://ebi.ac.uk/biostudies/studies/S-BSST1502/", "biostudies", "S-BSST1502", true},
		{"https://www.ebi.ac.uk/biostudies/arrayexpress/studies/E-MTAB-14004?query=x#files", "biostudies", "E-MTAB-14004", true},
		{"https://www.ebi.ac.uk/arrayexpress/experiments/E-MTAB-8077/", "biostudies", "E-MTAB-8077", true},
		{"https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE133344", "geo", "GSE133344", true},
		{"https://zenodo.org/records/3242074", "zenodo", "3242074", true},
		{"https://zenodo.org/record/3242074/", "zenodo", "3242074", true},
		{"https://doi.org/10.5281/zenodo.3242074", "zenodo", "3242074", true},
		{"https://figshare.com/articles/dataset/some_title/12345678", "figshare", "12345678", true},
		{"https://figshare.com/articles/dataset/some_title/12345678/2", "figshare", "12345678", true},
		{"https://plus.figshare.com/articles/dataset/x/24539828", "figshare", "24539828", true},
		{"https://www.ebi.ac.uk/ena/browser/view/PRJEB12345", "sra", "PRJEB12345", true},
		{"https://www.ncbi.nlm.nih.gov/sra/SRR1234567", "sra", "SRR1234567", true},
		{"https://www.ncbi.nlm.nih.gov/sra?term=SRR1234567", "sra", "SRR1234567", true},
		{"https://huggingface.co/datasets/org/name", "hf", "org/name", true},
		{"https://huggingface.co/datasets/org/name/tree/main", "hf", "org/name", true},
		{"https://cellxgene.cziscience.com/collections/0d281971-11ea-4f85-9c0f-a3a4a7b4f888", "cellxgene", "0d281971-11ea-4f85-9c0f-a3a4a7b4f888", true},
		// Unrecognised pages and plain files fall to the url source.
		{"https://example.com/data.h5ad", "url", "https://example.com/data.h5ad", true},
		{"https://zenodo.org/communities/foo", "url", "https://zenodo.org/communities/foo", true},
		{"https://www.ebi.ac.uk/biostudies/", "url", "https://www.ebi.ac.uk/biostudies/", true},
		// Not an http(s) URL.
		{"GSE123", "", "", false},
		{"", "", "", false},
		{"ftp://ftp.example.org/x.gz", "", "", false},
	}
	for _, tt := range tests {
		source, id, ok := FromURL(tt.in)
		if source != tt.source || id != tt.id || ok != tt.ok {
			t.Errorf("FromURL(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.in, source, id, ok, tt.source, tt.id, tt.ok)
		}
	}
}
