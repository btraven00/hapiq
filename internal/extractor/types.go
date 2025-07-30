package extractor

import (
	"regexp"
	"time"

	"github.com/btraven00/hapiq/pkg/validators/domains"
)

// ExtractedLink represents a link found in a PDF document
type ExtractedLink struct {
	URL          string                          `json:"url"`
	Type         LinkType                        `json:"type"`
	Context      string                          `json:"context,omitempty"`
	Page         int                             `json:"page"`
	Position     Position                        `json:"position"`
	Confidence   float64                         `json:"confidence"`
	Section      string                          `json:"section,omitempty"`
	Validation   *ValidationResult               `json:"validation,omitempty"`
	DomainResult *domains.DomainValidationResult `json:"domain_result,omitempty"`
}

// Position represents the location of a link within a page
type Position struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// ValidationResult contains validation information for a link
type ValidationResult struct {
	IsAccessible  bool          `json:"is_accessible"`
	StatusCode    int           `json:"status_code,omitempty"`
	ContentType   string        `json:"content_type,omitempty"`
	ContentLength int64         `json:"content_length,omitempty"`
	LastModified  string        `json:"last_modified,omitempty"`
	ResponseTime  time.Duration `json:"response_time,omitempty"`
	FinalURL      string        `json:"final_url,omitempty"` // After redirects
	LastChecked   time.Time     `json:"last_checked"`
	Error         string        `json:"error,omitempty"`
	IsDataset     bool          `json:"is_dataset,omitempty"`
	DatasetScore  float64       `json:"dataset_score,omitempty"`
	RequestMethod string        `json:"request_method,omitempty"`
}

// LinkType represents the type of link found
type LinkType string

const (
	LinkTypeURL      LinkType = "url"
	LinkTypeDOI      LinkType = "doi"
	LinkTypeGeoID    LinkType = "geo_id"
	LinkTypeFigshare LinkType = "figshare"
	LinkTypeZenodo   LinkType = "zenodo"
	LinkTypeGeneric  LinkType = "generic"
)

// ExtractionResult contains the complete result of PDF link extraction
type ExtractionResult struct {
	Filename    string          `json:"filename"`
	Pages       int             `json:"pages"`
	TotalText   int             `json:"total_text"`
	Links       []ExtractedLink `json:"links"`
	Summary     ExtractionStats `json:"summary"`
	ProcessTime time.Duration   `json:"process_time"`
	Errors      []string        `json:"errors,omitempty"`
	Warnings    []string        `json:"warnings,omitempty"`
}

// ExtractionStats provides summary statistics for extracted links
type ExtractionStats struct {
	TotalLinks      int              `json:"total_links"`
	UniqueLinks     int              `json:"unique_links"`
	LinksByType     map[LinkType]int `json:"links_by_type"`
	LinksByPage     map[int]int      `json:"links_by_page"`
	ValidatedLinks  int              `json:"validated_links"`
	AccessibleLinks int              `json:"accessible_links"`
}

// LinkPattern defines a pattern for detecting specific types of links (deprecated, use ExtractionPattern)
type LinkPattern struct {
	Regex      *regexp.Regexp      `json:"-"`
	Type       LinkType            `json:"type"`
	Confidence float64             `json:"confidence"`
	Normalizer func(string) string `json:"-"`
}

// ExtractionOptions configures the link extraction process
type ExtractionOptions struct {
	ValidateLinks           bool     `json:"validate_links"`
	IncludeContext          bool     `json:"include_context"`
	ContextLength           int      `json:"context_length"`
	FilterDomains           []string `json:"filter_domains,omitempty"`
	MinConfidence           float64  `json:"min_confidence"`
	MaxLinksPerPage         int      `json:"max_links_per_page"`
	UseAccessionRecognition bool     `json:"use_accession_recognition"`
	UseConvertTokenization  bool     `json:"use_convert_tokenization"`
	ExtractPositions        bool     `json:"extract_positions"`
	Keep404s                bool     `json:"keep_404s"`
}

// DefaultExtractionOptions returns default extraction options
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
