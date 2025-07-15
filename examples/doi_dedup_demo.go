//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"strings"

	"github.com/btraven00/hapiq/internal/extractor"
)

func main() {
	fmt.Println("🔗 DOI Deduplication Demo")
	fmt.Println("=========================")
	fmt.Println("Demonstrating smart deduplication of corrupted DOI links from PDF extraction")
	fmt.Println()

	// Simulate the corrupted DOI links you're seeing from PDF extraction
	corruptedDOIs := []string{
		"https://doi.org/10.1038/s41467-021-23778-6",                     // Clean version (95% confidence)
		"https://doi.org/10.1038/s41467-021-23778-6|www.nature.com/",     // Corrupted with pipe
		"https://doi.org/10.1038/s41467-021-23778-62",                    // Extra digit
		"https://doi.org/10.1038/s41467-021-23778-6ARTICLE",              // Appended text
		"https://doi.org/10.1038/s41467-021-23778-64",                    // Different extra digit
		"doi:10.1038/s41467-021-23778-6",                                 // DOI format
		"10.1038/s41467-021-23778-6",                                     // Bare DOI
		"https://dx.doi.org/10.1038/s41467-021-23778-6",                  // DX prefix
		"https://doi.org/10.1038/s41467-021-23778-6Naturecommunications", // Journal name appended
	}

	// Convert to ExtractedLink structs (simulating what would come from PDF extraction)
	var links []extractor.ExtractedLink
	for i, url := range corruptedDOIs {
		confidence := 0.95
		if strings.Contains(url, "|") {
			confidence = 0.3 // Corrupted links get lower confidence
		} else if strings.HasSuffix(url, "2") || strings.HasSuffix(url, "4") {
			confidence = 0.4 // Trailing digits
		} else if strings.Contains(strings.ToLower(url), "article") {
			confidence = 0.35 // Article text appended
		} else if strings.Contains(strings.ToLower(url), "nature") {
			confidence = 0.25 // Journal name appended
		}

		link := extractor.ExtractedLink{
			URL:        url,
			Type:       extractor.LinkTypeDOI,
			Confidence: confidence,
			Page:       i%3 + 1, // Spread across different pages
			Section:    "references",
			Context:    fmt.Sprintf("Reference context for link %d", i+1),
		}
		links = append(links, link)
	}

	fmt.Printf("📚 Original extracted links (%d):\n", len(links))
	for i, link := range links {
		fmt.Printf("   %d. %s [%.1f%%, p.%d]\n", i+1, link.URL, link.Confidence*100, link.Page)
	}

	fmt.Println("\n🧹 After smart deduplication:")

	// This would normally be called internally by the extractor
	// But we'll call it directly for demonstration
	dedupedLinks := deduplicateLinksDemo(links)

	fmt.Printf("   Reduced from %d to %d links\n\n", len(links), len(dedupedLinks))

	for i, link := range dedupedLinks {
		fmt.Printf("   %d. %s [%.1f%%, p.%d] ✅\n", i+1, link.URL, link.Confidence*100, link.Page)
		fmt.Printf("      → Selected as best candidate\n")
	}

	fmt.Println("\n💡 Deduplication Logic:")
	fmt.Println("   • Groups URLs by normalized DOI identifier (10.1038/s41467-021-23778-6)")
	fmt.Println("   • Scores each candidate based on:")
	fmt.Println("     - Original confidence score")
	fmt.Println("     - URL length (shorter = better)")
	fmt.Println("     - Presence of corruption patterns (|, trailing digits, etc.)")
	fmt.Println("     - Valid DOI structure")
	fmt.Println("   • Selects the highest-scoring candidate from each group")

	// Demonstrate normalization
	fmt.Println("\n🔍 Normalization Examples:")
	testURLs := []string{
		"https://doi.org/10.1038/s41467-021-23778-6",
		"https://doi.org/10.1038/s41467-021-23778-6|garbage",
		"doi:10.1038/s41467-021-23778-6",
		"10.1038/s41467-021-23778-6",
	}

	for _, url := range testURLs {
		normalized := normalizeDOIDemo(url)
		fmt.Printf("   %s\n   → %s\n", url, normalized)
	}

	fmt.Println("\n✅ Benefits:")
	fmt.Println("   • Eliminates redundant HTTP requests during validation")
	fmt.Println("   • Improves accuracy by selecting clean URLs over corrupted ones")
	fmt.Println("   • Reduces noise in final results")
	fmt.Println("   • Works with any type of identifier (DOIs, GEO IDs, etc.)")
}

// Demo version of the deduplication logic (simplified)
func deduplicateLinksDemo(links []extractor.ExtractedLink) []extractor.ExtractedLink {
	// Group by normalized DOI
	normalizedGroups := make(map[string][]extractor.ExtractedLink)

	for _, link := range links {
		normalizedKey := normalizeDOIDemo(link.URL)
		normalizedGroups[normalizedKey] = append(normalizedGroups[normalizedKey], link)
	}

	var deduped []extractor.ExtractedLink
	seen := make(map[string]bool)

	// For each group, pick the best candidate
	for _, group := range normalizedGroups {
		if len(group) == 1 {
			link := group[0]
			if !seen[link.URL] {
				seen[link.URL] = true
				deduped = append(deduped, link)
			}
		} else {
			best := selectBestCandidateDemo(group)
			if !seen[best.URL] {
				seen[best.URL] = true
				deduped = append(deduped, best)
			}
		}
	}

	return deduped
}

// Demo version of DOI normalization
func normalizeDOIDemo(url string) string {
	// Remove DOI prefix variants
	normalized := strings.ToLower(url)
	normalized = strings.TrimPrefix(normalized, "https://doi.org/")
	normalized = strings.TrimPrefix(normalized, "http://doi.org/")
	normalized = strings.TrimPrefix(normalized, "https://dx.doi.org/")
	normalized = strings.TrimPrefix(normalized, "http://dx.doi.org/")
	normalized = strings.TrimPrefix(normalized, "doi:")
	normalized = strings.TrimSpace(normalized)

	// Extract just the DOI pattern (10.xxxx/yyyy)
	if strings.Contains(normalized, "10.") {
		// Find the DOI pattern
		start := strings.Index(normalized, "10.")
		if start != -1 {
			doiPart := normalized[start:]

			// Remove corruption patterns
			if idx := strings.Index(doiPart, "|"); idx != -1 {
				doiPart = doiPart[:idx]
			}
			if idx := strings.Index(strings.ToLower(doiPart), "article"); idx != -1 {
				doiPart = doiPart[:idx]
			}
			if idx := strings.Index(strings.ToLower(doiPart), "nature"); idx != -1 {
				doiPart = doiPart[:idx]
			}

			// For demo: just ensure we have a valid DOI structure
			if strings.Count(doiPart, "/") > 0 && len(doiPart) > 10 {
				return "doi:" + doiPart
			}
		}
	}

	return "doi:" + normalized
}

// Demo version of candidate selection
func selectBestCandidateDemo(candidates []extractor.ExtractedLink) extractor.ExtractedLink {
	if len(candidates) == 1 {
		return candidates[0]
	}

	best := candidates[0]
	bestScore := scoreLinkQualityDemo(best)

	for _, candidate := range candidates[1:] {
		score := scoreLinkQualityDemo(candidate)
		if score > bestScore {
			best = candidate
			bestScore = score
		}
	}

	return best
}

// Demo version of quality scoring
func scoreLinkQualityDemo(link extractor.ExtractedLink) float64 {
	score := link.Confidence

	// Prefer shorter URLs
	lengthPenalty := float64(len(link.URL)) / 1000.0
	score -= lengthPenalty

	// Bonus for HTTPS
	if strings.HasPrefix(link.URL, "https://") {
		score += 0.1
	}

	// Penalty for corruption patterns
	if strings.Contains(link.URL, "|") {
		score -= 0.3
	}
	if strings.Contains(strings.ToLower(link.URL), "article") {
		score -= 0.2
	}
	if strings.HasSuffix(link.URL, "2") || strings.HasSuffix(link.URL, "4") {
		score -= 0.15
	}

	// Bonus for clean DOI structure
	if strings.Count(link.URL, "/") >= 2 && !strings.Contains(link.URL, "|") {
		score += 0.1
	}

	return score
}
