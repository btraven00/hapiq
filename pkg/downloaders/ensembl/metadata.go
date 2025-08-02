// Package ensembl provides metadata handling for Ensembl Genomes datasets.
package ensembl

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

// getEnsemblMetadata retrieves comprehensive metadata for an Ensembl dataset.
func (d *EnsemblDownloader) getEnsemblMetadata(ctx context.Context, req *EnsemblRequest) (*downloaders.Metadata, error) {
	// Build species list URL to validate the database and version exist
	speciesListURL := fmt.Sprintf("%s/release-%s/%s/species_Ensembl%s.txt",
		d.ftpBaseURL, req.Version, req.Database, strings.Title(string(req.Database)))

	// Validate that the database/version combination exists
	exists, err := d.validateDatabaseExists(ctx, speciesListURL)
	if err != nil {
		return nil, fmt.Errorf("failed to validate database: %w", err)
	}

	if !exists {
		return nil, &downloaders.DownloaderError{
			Type:    "not_found",
			Message: fmt.Sprintf("Ensembl %s release %s not found", req.Database, req.Version),
			Source:  d.GetSourceType(),
			ID:      fmt.Sprintf("%s:%s:%s", req.Database, req.Version, req.Content),
		}
	}

	// Get release information
	releaseInfo, err := d.getReleaseInfo(ctx, req)
	if err != nil {
		// Don't fail for missing release info, use defaults
		releaseInfo = &ReleaseInfo{
			Version:     req.Version,
			ReleaseDate: time.Now(),
			Description: fmt.Sprintf("Ensembl %s Release %s", strings.Title(string(req.Database)), req.Version),
		}
	}

	// Estimate species count and file sizes
	speciesCount, err := d.estimateSpeciesCount(ctx, req.Database)
	if err != nil {
		// Use conservative defaults if estimation fails
		speciesCount = d.getDefaultSpeciesCount(req.Database)
	}

	// Calculate file count and size estimates based on content type and species
	fileCount, estimatedSize := d.calculateEstimates(req, speciesCount)

	// If specific species requested, adjust estimates
	if req.Species != "" {
		fileCount = 1
		estimatedSize = estimatedSize / int64(speciesCount) // Per-species estimate
	}

	// Build comprehensive metadata
	metadata := &downloaders.Metadata{
		ID:           fmt.Sprintf("%s:%s:%s", req.Database, req.Version, req.Content),
		Title:        d.buildTitle(req),
		Description:  d.buildDescription(req, releaseInfo),
		Source:       d.GetSourceType(),
		Version:      req.Version,
		License:      "Apache License 2.0",
		Authors:      []string{"Ensembl Genomes Team", "European Bioinformatics Institute"},
		Tags:         d.buildTags(req),
		Keywords:     d.buildKeywords(req),
		TotalSize:    estimatedSize,
		FileCount:    fileCount,
		Created:      releaseInfo.ReleaseDate,
		LastModified: releaseInfo.ReleaseDate,
		DOI:          d.buildDOI(req),
		Custom: map[string]any{
			"database_type":     string(req.Database),
			"content_type":      string(req.Content),
			"release_version":   req.Version,
			"species_filter":    req.Species,
			"estimated_species": speciesCount,
			"ftp_base_url":      d.ftpBaseURL,
			"api_documentation": "https://rest.ensembl.org/",
		},
	}

	// Add collection information for bulk downloads
	if req.Species == "" {
		metadata.Collections = []downloaders.Collection{
			{
				Type:          fmt.Sprintf("ensembl_%s_%s", req.Database, req.Content),
				ID:            fmt.Sprintf("%s_%s_%s", req.Database, req.Version, req.Content),
				Title:         fmt.Sprintf("All %s species - %s data", req.Database, req.Content),
				FileCount:     fileCount,
				EstimatedSize: estimatedSize,
				UserConfirmed: false,
				Samples:       d.buildSampleList(req, speciesCount),
			},
		}
	}

	return metadata, nil
}

// ReleaseInfo contains information about an Ensembl release.
type ReleaseInfo struct {
	Version     string
	ReleaseDate time.Time
	Description string
	Statistics  map[string]int
}

// validateDatabaseExists checks if the specified database and version exist.
func (d *EnsemblDownloader) validateDatabaseExists(ctx context.Context, url string) (bool, error) {
	resp, err := d.protoClient.Head(ctx, url)
	if err != nil {
		return false, err
	}

	return resp.StatusCode == 200, nil
}

// getReleaseInfo retrieves release-specific information.
func (d *EnsemblDownloader) getReleaseInfo(ctx context.Context, req *EnsemblRequest) (*ReleaseInfo, error) {
	// In a full implementation, this would parse release notes or metadata files
	// For now, we'll construct basic release info

	version, err := strconv.Atoi(req.Version)
	if err != nil {
		version = 50 // Default fallback
	}

	// Estimate release date based on version (Ensembl releases roughly every 3 months)
	baseDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	releaseDate := baseDate.AddDate(0, (version-47)*3, 0) // Version 47 was around 2020

	return &ReleaseInfo{
		Version:     req.Version,
		ReleaseDate: releaseDate,
		Description: fmt.Sprintf("Ensembl Genomes %s Release %s", strings.Title(string(req.Database)), req.Version),
		Statistics:  make(map[string]int),
	}, nil
}

// estimateSpeciesCount estimates the number of species in a database.
func (d *EnsemblDownloader) estimateSpeciesCount(ctx context.Context, database DatabaseType) (int, error) {
	// This would ideally parse the actual species list, but for now use estimates
	return d.getDefaultSpeciesCount(database), nil
}

// getDefaultSpeciesCount returns default species counts based on historical data.
func (d *EnsemblDownloader) getDefaultSpeciesCount(database DatabaseType) int {
	estimates := map[DatabaseType]int{
		DatabaseBacteria: 50000, // Bacteria has the most species
		DatabaseFungi:    1000,
		DatabaseMetazoa:  500,
		DatabasePlants:   100,
		DatabaseProtists: 200,
	}

	if count, exists := estimates[database]; exists {
		return count
	}
	return 100 // Conservative default
}

// calculateEstimates calculates file count and size estimates.
func (d *EnsemblDownloader) calculateEstimates(req *EnsemblRequest, speciesCount int) (int, int64) {
	var estimatedFileCount int
	var estimatedSize int64

	// Base file count is usually one file per species
	estimatedFileCount = speciesCount

	// Size estimates per species based on content type and database
	var sizePerSpecies int64

	switch req.Content {
	case ContentPeptides:
		sizePerSpecies = d.getPeptideSizeEstimate(req.Database)
	case ContentCDS:
		sizePerSpecies = d.getCDSSizeEstimate(req.Database)
	case ContentGFF3:
		sizePerSpecies = d.getGFF3SizeEstimate(req.Database)
	case ContentDNA:
		sizePerSpecies = d.getDNASizeEstimate(req.Database)
	default:
		sizePerSpecies = 50 * 1024 * 1024 // 50MB default
	}

	estimatedSize = int64(estimatedFileCount) * sizePerSpecies

	return estimatedFileCount, estimatedSize
}

// Size estimation helpers - using methods from download.go

// buildTitle creates a descriptive title for the dataset.
func (d *EnsemblDownloader) buildTitle(req *EnsemblRequest) string {
	contentDesc := map[ContentType]string{
		ContentPeptides: "Protein Sequences",
		ContentCDS:      "Coding Sequences",
		ContentGFF3:     "Genome Annotations",
		ContentDNA:      "Genomic DNA",
	}

	title := fmt.Sprintf("Ensembl %s Release %s - %s",
		strings.Title(string(req.Database)),
		req.Version,
		contentDesc[req.Content])

	if req.Species != "" {
		title += fmt.Sprintf(" (%s)", req.Species)
	}

	return title
}

// buildDescription creates a detailed description.
func (d *EnsemblDownloader) buildDescription(req *EnsemblRequest, releaseInfo *ReleaseInfo) string {
	contentDesc := map[ContentType]string{
		ContentPeptides: "protein sequences",
		ContentCDS:      "coding DNA sequences",
		ContentGFF3:     "genome annotations in GFF3 format",
		ContentDNA:      "genomic DNA sequences",
	}

	desc := fmt.Sprintf("Ensembl Genomes %s database release %s containing %s. %s",
		req.Database, req.Version, contentDesc[req.Content], releaseInfo.Description)

	if req.Species != "" {
		desc += fmt.Sprintf(" Filtered for species: %s.", req.Species)
	}

	return desc
}

// buildTags creates relevant tags for the dataset.
func (d *EnsemblDownloader) buildTags(req *EnsemblRequest) []string {
	tags := []string{
		"genomics",
		"bioinformatics",
		"ensembl",
		string(req.Database),
		string(req.Content),
		fmt.Sprintf("release-%s", req.Version),
	}

	if req.Species != "" {
		tags = append(tags, req.Species)
	}

	return tags
}

// buildKeywords creates keywords for search and discovery.
func (d *EnsemblDownloader) buildKeywords(req *EnsemblRequest) []string {
	keywords := []string{
		"ensembl",
		"genomes",
		"genomics",
		"bioinformatics",
		"ebi",
		"embl",
		string(req.Database),
		string(req.Content),
	}

	// Add content-specific keywords
	switch req.Content {
	case ContentPeptides:
		keywords = append(keywords, "proteins", "peptides", "amino acids")
	case ContentCDS:
		keywords = append(keywords, "coding sequences", "genes", "transcripts")
	case ContentGFF3:
		keywords = append(keywords, "annotations", "features", "gff", "gff3")
	case ContentDNA:
		keywords = append(keywords, "dna", "genome", "chromosomes", "scaffolds")
	}

	return keywords
}

// buildDOI creates a DOI reference if available.
func (d *EnsemblDownloader) buildDOI(req *EnsemblRequest) string {
	// Ensembl doesn't typically assign DOIs to individual releases,
	// but we could reference the main Ensembl publication
	return "10.1093/nar/gkz966" // Ensembl 2020 paper
}

// buildSampleList creates a preview list of species for collection display.
func (d *EnsemblDownloader) buildSampleList(req *EnsemblRequest, speciesCount int) []string {
	// This would ideally come from parsing the actual species list
	// For now, provide representative examples

	samples := map[DatabaseType][]string{
		DatabaseBacteria: {
			"Escherichia coli",
			"Bacillus subtilis",
			"Salmonella enterica",
			"Pseudomonas aeruginosa",
			"Mycobacterium tuberculosis",
		},
		DatabaseFungi: {
			"Saccharomyces cerevisiae",
			"Candida albicans",
			"Aspergillus fumigatus",
			"Neurospora crassa",
			"Schizosaccharomyces pombe",
		},
		DatabaseMetazoa: {
			"Drosophila melanogaster",
			"Caenorhabditis elegans",
			"Anopheles gambiae",
			"Aedes aegypti",
			"Tribolium castaneum",
		},
		DatabasePlants: {
			"Arabidopsis thaliana",
			"Oryza sativa",
			"Zea mays",
			"Triticum aestivum",
			"Solanum lycopersicum",
		},
		DatabaseProtists: {
			"Plasmodium falciparum",
			"Leishmania major",
			"Trypanosoma brucei",
			"Dictyostelium discoideum",
			"Paramecium tetraurelia",
		},
	}

	if speciesExamples, exists := samples[req.Database]; exists {
		maxSamples := 10
		if len(speciesExamples) < maxSamples {
			return speciesExamples
		}
		return speciesExamples[:maxSamples]
	}

	return []string{"Various species available"}
}
