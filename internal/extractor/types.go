package extractor

import (
	"regexp"
	"time"

	"github.com/btraven00/hapiq/pkg/validators/domains"
)

// ExtractedLink represents a link found in a PDF document.
type ExtractedLink struct {
	Validation   *ValidationResult               `json:"validation,omitempty"`
	DomainResult *domains.DomainValidationResult `json:"domain_result,omitempty"`
	URL          string                          `json:"url"`
	Type         LinkType                        `json:"type"`
	Context      string                          `json:"context,omitempty"`
	Section      string                          `json:"section,omitempty"`
	Position     Position                        `json:"position"`
	Page         int                             `json:"page"`
	Confidence   float64                         `json:"confidence"`
}

// Position represents the location of a link within a page.
type Position struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// ValidationResult contains validation information for a link.
type ValidationResult struct {
	LastChecked   time.Time     `json:"last_checked"`
	ContentType   string        `json:"content_type,omitempty"`
	LastModified  string        `json:"last_modified,omitempty"`
	FinalURL      string        `json:"final_url,omitempty"`
	Error         string        `json:"error,omitempty"`
	RequestMethod string        `json:"request_method,omitempty"`
	StatusCode    int           `json:"status_code"`
	ContentLength int64         `json:"content_length,omitempty"`
	ResponseTime  time.Duration `json:"response_time,omitempty"`
	DatasetScore  float64       `json:"dataset_score,omitempty"`
	IsAccessible  bool          `json:"is_accessible"`
	IsDataset     bool          `json:"is_dataset,omitempty"`
}

// LinkType represents the type of link found.
type LinkType string

const (
	LinkTypeURL      LinkType = "url"
	LinkTypeDOI      LinkType = "doi"
	LinkTypeGeoID    LinkType = "geo_id"
	LinkTypeFigshare LinkType = "figshare"
	LinkTypeZenodo   LinkType = "zenodo"
	LinkTypeGeneric  LinkType = "generic"
)

// ExtractionResult contains the complete result of PDF link extraction.
type ExtractionResult struct {
	Filename    string          `json:"filename"`
	Summary     ExtractionStats `json:"summary"`
	Links       []ExtractedLink `json:"links"`
	Errors      []string        `json:"errors,omitempty"`
	Warnings    []string        `json:"warnings,omitempty"`
	Pages       int             `json:"pages"`
	TotalText   int             `json:"total_text"`
	ProcessTime time.Duration   `json:"process_time"`
}

// ExtractionStats provides summary statistics for extracted links.
type ExtractionStats struct {
	LinksByType     map[LinkType]int `json:"links_by_type"`
	LinksByPage     map[int]int      `json:"links_by_page"`
	TotalLinks      int              `json:"total_links"`
	UniqueLinks     int              `json:"unique_links"`
	ValidatedLinks  int              `json:"validated_links"`
	AccessibleLinks int              `json:"accessible_links"`
}

// LinkPattern defines a pattern for detecting specific types of links (deprecated, use ExtractionPattern).
type LinkPattern struct {
	Regex      *regexp.Regexp      `json:"-"`
	Normalizer func(string) string `json:"-"`
	Type       LinkType            `json:"type"`
	Confidence float64             `json:"confidence"`
}

// ExtractionOptions configures the link extraction process.
type ExtractionOptions struct {
	FilterDomains           []string `json:"filter_domains,omitempty"`
	ContextLength           int      `json:"context_length"`
	MinConfidence           float64  `json:"min_confidence"`
	MaxLinksPerPage         int      `json:"max_links_per_page"`
	ValidateLinks           bool     `json:"validate_links"`
	IncludeContext          bool     `json:"include_context"`
	UseAccessionRecognition bool     `json:"use_accession_recognition"`
	UseConvertTokenization  bool     `json:"use_convert_tokenization"`
	ExtractPositions        bool     `json:"extract_positions"`
	Keep404s                bool     `json:"keep_404s"`
}

// DefaultExtractionOptions returns default extraction options.
func DefaultExtractionOptions() ExtractionOptions {
	return ExtractionOptions{
		ValidateLinks:           false,
		IncludeContext:          true,
		ContextLength:           100,
		FilterDomains:           nil,
		MinConfidence:           0.5,
		MaxLinksPerPage:         50,
		UseAccessionRecognition: true,
		UseConvertTokenization:  true,
		ExtractPositions:        false,
		Keep404s:                false,
	}
}
