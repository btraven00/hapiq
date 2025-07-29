// Package figshare provides download functionality for different Figshare dataset types
// including articles, collections, and projects with comprehensive file handling.
package figshare

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
)

// downloadArticle downloads a single Figshare article with all its files
func (d *FigshareDownloader) downloadArticle(ctx context.Context, id, targetDir string, options *downloaders.DownloadOptions, result *downloaders.DownloadResult) error {
	if d.verbose {
		fmt.Printf("📄 Downloading Figshare Article: %s\n", id)
	}

	// Get article details with files
	var article FigshareArticle
	endpoint := fmt.Sprintf("articles/%s", id)
	if err := d.apiRequest(ctx, endpoint, &article); err != nil {
		return fmt.Errorf("failed to fetch article details: %w", err)
	}

	if len(article.Files) == 0 {
		result.Warnings = append(result.Warnings, "no files found in article")
		return nil
	}

	// Create metadata file
	if err := d.saveArticleMetadata(article, targetDir); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to save metadata: %v", err))
	}

	// Initialize progress tracker
	totalFiles := 0
	totalSize := int64(0)
	for _, file := range article.Files {
		if options == nil || d.shouldDownloadFile(file, options) {
			totalFiles++
			totalSize += file.Size
		}
	}

	var progressTracker *common.ProgressTracker
	if d.verbose && totalFiles > 0 {
		progressTracker = common.NewProgressTracker(totalFiles, totalSize, nil, d.verbose)
	}

	// Download each file
	for _, file := range article.Files {
		// Skip if file should be filtered out
		if options != nil && !d.shouldDownloadFile(file, options) {
			if d.verbose {
				fmt.Printf("🚫 Filtering out file: %s\n", file.Name)
			}
			continue
		}

		// Skip if file exists and skip_existing is enabled
		targetPath := filepath.Join(targetDir, common.SanitizeFilename(file.Name))
		if options != nil && options.SkipExisting {
			if _, err := os.Stat(targetPath); err == nil {
				if d.verbose {
					fmt.Printf("⏭️  Skipping existing file: %s\n", file.Name)
				}
				if progressTracker != nil {
					progressTracker.SkipFile(file.Name, "file already exists")
				}
				continue
			}
		}

		if d.verbose {
			fmt.Printf("⬇️  Downloading: %s (%s)\n", file.Name, common.FormatBytes(file.Size))
		}

		// Start file tracking
		if progressTracker != nil {
			progressTracker.StartFile(file.Name, file.Size)
		}

		fileInfo, err := d.downloadFileWithProgress(ctx, file.DownloadURL, targetPath, file.Name, file.Size, progressTracker)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to download %s: %v", file.Name, err))
			if progressTracker != nil {
				progressTracker.FailFile(file.Name, err)
			}
			continue
		}

		// Use the original filename from Figshare
		fileInfo.OriginalName = file.Name

		// Verify checksum if available
		if file.MD5 != "" && fileInfo.Checksum != "" {
			// Convert SHA256 to MD5 for comparison (simplified - in practice you'd compute MD5)
			if d.verbose {
				fmt.Printf("✅ Checksum available for: %s\n", file.Name)
			}
			// Note: We could implement MD5 verification here
		}

		// Make path relative to target directory
		relPath, err := filepath.Rel(targetDir, fileInfo.Path)
		if err != nil {
			relPath = fileInfo.Path
		}
		fileInfo.Path = relPath

		result.Files = append(result.Files, *fileInfo)
	}

	return nil
}

// downloadCollection downloads a Figshare collection with all its articles
func (d *FigshareDownloader) downloadCollection(ctx context.Context, id, targetDir string, options *downloaders.DownloadOptions, result *downloaders.DownloadResult) error {
	if d.verbose {
		fmt.Printf("📚 Downloading Figshare Collection: %s\n", id)
	}

	// Get collection details
	var collection FigshareCollection
	endpoint := fmt.Sprintf("collections/%s", id)
	if err := d.apiRequest(ctx, endpoint, &collection); err != nil {
		return fmt.Errorf("failed to fetch collection details: %w", err)
	}

	if len(collection.Articles) == 0 {
		result.Warnings = append(result.Warnings, "no articles found in collection")
		return nil
	}

	// Create collection metadata directory
	metadataDir := filepath.Join(targetDir, "metadata")
	if err := common.EnsureDirectory(metadataDir); err != nil {
		return fmt.Errorf("failed to create metadata directory: %w", err)
	}

	// Save collection metadata
	if err := d.saveCollectionMetadata(collection, metadataDir); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to save collection metadata: %v", err))
	}

	// Create articles directory
	articlesDir := filepath.Join(targetDir, "articles")
	if err := common.EnsureDirectory(articlesDir); err != nil {
		return fmt.Errorf("failed to create articles directory: %w", err)
	}

	// Download each article
	for i, article := range collection.Articles {
		if d.verbose {
			fmt.Printf("📄 [%d/%d] Processing article: %s\n", i+1, len(collection.Articles), article.Title)
		}

		// Create article subdirectory
		articleDirName := fmt.Sprintf("%d_%s", article.ID, common.SanitizeFilename(article.Title))
		if len(articleDirName) > 100 { // Limit directory name length
			articleDirName = articleDirName[:100]
		}
		articleDir := filepath.Join(articlesDir, articleDirName)

		if err := common.EnsureDirectory(articleDir); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to create article directory for %s: %v", article.Title, err))
			continue
		}

		// Download the article
		articleID := fmt.Sprintf("%d", article.ID)
		if err := d.downloadArticle(ctx, articleID, articleDir, options, result); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to download article %s: %v", article.Title, err))
			continue
		}
	}

	return nil
}

// downloadProject downloads a Figshare project with all its articles
func (d *FigshareDownloader) downloadProject(ctx context.Context, id, targetDir string, options *downloaders.DownloadOptions, result *downloaders.DownloadResult) error {
	if d.verbose {
		fmt.Printf("🎯 Downloading Figshare Project: %s\n", id)
	}

	// Get project details
	var project FigshareProject
	endpoint := fmt.Sprintf("projects/%s", id)
	if err := d.apiRequest(ctx, endpoint, &project); err != nil {
		return fmt.Errorf("failed to fetch project details: %w", err)
	}

	if len(project.Articles) == 0 {
		result.Warnings = append(result.Warnings, "no articles found in project")
		return nil
	}

	// Create project metadata directory
	metadataDir := filepath.Join(targetDir, "metadata")
	if err := common.EnsureDirectory(metadataDir); err != nil {
		return fmt.Errorf("failed to create metadata directory: %w", err)
	}

	// Save project metadata
	if err := d.saveProjectMetadata(project, metadataDir); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to save project metadata: %v", err))
	}

	// Create articles directory
	articlesDir := filepath.Join(targetDir, "articles")
	if err := common.EnsureDirectory(articlesDir); err != nil {
		return fmt.Errorf("failed to create articles directory: %w", err)
	}

	// Download each article
	for i, article := range project.Articles {
		if d.verbose {
			fmt.Printf("📄 [%d/%d] Processing article: %s\n", i+1, len(project.Articles), article.Title)
		}

		// Create article subdirectory
		articleDirName := fmt.Sprintf("%d_%s", article.ID, common.SanitizeFilename(article.Title))
		if len(articleDirName) > 100 { // Limit directory name length
			articleDirName = articleDirName[:100]
		}
		articleDir := filepath.Join(articlesDir, articleDirName)

		if err := common.EnsureDirectory(articleDir); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to create article directory for %s: %v", article.Title, err))
			continue
		}

		// Download the article
		articleID := fmt.Sprintf("%d", article.ID)
		if err := d.downloadArticle(ctx, articleID, articleDir, options, result); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to download article %s: %v", article.Title, err))
			continue
		}
	}

	return nil
}

// saveArticleMetadata saves article metadata to a JSON file
func (d *FigshareDownloader) saveArticleMetadata(article FigshareArticle, targetDir string) error {
	metadataPath := filepath.Join(targetDir, "article_metadata.json")

	file, err := os.Create(metadataPath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := common.NewJSONEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(article)
}

// saveCollectionMetadata saves collection metadata to a JSON file
func (d *FigshareDownloader) saveCollectionMetadata(collection FigshareCollection, targetDir string) error {
	metadataPath := filepath.Join(targetDir, "collection_metadata.json")

	file, err := os.Create(metadataPath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := common.NewJSONEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(collection)
}

// saveProjectMetadata saves project metadata to a JSON file
func (d *FigshareDownloader) saveProjectMetadata(project FigshareProject, targetDir string) error {
	metadataPath := filepath.Join(targetDir, "project_metadata.json")

	file, err := os.Create(metadataPath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := common.NewJSONEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(project)
}

// getArticleFiles retrieves the file list for an article
func (d *FigshareDownloader) getArticleFiles(ctx context.Context, articleID int) ([]FigshareFile, error) {
	var files []FigshareFile
	endpoint := fmt.Sprintf("articles/%d/files", articleID)

	if err := d.apiRequest(ctx, endpoint, &files); err != nil {
		return nil, fmt.Errorf("failed to fetch article files: %w", err)
	}

	return files, nil
}

// downloadFileWithProgress downloads a file with progress tracking
func (d *FigshareDownloader) downloadFileWithProgress(ctx context.Context, url, targetPath, filename string, size int64, tracker *common.ProgressTracker) (*downloaders.FileInfo, error) {
	// Use the existing downloadFile method if no progress tracking needed
	if tracker == nil {
		return d.downloadFile(ctx, url, targetPath)
	}

	// Download with progress tracking
	return d.downloadFileWithProgressTracking(ctx, url, targetPath, filename, size, tracker)
}

// estimateDownloadSize calculates the total size of files to be downloaded
func (d *FigshareDownloader) estimateDownloadSize(files []FigshareFile, options *downloaders.DownloadOptions) int64 {
	var totalSize int64

	for _, file := range files {
		if options == nil || d.shouldDownloadFile(file, options) {
			totalSize += file.Size
		}
	}

	return totalSize
}

// sanitizeArticleTitle creates a safe directory name from article title
func (d *FigshareDownloader) sanitizeArticleTitle(title string) string {
	// Remove/replace problematic characters
	sanitized := strings.ReplaceAll(title, "/", "_")
	sanitized = strings.ReplaceAll(sanitized, "\\", "_")
	sanitized = strings.ReplaceAll(sanitized, ":", "_")
	sanitized = strings.ReplaceAll(sanitized, "*", "_")
	sanitized = strings.ReplaceAll(sanitized, "?", "_")
	sanitized = strings.ReplaceAll(sanitized, "\"", "_")
	sanitized = strings.ReplaceAll(sanitized, "<", "_")
	sanitized = strings.ReplaceAll(sanitized, ">", "_")
	sanitized = strings.ReplaceAll(sanitized, "|", "_")

	// Trim whitespace and limit length
	sanitized = strings.TrimSpace(sanitized)
	if len(sanitized) > 50 {
		sanitized = sanitized[:50]
	}

	// Ensure it's not empty
	if sanitized == "" {
		sanitized = "untitled"
	}

	return sanitized
}
