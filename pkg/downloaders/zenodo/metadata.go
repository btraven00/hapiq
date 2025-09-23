// Package zenodo provides metadata extraction functionality for Zenodo datasets
// including records, files, and comprehensive API integration.
package zenodo

import (
	"fmt"
	"strings"
	"time"
)

// ZenodoRecord represents a complete Zenodo record from the API.
type ZenodoRecord struct {
	ConceptDOI   string         `json:"conceptdoi"`
	ConceptRecID string         `json:"conceptrecid"`
	Created      string         `json:"created"`
	DOI          string         `json:"doi"`
	ID           int            `json:"id"`
	Links        ZenodoLinks    `json:"links"`
	Metadata     ZenodoMetadata `json:"metadata"`
	Modified     string         `json:"modified"`
	Owner        int            `json:"owner"`
	RecordID     string         `json:"recid"`
	Revision     int            `json:"revision"`
	State        string         `json:"state"`
	Submitted    bool           `json:"submitted"`
	Title        string         `json:"title"`
	Files        []ZenodoFile   `json:"files"`
}

// ZenodoMetadata represents the metadata section of a Zenodo record.
type ZenodoMetadata struct {
	AccessConditions   string                    `json:"access_conditions,omitempty"`
	AccessRight        string                    `json:"access_right"`
	Communities        []ZenodoCommunity         `json:"communities,omitempty"`
	Creators           []ZenodoCreator           `json:"creators"`
	Description        string                    `json:"description"`
	DOI                string                    `json:"doi"`
	Keywords           []string                  `json:"keywords,omitempty"`
	Language           string                    `json:"language,omitempty"`
	License            ZenodoLicense             `json:"license"`
	Notes              string                    `json:"notes,omitempty"`
	PublicationDate    string                    `json:"publication_date"`
	Publisher          string                    `json:"publisher,omitempty"`
	RelatedIdentifiers []ZenodoRelatedIdentifier `json:"related_identifiers,omitempty"`
	Relations          ZenodoRelations           `json:"relations,omitempty"`
	ResourceType       ZenodoResourceType        `json:"resource_type"`
	Subjects           []ZenodoSubject           `json:"subjects,omitempty"`
	Title              string                    `json:"title"`
	UploadType         string                    `json:"upload_type"`
	Version            string                    `json:"version,omitempty"`
	PreReserveDOI      ZenodoPreReserveDOI       `json:"prereserve_doi,omitempty"`
}

// ZenodoCreator represents an author/creator of a Zenodo record.
type ZenodoCreator struct {
	Affiliation string `json:"affiliation,omitempty"`
	GND         string `json:"gnd,omitempty"`
	Name        string `json:"name"`
	ORCID       string `json:"orcid,omitempty"`
}

// ZenodoLicense represents license information for a Zenodo record.
type ZenodoLicense struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	URL   string `json:"url,omitempty"`
}

// ZenodoResourceType represents the resource type information.
type ZenodoResourceType struct {
	Subtype string `json:"subtype,omitempty"`
	Title   string `json:"title,omitempty"`
	Type    string `json:"type"`
}

// ZenodoCommunity represents a community that the record belongs to.
type ZenodoCommunity struct {
	ID          string `json:"id"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// ZenodoSubject represents a subject/tag for categorization.
type ZenodoSubject struct {
	Identifier string `json:"identifier,omitempty"`
	Scheme     string `json:"scheme,omitempty"`
	Term       string `json:"term"`
}

// ZenodoRelatedIdentifier represents related identifiers like papers, datasets, etc.
type ZenodoRelatedIdentifier struct {
	Identifier string `json:"identifier"`
	Relation   string `json:"relation"`
	Scheme     string `json:"scheme"`
	Resource   string `json:"resource_type,omitempty"`
}

// ZenodoRelations represents relationships with other records.
type ZenodoRelations struct {
	Version []ZenodoVersionRelation `json:"version,omitempty"`
}

// ZenodoVersionRelation represents version relationships.
type ZenodoVersionRelation struct {
	Count  int                 `json:"count"`
	Index  int                 `json:"index"`
	IsLast bool                `json:"is_last"`
	Last   string              `json:"last,omitempty"`
	Parent ZenodoVersionParent `json:"parent,omitempty"`
}

// ZenodoVersionParent represents the parent reference in version relationships.
type ZenodoVersionParent struct {
	PidType  string `json:"pid_type"`
	PidValue string `json:"pid_value"`
}

// ZenodoPreReserveDOI represents pre-reserved DOI information.
type ZenodoPreReserveDOI struct {
	DOI   string `json:"doi"`
	RecID int    `json:"recid"`
}

// ZenodoLinks represents various links related to the record.
type ZenodoLinks struct {
	Badge        string `json:"badge,omitempty"`
	Bucket       string `json:"bucket,omitempty"`
	ConceptBadge string `json:"conceptbadge,omitempty"`
	ConceptDOI   string `json:"conceptdoi,omitempty"`
	DOI          string `json:"doi,omitempty"`
	HTML         string `json:"html,omitempty"`
	Latest       string `json:"latest,omitempty"`
	LatestHTML   string `json:"latest_html,omitempty"`
	Self         string `json:"self,omitempty"`
	Thumb250     string `json:"thumb250,omitempty"`
	Thumbs       string `json:"thumbs,omitempty"`
}

// ZenodoFile represents a file in a Zenodo record.
type ZenodoFile struct {
	Bucket   string          `json:"bucket"`
	Checksum string          `json:"checksum"`
	ID       string          `json:"id"`
	Key      string          `json:"key"`
	Links    ZenodoFileLinks `json:"links"`
	Size     int64           `json:"size"`
	Type     string          `json:"type"`
}

// ZenodoFileLinks represents links for a specific file.
type ZenodoFileLinks struct {
	Self string `json:"self"`
}

// ZenodoStats represents statistics about a record.
type ZenodoStats struct {
	Downloads       int `json:"downloads"`
	UniqueViews     int `json:"unique_views"`
	Views           int `json:"views"`
	Version         int `json:"version_downloads,omitempty"`
	UniqueDownloads int `json:"unique_downloads,omitempty"`
}

// ZenodoSearchResult represents search results from Zenodo API.
type ZenodoSearchResult struct {
	Aggregations interface{} `json:"aggregations,omitempty"`
	Hits         ZenodoHits  `json:"hits"`
	Links        ZenodoLinks `json:"links"`
	Total        int         `json:"total"`
}

// ZenodoHits represents the hits section of search results.
type ZenodoHits struct {
	Hits  []ZenodoRecord `json:"hits"`
	Total int            `json:"total"`
}

// ZenodoVersionInfo represents version information for a record.
type ZenodoVersionInfo struct {
	ConceptRecID string    `json:"conceptrecid"`
	Created      time.Time `json:"created"`
	ID           string    `json:"id"`
	Updated      time.Time `json:"updated"`
	Version      string    `json:"version"`
	IsLatest     bool      `json:"is_latest"`
}

// ZenodoDeposition represents a deposition (draft) record.
type ZenodoDeposition struct {
	ConceptRecID string         `json:"conceptrecid"`
	Created      string         `json:"created"`
	DOI          string         `json:"doi"`
	DOIURL       string         `json:"doi_url"`
	Files        []ZenodoFile   `json:"files"`
	ID           int            `json:"id"`
	Links        ZenodoLinks    `json:"links"`
	Metadata     ZenodoMetadata `json:"metadata"`
	Modified     string         `json:"modified"`
	Owner        int            `json:"owner"`
	RecordID     string         `json:"record_id"`
	State        string         `json:"state"`
	Submitted    bool           `json:"submitted"`
	Title        string         `json:"title"`
}

// ZenodoConcept represents a concept (group of versions) record.
type ZenodoConcept struct {
	ConceptDOI   string         `json:"conceptdoi"`
	ConceptRecID string         `json:"conceptrecid"`
	Created      string         `json:"created"`
	ID           int            `json:"id"`
	Latest       ZenodoRecord   `json:"latest"`
	Modified     string         `json:"modified"`
	Versions     []ZenodoRecord `json:"versions,omitempty"`
}

// ZenodoQuota represents quota information for a user.
type ZenodoQuota struct {
	MaxFileSize int64 `json:"maxfilesize"`
	QuotaSize   int64 `json:"quotasize"`
	QuotaUsed   int64 `json:"quotaused"`
}

// ZenodoError represents an error response from Zenodo API.
type ZenodoError struct {
	Message string             `json:"message"`
	Status  int                `json:"status"`
	Errors  []ZenodoFieldError `json:"errors,omitempty"`
}

// ZenodoFieldError represents field-specific validation errors.
type ZenodoFieldError struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Error implements the error interface for ZenodoError.
func (e *ZenodoError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if len(e.Errors) > 0 {
		return e.Errors[0].Message
	}
	return "unknown Zenodo API error"
}

// ZenodoAccessToken represents an access token for authenticated requests.
type ZenodoAccessToken struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresIn   int       `json:"expires_in"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	Scope       []string  `json:"scope,omitempty"`
}

// IsExpired checks if the access token has expired.
func (t *ZenodoAccessToken) IsExpired() bool {
	if t.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(t.ExpiresAt)
}

// ZenodoWebhook represents webhook configuration for notifications.
type ZenodoWebhook struct {
	ID       int                   `json:"id"`
	Event    string                `json:"event"`
	Target   string                `json:"target_url"`
	Receiver ZenodoWebhookReceiver `json:"receiver"`
}

// ZenodoWebhookReceiver represents the receiver configuration for webhooks.
type ZenodoWebhookReceiver struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ParseZenodoDate parses various Zenodo date formats into a time.Time.
func ParseZenodoDate(dateStr string) (time.Time, error) {
	if dateStr == "" {
		return time.Time{}, nil
	}

	// Common Zenodo date formats
	formats := []string{
		"2006-01-02T15:04:05.000000+00:00", // Full ISO format with microseconds
		"2006-01-02T15:04:05.000000Z",      // ISO format with microseconds and Z
		"2006-01-02T15:04:05+00:00",        // ISO format with timezone
		"2006-01-02T15:04:05Z",             // ISO format with Z
		"2006-01-02T15:04:05",              // ISO format without timezone
		"2006-01-02",                       // Date only
		"2006-01-02 15:04:05",              // Simple datetime format
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse date: %s", dateStr)
}

// GetFileExtension extracts the file extension from a filename.
func GetFileExtension(filename string) string {
	parts := strings.Split(filename, ".")
	if len(parts) < 2 {
		return ""
	}
	return "." + parts[len(parts)-1]
}

// IsArchiveFile checks if a filename represents an archive file.
func IsArchiveFile(filename string) bool {
	ext := strings.ToLower(GetFileExtension(filename))
	archiveExts := []string{".zip", ".tar", ".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tar.xz", ".txz", ".rar", ".7z"}

	for _, archiveExt := range archiveExts {
		if ext == archiveExt || strings.HasSuffix(strings.ToLower(filename), archiveExt) {
			return true
		}
	}

	return false
}

// GetMimeTypeFromExtension returns a likely MIME type based on file extension.
func GetMimeTypeFromExtension(filename string) string {
	ext := strings.ToLower(GetFileExtension(filename))

	mimeMap := map[string]string{
		".txt":  "text/plain",
		".csv":  "text/csv",
		".json": "application/json",
		".xml":  "application/xml",
		".pdf":  "application/pdf",
		".zip":  "application/zip",
		".tar":  "application/x-tar",
		".gz":   "application/gzip",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".png":  "image/png",
		".gif":  "image/gif",
		".html": "text/html",
		".htm":  "text/html",
		".md":   "text/markdown",
		".py":   "text/x-python",
		".r":    "text/x-r",
		".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		".h5":   "application/x-hdf5",
		".nc":   "application/x-netcdf",
		".mat":  "application/x-matlab-data",
	}

	if mime, exists := mimeMap[ext]; exists {
		return mime
	}

	return "application/octet-stream"
}
