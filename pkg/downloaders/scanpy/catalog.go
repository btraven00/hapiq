package scanpy

import (
	"fmt"
	"strings"
)

// fileSpec describes a single file to be fetched for a dataset.
type fileSpec struct {
	URL      string
	Filename string // optional override; defaults to URL basename
}

// entry describes a dataset shipped by scanpy.datasets.
//
// For fixed datasets, Files is populated directly. For parametrized datasets
// (visium_sge, ebi_expression_atlas) Files is nil and Resolve is set; it
// returns the file list for a given parameter (sample_id / accession).
type entry struct {
	Name        string
	Label       string
	Format      string
	Files       []fileSpec
	Examples    []string                            // example parameters for parametrized entries
	Resolve     func(param string) ([]fileSpec, error)
}

// catalog mirrors scanpy.datasets downloadable entries (excluding the ones
// bundled inside the scanpy package).
var catalog = map[string]entry{
	"pbmc3k": {
		Name:   "pbmc3k",
		Label:  "3k PBMCs from 10x Genomics (raw)",
		Format: "h5ad",
		Files: []fileSpec{
			{URL: "http://falexwolf.de/data/pbmc3k_raw.h5ad"},
		},
	},
	"pbmc3k_processed": {
		Name:   "pbmc3k_processed",
		Label:  "Processed 3k PBMCs (basic tutorial output)",
		Format: "h5ad",
		Files: []fileSpec{
			{URL: "https://raw.githubusercontent.com/chanzuckerberg/cellxgene/master/example-dataset/pbmc3k.h5ad"},
		},
	},
	"paul15": {
		Name:   "paul15",
		Label:  "Development of Myeloid Progenitors (Paul et al. 2015)",
		Format: "h5",
		Files: []fileSpec{
			{URL: "http://falexwolf.de/data/paul15.h5"},
		},
	},
	"moignard15": {
		Name:   "moignard15",
		Label:  "Hematopoiesis in early mouse embryos (Moignard et al. 2015)",
		Format: "xlsx",
		Files: []fileSpec{
			{URL: "http://www.nature.com/nbt/journal/v33/n3/extref/nbt.3154-S3.xlsx"},
		},
	},
	"burczynski06": {
		Name:   "burczynski06",
		Label:  "Bulk PBMC microarray, UC/CD/healthy (Burczynski et al. 2006)",
		Format: "soft.gz",
		Files: []fileSpec{
			{URL: "ftp://ftp.ncbi.nlm.nih.gov/geo/datasets/GDS1nnn/GDS1615/soft/GDS1615_full.soft.gz"},
		},
	},
	"visium_sge": {
		Name:   "visium_sge",
		Label:  "10x Visium spatial samples (parametrized by sample_id)",
		Format: "tar.gz + h5",
		Examples: []string{
			"V1_Breast_Cancer_Block_A_Section_1",
			"V1_Breast_Cancer_Block_A_Section_2",
			"V1_Human_Heart",
			"V1_Human_Lymph_Node",
			"V1_Mouse_Kidney",
			"V1_Adult_Mouse_Brain",
			"V1_Mouse_Brain_Sagittal_Posterior",
			"V1_Mouse_Brain_Sagittal_Posterior_Section_2",
			"V1_Mouse_Brain_Sagittal_Anterior",
			"V1_Mouse_Brain_Sagittal_Anterior_Section_2",
		},
		Resolve: func(sampleID string) ([]fileSpec, error) {
			if sampleID == "" {
				return nil, fmt.Errorf("visium_sge requires a sample_id (e.g. visium_sge/V1_Human_Heart)")
			}
			base := "http://cf.10xgenomics.com/samples/spatial-exp/1.0.0/" + sampleID
			return []fileSpec{
				{URL: base + "/" + sampleID + "_spatial.tar.gz"},
				{URL: base + "/" + sampleID + "_filtered_feature_bc_matrix.h5"},
			}, nil
		},
	},
	"ebi_expression_atlas": {
		Name:   "ebi_expression_atlas",
		Label:  "EBI Single Cell Expression Atlas (parametrized by accession)",
		Format: "mtx + tsv",
		Examples: []string{"E-GEOD-98816", "E-MTAB-4888"},
		Resolve: func(accession string) ([]fileSpec, error) {
			if accession == "" {
				return nil, fmt.Errorf("ebi_expression_atlas requires an accession (e.g. ebi_expression_atlas/E-GEOD-98816)")
			}
			base := "https://www.ebi.ac.uk/gxa/sc/experiment/" + accession + "/download"
			zipBase := "https://www.ebi.ac.uk/gxa/sc/experiment/" + accession + "/download/zip"
			// Mirrors scanpy.datasets.ebi_expression_atlas: experiment design,
			// cluster annotations, and the raw quantification matrix bundle.
			return []fileSpec{
				{URL: base + "?fileType=experiment-design", Filename: accession + ".experiment-design.tsv"},
				{URL: base + "?fileType=cluster", Filename: accession + ".clusters.tsv"},
				{URL: zipBase + "?fileType=quantification-raw", Filename: accession + ".quantification-raw.zip"},
			}, nil
		},
	},
}

// parseID splits a scanpy ID into (name, param). param is empty for fixed
// datasets. Accepts both "name/param" and "name:param".
func parseID(raw string) (string, string) {
	id := strings.TrimSpace(raw)
	for _, sep := range []string{"/", ":"} {
		if i := strings.Index(id, sep); i > 0 {
			return id[:i], id[i+1:]
		}
	}
	return id, ""
}

// resolveID returns the entry and its file list for the given ID.
func resolveID(raw string) (entry, []fileSpec, error) {
	name, param := parseID(raw)
	e, ok := catalog[name]
	if !ok {
		return entry{}, nil, fmt.Errorf("unknown scanpy dataset %q (use `hapiq download --list-sources` to see the catalog)", name)
	}
	if e.Resolve != nil {
		files, err := e.Resolve(param)
		if err != nil {
			return e, nil, err
		}
		return e, files, nil
	}
	if param != "" {
		return e, nil, fmt.Errorf("dataset %q does not take a parameter", name)
	}
	return e, e.Files, nil
}

// CatalogNames returns the sorted list of available dataset names.
func CatalogNames() []string {
	names := make([]string, 0, len(catalog))
	for n := range catalog {
		names = append(names, n)
	}
	return names
}
