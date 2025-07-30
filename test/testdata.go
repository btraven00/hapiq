package test

import (
	"net/http"
	"net/http/httptest"
	"time"
)

// TestData contains example URLs and identifiers for testing.
type TestData struct {
	ValidURLs     []URLTestCase
	ValidDOIs     []DOITestCase
	InvalidInputs []InvalidTestCase
	MockResponses []MockResponse
}

// URLTestCase represents a test case for URL validation.
type URLTestCase struct {
	URL          string
	Description  string
	Expected     URLExpectation
	ResponseCode int
	ShouldPass   bool
}

// DOITestCase represents a test case for DOI validation.
type DOITestCase struct {
	DOI         string
	Description string
	Expected    DOIExpectation
	ShouldPass  bool
}

// InvalidTestCase represents invalid inputs that should fail.
type InvalidTestCase struct {
	Input       string
	Description string
	ExpectedErr string
}

// URLExpectation defines what we expect from a URL validation.
type URLExpectation struct {
	Type              string
	ContentType       string
	ExpectedFileTypes []string
	LikelihoodScore   float64
	IsDatasetRepo     bool
}

// DOIExpectation defines what we expect from a DOI validation.
type DOIExpectation struct {
	Type            string
	Prefix          string
	IsDatasetRepo   bool
	LikelihoodScore float64
}

// MockResponse represents a mock HTTP response for testing.
type MockResponse struct {
	Headers       map[string]string
	URL           string
	ContentType   string
	Body          string
	StatusCode    int
	ContentLength int64
}

// GetTestData returns comprehensive test data for hapiq.
func GetTestData() *TestData {
	return &TestData{
		ValidURLs: []URLTestCase{
			{
				URL:          "https://zenodo.org/record/1234567",
				ResponseCode: 200,
				ShouldPass:   true,
				Description:  "Zenodo record URL",
				Expected: URLExpectation{
					Type:              "zenodo_record",
					IsDatasetRepo:     true,
					LikelihoodScore:   0.95,
					ContentType:       "text/html",
					ExpectedFileTypes: []string{"zip", "csv", "json"},
				},
			},
			{
				URL:          "https://figshare.com/articles/dataset/example_dataset/1234567",
				ResponseCode: 200,
				ShouldPass:   true,
				Description:  "Figshare dataset URL",
				Expected: URLExpectation{
					Type:              "figshare_article",
					IsDatasetRepo:     true,
					LikelihoodScore:   0.95,
					ContentType:       "text/html",
					ExpectedFileTypes: []string{"xlsx", "pdf", "zip"},
				},
			},
			{
				URL:          "https://datadryad.org/stash/dataset/doi:10.5061/dryad.example",
				ResponseCode: 200,
				ShouldPass:   true,
				Description:  "Dryad dataset URL",
				Expected: URLExpectation{
					Type:              "dryad_dataset",
					IsDatasetRepo:     true,
					LikelihoodScore:   0.95,
					ContentType:       "text/html",
					ExpectedFileTypes: []string{"csv", "txt", "r"},
				},
			},
			{
				URL:          "https://github.com/user/repository/releases/download/v1.0.0/dataset.zip",
				ResponseCode: 200,
				ShouldPass:   true,
				Description:  "GitHub release download URL",
				Expected: URLExpectation{
					Type:              "github_release",
					IsDatasetRepo:     false,
					LikelihoodScore:   0.60,
					ContentType:       "application/zip",
					ExpectedFileTypes: []string{"zip"},
				},
			},
			{
				URL:          "https://osf.io/abc123/",
				ResponseCode: 200,
				ShouldPass:   true,
				Description:  "OSF project URL",
				Expected: URLExpectation{
					Type:              "osf",
					IsDatasetRepo:     true,
					LikelihoodScore:   0.80,
					ContentType:       "text/html",
					ExpectedFileTypes: []string{"csv", "json", "xlsx"},
				},
			},
			{
				URL:          "https://dataverse.harvard.edu/dataset.xhtml?persistentId=doi:10.7910/DVN/EXAMPLE",
				ResponseCode: 200,
				ShouldPass:   true,
				Description:  "Harvard Dataverse URL",
				Expected: URLExpectation{
					Type:              "dataverse",
					IsDatasetRepo:     true,
					LikelihoodScore:   0.80,
					ContentType:       "text/html",
					ExpectedFileTypes: []string{"tab", "csv", "dta"},
				},
			},
			{
				URL:          "https://example.com/data/experiment_results.csv",
				ResponseCode: 200,
				ShouldPass:   true,
				Description:  "Generic data URL",
				Expected: URLExpectation{
					Type:              "generic",
					IsDatasetRepo:     false,
					LikelihoodScore:   0.10,
					ContentType:       "text/csv",
					ExpectedFileTypes: []string{"csv"},
				},
			},
		},

		ValidDOIs: []DOITestCase{
			{
				DOI:         "10.5281/zenodo.1234567",
				ShouldPass:  true,
				Description: "Zenodo DOI",
				Expected: DOIExpectation{
					Type:            "zenodo_doi",
					Prefix:          "10.5281",
					IsDatasetRepo:   true,
					LikelihoodScore: 0.90,
				},
			},
			{
				DOI:         "10.6084/m9.figshare.1234567",
				ShouldPass:  true,
				Description: "Figshare DOI",
				Expected: DOIExpectation{
					Type:            "figshare_doi",
					Prefix:          "10.6084",
					IsDatasetRepo:   true,
					LikelihoodScore: 0.90,
				},
			},
			{
				DOI:         "10.5061/dryad.abc123",
				ShouldPass:  true,
				Description: "Dryad DOI",
				Expected: DOIExpectation{
					Type:            "dryad_doi",
					Prefix:          "10.5061",
					IsDatasetRepo:   true,
					LikelihoodScore: 0.90,
				},
			},
			{
				DOI:         "doi:10.5281/zenodo.7890123",
				ShouldPass:  true,
				Description: "Zenodo DOI with prefix",
				Expected: DOIExpectation{
					Type:            "zenodo_doi",
					Prefix:          "10.5281",
					IsDatasetRepo:   true,
					LikelihoodScore: 0.90,
				},
			},
			{
				DOI:         "https://doi.org/10.1371/journal.pone.0123456",
				ShouldPass:  true,
				Description: "PLOS DOI with URL prefix",
				Expected: DOIExpectation{
					Type:            "plos_doi",
					Prefix:          "10.1371",
					IsDatasetRepo:   false,
					LikelihoodScore: 0.30,
				},
			},
			{
				DOI:         "10.1038/s41586-021-03456-x",
				ShouldPass:  true,
				Description: "Nature DOI",
				Expected: DOIExpectation{
					Type:            "nature_doi",
					Prefix:          "10.1038",
					IsDatasetRepo:   false,
					LikelihoodScore: 0.30,
				},
			},
		},

		InvalidInputs: []InvalidTestCase{
			{
				Input:       "",
				Description: "Empty string",
				ExpectedErr: "Invalid URL or DOI",
			},
			{
				Input:       "not-a-url-or-doi",
				Description: "Plain text",
				ExpectedErr: "Invalid URL or DOI",
			},
			{
				Input:       "http://",
				Description: "Incomplete URL",
				ExpectedErr: "URL missing host",
			},
			{
				Input:       "ftp://example.com/file.zip",
				Description: "Unsupported protocol",
				ExpectedErr: "Unsupported URL scheme",
			},
			{
				Input:       "10.123/incomplete",
				Description: "Invalid DOI prefix",
				ExpectedErr: "Invalid DOI format",
			},
			{
				Input:       "10.5281/",
				Description: "DOI missing suffix",
				ExpectedErr: "DOI suffix cannot be empty",
			},
			{
				Input:       "11.5281/zenodo.123456",
				Description: "DOI wrong prefix start",
				ExpectedErr: "Invalid DOI format",
			},
		},

		MockResponses: []MockResponse{
			{
				URL:           "https://zenodo.org/record/1234567",
				StatusCode:    200,
				ContentType:   "text/html; charset=utf-8",
				ContentLength: 15234,
				Headers: map[string]string{
					"Server":        "nginx/1.18.0",
					"Last-Modified": "Wed, 15 Mar 2023 10:30:00 GMT",
					"ETag":          "\"abc123def456\"",
				},
				Body: `<!DOCTYPE html><html><head><title>Dataset: Example Research Data</title></head><body><h1>Research Dataset</h1></body></html>`,
			},
			{
				URL:           "https://figshare.com/articles/dataset/example_dataset/1234567",
				StatusCode:    200,
				ContentType:   "text/html; charset=utf-8",
				ContentLength: 8945,
				Headers: map[string]string{
					"Server":           "Apache/2.4.41",
					"Content-Language": "en",
				},
				Body: `<!DOCTYPE html><html><head><title>Example Dataset - figshare</title></head><body><h1>Example Dataset</h1></body></html>`,
			},
			{
				URL:           "https://example.com/data/experiment_results.csv",
				StatusCode:    200,
				ContentType:   "text/csv",
				ContentLength: 52341,
				Headers: map[string]string{
					"Content-Disposition": "attachment; filename=experiment_results.csv",
				},
				Body: "id,name,value\n1,sample1,0.123\n2,sample2,0.456\n",
			},
			{
				URL:        "https://broken.example.com/dataset",
				StatusCode: 404,
				Headers: map[string]string{
					"Content-Type": "text/html",
				},
				Body: `<!DOCTYPE html><html><head><title>404 Not Found</title></head><body><h1>Not Found</h1></body></html>`,
			},
		},
	}
}

// CreateMockServer creates an HTTP test server with predefined responses.
func CreateMockServer() *httptest.Server {
	data := GetTestData()
	responseMap := make(map[string]MockResponse)

	for _, resp := range data.MockResponses {
		responseMap[resp.URL] = resp
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fullURL := "https://" + r.Host + r.URL.Path
		if r.URL.RawQuery != "" {
			fullURL += "?" + r.URL.RawQuery
		}

		if resp, exists := responseMap[fullURL]; exists {
			// Set headers
			for key, value := range resp.Headers {
				w.Header().Set(key, value)
			}

			if resp.ContentType != "" {
				w.Header().Set("Content-Type", resp.ContentType)
			}

			if resp.ContentLength > 0 {
				w.Header().Set("Content-Length", string(rune(resp.ContentLength)))
			}

			w.WriteHeader(resp.StatusCode)
			w.Write([]byte(resp.Body))
		} else {
			// Default 404 response
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("Not Found"))
		}
	})

	return httptest.NewServer(handler)
}

// Note: These should be used sparingly to avoid overwhelming external services.
var RealWorldExamples = struct {
	ZenodoRecord  string
	FigshareData  string
	DryadDataset  string
	GitHubRelease string
}{
	ZenodoRecord:  "https://zenodo.org/record/3242074", // Example: actual small dataset
	FigshareData:  "https://figshare.com/articles/dataset/Iris_flower_data_set/5975619",
	DryadDataset:  "https://datadryad.org/stash/dataset/doi:10.5061/dryad.2rqxwdc8v",
	GitHubRelease: "https://github.com/scikit-learn/scikit-learn/releases/tag/1.3.0",
}

// TimingConstraints defines expected response time constraints for testing.
var TimingConstraints = struct {
	MaxValidationTime  time.Duration
	MaxHTTPRequestTime time.Duration
	MaxDownloadTime    time.Duration
}{
	MaxValidationTime:  100 * time.Millisecond,
	MaxHTTPRequestTime: 5 * time.Second,
	MaxDownloadTime:    30 * time.Second,
}

// FileTypePatterns contains common file extensions found in datasets.
var FileTypePatterns = map[string][]string{
	"data_files": {
		"csv", "tsv", "json", "xml", "xlsx", "xls",
		"parquet", "hdf5", "h5", "mat", "rds", "sav",
	},
	"archive_files": {
		"zip", "tar.gz", "tgz", "rar", "7z", "bz2",
	},
	"code_files": {
		"py", "r", "m", "ipynb", "sh", "sql", "sas",
	},
	"documentation": {
		"txt", "md", "pdf", "docx", "readme",
	},
	"image_files": {
		"png", "jpg", "jpeg", "tiff", "svg", "gif",
	},
}

// ExpectedLikelihoodRanges defines expected likelihood score ranges.
var ExpectedLikelihoodRanges = map[string][2]float64{
	"zenodo_record":    {0.90, 1.00},
	"figshare_article": {0.90, 1.00},
	"dryad_dataset":    {0.90, 1.00},
	"zenodo_doi":       {0.85, 0.95},
	"figshare_doi":     {0.85, 0.95},
	"github_release":   {0.55, 0.70},
	"github":           {0.35, 0.50},
	"generic":          {0.05, 0.20},
}
