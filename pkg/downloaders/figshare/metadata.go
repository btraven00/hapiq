// Package figshare provides metadata extraction functionality for Figshare datasets
// including articles, collections, and projects with comprehensive API integration.
package figshare

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

// FigshareArticle represents a Figshare article from the API.
type FigshareArticle struct {
	License       FigshareLicense    `json:"license"`
	ModifiedDate  string             `json:"modified_date"`
	Description   string             `json:"description"`
	DOI           string             `json:"doi"`
	Title         string             `json:"title"`
	ResourceDOI   string             `json:"resource_doi"`
	ResourceTitle string             `json:"resource_title"`
	PublishedDate string             `json:"published_date"`
	CreatedDate   string             `json:"created_date"`
	Files         []FigshareFile     `json:"files"`
	Keywords      []string           `json:"keywords"`
	References    []string           `json:"references"`
	Tags          []string           `json:"tags"`
	Categories    []FigshareCategory `json:"categories"`
	Authors       []FigshareAuthor   `json:"authors"`
	Custom        []interface{}      `json:"custom_fields,omitempty"`
	ID            int                `json:"id"`
	Version       int                `json:"version"`
}

// FigshareCollection represents a Figshare collection from the API.
type FigshareCollection struct {
	Title       string             `json:"title"`
	Description string             `json:"description"`
	DOI         string             `json:"doi"`
	CreatedDate string             `json:"created_date"`
	Articles    []FigshareArticle  `json:"articles"`
	Authors     []FigshareAuthor   `json:"authors"`
	Categories  []FigshareCategory `json:"categories"`
	Tags        []string           `json:"tags"`
	Keywords    []string           `json:"keywords"`
	ID          int                `json:"id"`
	Version     int                `json:"version"`
}

// FigshareProject represents a Figshare project from the API.
type FigshareProject struct {
	Title        string            `json:"title"`
	Description  string            `json:"description"`
	CreatedDate  string            `json:"created_date"`
	ModifiedDate string            `json:"modified_date"`
	Articles     []FigshareArticle `json:"articles"`
	Authors      []FigshareAuthor  `json:"authors"`
	Tags         []string          `json:"tags"`
	ID           int               `json:"id"`
}

// FigshareAuthor represents an author.
type FigshareAuthor struct {
	FullName  string `json:"full_name"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	ORCID     string `json:"orcid_id"`
	ID        int    `json:"id"`
}

// FigshareCategory represents a category.
type FigshareCategory struct {
	Title    string `json:"title"`
	ID       int    `json:"id"`
	ParentID int    `json:"parent_id"`
}

// FigshareLicense represents license information.
type FigshareLicense struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Value int    `json:"value"`
}

// FigshareFile represents a file in the dataset.
type FigshareFile struct {
	Name         string `json:"name"`
	DownloadURL  string `json:"download_url"`
	MD5          string `json:"computed_md5"`
	MimeType     string `json:"mimetype"`
	CreatedDate  string `json:"created_date"`
	SuppliedMD5  string `json:"supplied_md5"`
	PreviewState string `json:"preview_state"`
	ID           int    `json:"id"`
	Size         int64  `json:"size"`
	IsLinkOnly   bool   `json:"is_link_only"`
}

// getArticleMetadata retrieves metadata for a Figshare article.
func (d *FigshareDownloader) getArticleMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	var article FigshareArticle

	endpoint := fmt.Sprintf("articles/%s", id)

	if err := d.apiRequest(ctx, endpoint, &article); err != nil {
		return nil, fmt.Errorf("failed to fetch article metadata: %w", err)
	}

	metadata := &downloaders.Metadata{
		Source:      d.GetSourceType(),
		ID:          id,
		Title:       article.Title,
		Description: article.Description,
		DOI:         article.DOI,
		License:     article.License.Name,
		Version:     strconv.Itoa(article.Version),
		FileCount:   len(article.Files),
		Custom:      make(map[string]any),
	}

	// Extract authors
	var authors []string
	for _, author := range article.Authors {
		authors = append(authors, author.FullName)
	}

	metadata.Authors = authors

	// Extract tags and keywords
	metadata.Tags = article.Tags
	metadata.Keywords = article.Keywords

	// Parse dates
	if created, err := d.parseFigshareDate(article.CreatedDate); err == nil {
		metadata.Created = created
	}

	if modified, err := d.parseFigshareDate(article.ModifiedDate); err == nil {
		metadata.LastModified = modified
	}

	// Calculate total size
	var totalSize int64
	for _, file := range article.Files {
		totalSize += file.Size
	}

	metadata.TotalSize = totalSize

	// Add custom fields
	metadata.Custom["resource_title"] = article.ResourceTitle
	metadata.Custom["resource_doi"] = article.ResourceDOI
	metadata.Custom["published_date"] = article.PublishedDate

	// Add categories
	var categories []string
	for _, cat := range article.Categories {
		categories = append(categories, cat.Title)
	}

	if len(categories) > 0 {
		metadata.Custom["categories"] = categories
	}

	// Add license details
	if article.License.URL != "" {
		metadata.Custom["license_url"] = article.License.URL
	}

	// Add files information to custom fields
	var fileInfos []map[string]interface{}

	for _, file := range article.Files {
		fileInfo := map[string]interface{}{
			"id":           file.ID,
			"name":         file.Name,
			"size":         file.Size,
			"mimetype":     file.MimeType,
			"is_link_only": file.IsLinkOnly,
		}
		if file.MD5 != "" {
			fileInfo["md5"] = file.MD5
		}

		fileInfos = append(fileInfos, fileInfo)
	}

	metadata.Custom["files"] = fileInfos

	return metadata, nil
}

// getCollectionMetadata retrieves metadata for a Figshare collection.
func (d *FigshareDownloader) getCollectionMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	var collection FigshareCollection

	endpoint := fmt.Sprintf("collections/%s", id)

	if err := d.apiRequest(ctx, endpoint, &collection); err != nil {
		return nil, fmt.Errorf("failed to fetch collection metadata: %w", err)
	}

	metadata := &downloaders.Metadata{
		Source:      d.GetSourceType(),
		ID:          id,
		Title:       collection.Title,
		Description: collection.Description,
		DOI:         collection.DOI,
		Version:     strconv.Itoa(collection.Version),
		Custom:      make(map[string]any),
	}

	// Extract authors
	var authors []string
	for _, author := range collection.Authors {
		authors = append(authors, author.FullName)
	}

	metadata.Authors = authors

	// Extract tags and keywords
	metadata.Tags = collection.Tags
	metadata.Keywords = collection.Keywords

	// Parse creation date
	if created, err := d.parseFigshareDate(collection.CreatedDate); err == nil {
		metadata.Created = created
		metadata.LastModified = created // Collections may not have separate modified date
	}

	// Calculate statistics from articles
	var totalFiles int

	var totalSize int64

	var samples []string

	for _, article := range collection.Articles {
		totalFiles += len(article.Files)

		for _, file := range article.Files {
			totalSize += file.Size
		}

		samples = append(samples, article.Title)
	}

	metadata.FileCount = totalFiles
	metadata.TotalSize = totalSize

	// Create collection information
	if len(collection.Articles) > 1 {
		collectionInfo := downloaders.Collection{
			Type:          "figshare_collection",
			ID:            id,
			Title:         collection.Title,
			FileCount:     totalFiles,
			EstimatedSize: totalSize,
			UserConfirmed: false,
			Samples:       samples,
		}
		metadata.Collections = []downloaders.Collection{collectionInfo}
	}

	// Add categories
	var categories []string
	for _, cat := range collection.Categories {
		categories = append(categories, cat.Title)
	}

	if len(categories) > 0 {
		metadata.Custom["categories"] = categories
	}

	// Add articles information
	var articleInfos []map[string]interface{}

	for _, article := range collection.Articles {
		articleInfo := map[string]interface{}{
			"id":    article.ID,
			"title": article.Title,
			"doi":   article.DOI,
			"files": len(article.Files),
		}
		articleInfos = append(articleInfos, articleInfo)
	}

	metadata.Custom["articles"] = articleInfos

	return metadata, nil
}

// getProjectMetadata retrieves metadata for a Figshare project.
func (d *FigshareDownloader) getProjectMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	var project FigshareProject

	endpoint := fmt.Sprintf("projects/%s", id)

	if err := d.apiRequest(ctx, endpoint, &project); err != nil {
		return nil, fmt.Errorf("failed to fetch project metadata: %w", err)
	}

	metadata := &downloaders.Metadata{
		Source:      d.GetSourceType(),
		ID:          id,
		Title:       project.Title,
		Description: project.Description,
		Custom:      make(map[string]any),
	}

	// Extract authors
	var authors []string
	for _, author := range project.Authors {
		authors = append(authors, author.FullName)
	}

	metadata.Authors = authors

	// Extract tags
	metadata.Tags = project.Tags

	// Parse dates
	if created, err := d.parseFigshareDate(project.CreatedDate); err == nil {
		metadata.Created = created
	}

	if modified, err := d.parseFigshareDate(project.ModifiedDate); err == nil {
		metadata.LastModified = modified
	}

	// Calculate statistics from articles
	var totalFiles int

	var totalSize int64

	var samples []string

	for _, article := range project.Articles {
		totalFiles += len(article.Files)

		for _, file := range article.Files {
			totalSize += file.Size
		}

		samples = append(samples, article.Title)
	}

	metadata.FileCount = totalFiles
	metadata.TotalSize = totalSize

	// Create collection information for projects with multiple articles
	if len(project.Articles) > 1 {
		collectionInfo := downloaders.Collection{
			Type:          "figshare_project",
			ID:            id,
			Title:         project.Title,
			FileCount:     totalFiles,
			EstimatedSize: totalSize,
			UserConfirmed: false,
			Samples:       samples,
		}
		metadata.Collections = []downloaders.Collection{collectionInfo}
	}

	// Add articles information
	var articleInfos []map[string]interface{}

	for _, article := range project.Articles {
		articleInfo := map[string]interface{}{
			"id":    article.ID,
			"title": article.Title,
			"doi":   article.DOI,
			"files": len(article.Files),
		}
		articleInfos = append(articleInfos, articleInfo)
	}

	metadata.Custom["articles"] = articleInfos

	return metadata, nil
}

// parseFigshareDate parses Figshare date formats.
func (d *FigshareDownloader) parseFigshareDate(dateStr string) (time.Time, error) {
	if dateStr == "" {
		return time.Time{}, fmt.Errorf("empty date string")
	}

	// Common Figshare date formats
	formats := []string{
		"2006-01-02T15:04:05Z",          // ISO 8601 with Z
		"2006-01-02T15:04:05.000Z",      // ISO 8601 with milliseconds
		"2006-01-02T15:04:05-07:00",     // ISO 8601 with timezone
		"2006-01-02T15:04:05.000-07:00", // ISO 8601 with milliseconds and timezone
		"2006-01-02T15:04:05",           // ISO 8601 without timezone
		"2006-01-02 15:04:05",           // Simple format
		"2006-01-02",                    // Date only
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse date: %s", dateStr)
}

// shouldDownloadFile determines if a file should be downloaded based on options.
func (d *FigshareDownloader) shouldDownloadFile(file FigshareFile, options *downloaders.DownloadOptions) bool {
	filename := strings.ToLower(file.Name)

	// Skip link-only files if they can't be downloaded directly
	if file.IsLinkOnly {
		return false
	}

	if options == nil {
		return true
	}

	// Check for raw data exclusion
	if !options.IncludeRaw {
		rawPatterns := []string{
			".fastq", ".fq", ".sra", ".bam", ".sam",
			"_raw", "raw_", ".cel", ".fcs",
		}
		for _, pattern := range rawPatterns {
			if strings.Contains(filename, pattern) {
				return false
			}
		}
	}

	// Check for supplementary exclusion
	if options.ExcludeSupplementary {
		suppPatterns := []string{
			"supplementary", "suppl", "readme", "license",
			"metadata", "manifest",
		}
		for _, pattern := range suppPatterns {
			if strings.Contains(filename, pattern) {
				return false
			}
		}
	}

	// Apply custom filters
	if options.CustomFilters != nil {
		for filterType, filterValue := range options.CustomFilters {
			switch filterType {
			case "extension":
				if !strings.HasSuffix(filename, filterValue) {
					return false
				}
			case "contains":
				if !strings.Contains(filename, filterValue) {
					return false
				}
			case "excludes":
				if strings.Contains(filename, filterValue) {
					return false
				}
			case "mimetype":
				if !strings.Contains(strings.ToLower(file.MimeType), strings.ToLower(filterValue)) {
					return false
				}
			case "max_size":
				if maxSize, err := strconv.ParseInt(filterValue, 10, 64); err == nil {
					if file.Size > maxSize {
						return false
					}
				}
			case "min_size":
				if minSize, err := strconv.ParseInt(filterValue, 10, 64); err == nil {
					if file.Size < minSize {
						return false
					}
				}
			}
		}
	}

	return true
}
