//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/btraven00/hapiq/internal/extractor"
)

func main() {
	fmt.Println("⚡ Validation Efficiency Demo")
	fmt.Println("==============================")
	fmt.Println("Comparing validation with and without smart deduplication")
	fmt.Println()

	// Simulate a realistic extraction scenario with duplicated/corrupted DOIs
	// This is what you'd typically see from PDF extraction
	extractedLinks := []string{
		// Clean DOI (what we want)
		"https://doi.org/10.1038/s41467-021-23778-6",

		// Various corruption patterns from PDF extraction
		"https://doi.org/10.1038/s41467-021-23778-6|www.nature.com/",
		"https://doi.org/10.1038/s41467-021-23778-62",
		"https://doi.org/10.1038/s41467-021-23778-6|www.nature.com/",
		"https://doi.org/10.1038/s41467-021-23778-6ARTICLE",
		"https://doi.org/10.1038/s41467-021-23778-6|www.nature.com/",
		"https://doi.org/10.1038/s41467-021-23778-64",
		"https://doi.org/10.1038/s41467-021-23778-6|www.nature.com/",
		"https://doi.org/10.1038/s41467-021-23778-6ARTICLE",
		"https://doi.org/10.1038/s41467-021-23778-6|www.nature.com/",

		// Another DOI with similar corruption
		"https://doi.org/10.1234/example.dataset.2024",
		"https://doi.org/10.1234/example.dataset.2024|garbage",
		"https://doi.org/10.1234/example.dataset.202455",
		"https://doi.org/10.1234/example.dataset.2024SUPPLEMENTARY",

		// GEO IDs
		"GSE123456",
		"GSE123456supplementary",
		"GSE123456data",

		// Some valid URLs that should remain
		"https://zenodo.org/record/7891234",
		"https://figshare.com/articles/dataset/example/12345",
		"https://github.com/user/dataset-repo",
	}

	fmt.Printf("📥 Extracted from PDF: %d links\n", len(extractedLinks))
	printLinksSummary(extractedLinks)

	validator := extractor.NewHTTPValidator(10 * time.Second)
	ctx := context.Background()

	fmt.Println("\n📊 Scenario 1: Validation WITHOUT Smart Deduplication")
	fmt.Println("=====================================================")

	start := time.Now()

	// Simulate validation of all links (what happens without deduplication)
	fmt.Printf("Validating %d links individually...\n", len(extractedLinks))

	accessibleCount := 0
	failedCount := 0

	for i, url := range extractedLinks {
		if i < 3 { // Only validate first few for demo (to avoid too many requests)
			result, err := validator.ValidateURL(ctx, url)
			if err != nil {
				failedCount++
				fmt.Printf("   ❌ %s - Error: %v\n", truncateURL(url), err)
			} else if result.Accessible {
				accessibleCount++
				fmt.Printf("   ✅ %s - HTTP %d\n", truncateURL(url), result.StatusCode)
			} else {
				failedCount++
				fmt.Printf("   ❌ %s - HTTP %d\n", truncateURL(url), result.StatusCode)
			}
		} else {
			// Simulate results for remaining links
			if strings.Contains(url, "|") || strings.HasSuffix(url, "2") || strings.HasSuffix(url, "4") {
				failedCount++
				fmt.Printf("   ❌ %s - Would fail (corrupted)\n", truncateURL(url))
			} else {
				accessibleCount++
				fmt.Printf("   ✅ %s - Would succeed\n", truncateURL(url))
			}
		}
	}

	duration1 := time.Since(start)
	totalRequests1 := len(extractedLinks)

	fmt.Printf("\nResults: %d accessible, %d failed\n", accessibleCount, failedCount)
	fmt.Printf("Time: %v | Requests: %d\n", duration1, totalRequests1)

	fmt.Println("\n🎯 Scenario 2: Validation WITH Smart Deduplication")
	fmt.Println("==================================================")

	start = time.Now()

	// Convert to ExtractedLink structs for deduplication
	var links []extractor.ExtractedLink
	for i, url := range extractedLinks {
		confidence := calculateConfidence(url)
		linkType := determineLinkType(url)

		link := extractor.ExtractedLink{
			URL:        url,
			Type:       linkType,
			Confidence: confidence,
			Page:       i%5 + 1,
			Section:    "references",
			Context:    fmt.Sprintf("Context for %s", url),
		}
		links = append(links, link)
	}

	// Apply smart deduplication (using the enhanced logic)
	dedupedLinks := smartDeduplication(links)

	fmt.Printf("After deduplication: %d unique links (%.1f%% reduction)\n",
		len(dedupedLinks), float64(len(extractedLinks)-len(dedupedLinks))/float64(len(extractedLinks))*100)

	// Show what was deduplicated
	fmt.Println("\nDeduplicated groups:")
	showDeduplicationGroups(links, dedupedLinks)

	// Validate only the deduplicated links
	fmt.Printf("\nValidating %d deduplicated links...\n", len(dedupedLinks))

	accessibleCount2 := 0
	failedCount2 := 0

	for i, link := range dedupedLinks {
		if i < 3 { // Only validate first few for demo
			result, err := validator.ValidateURL(ctx, link.URL)
			if err != nil {
				failedCount2++
				fmt.Printf("   ❌ %s - Error: %v\n", truncateURL(link.URL), err)
			} else if result.Accessible {
				accessibleCount2++
				fmt.Printf("   ✅ %s - HTTP %d\n", truncateURL(link.URL), result.StatusCode)
			} else {
				failedCount2++
				fmt.Printf("   ❌ %s - HTTP %d\n", truncateURL(link.URL), result.StatusCode)
			}
		} else {
			// Simulate results
			if strings.Contains(link.URL, "zenodo") || strings.Contains(link.URL, "figshare") || strings.Contains(link.URL, "github") {
				accessibleCount2++
				fmt.Printf("   ✅ %s - Would succeed\n", truncateURL(link.URL))
			} else if strings.Contains(link.URL, "|") {
				failedCount2++
				fmt.Printf("   ❌ %s - Would fail (corrupted)\n", truncateURL(link.URL))
			} else {
				accessibleCount2++
				fmt.Printf("   ✅ %s - Would succeed\n", truncateURL(link.URL))
			}
		}
	}

	duration2 := time.Since(start)
	totalRequests2 := len(dedupedLinks)

	fmt.Printf("\nResults: %d accessible, %d failed\n", accessibleCount2, failedCount2)
	fmt.Printf("Time: %v | Requests: %d\n", duration2, totalRequests2)

	fmt.Println("\n📈 Efficiency Comparison")
	fmt.Println("========================")
	fmt.Printf("Without deduplication: %d requests, ~%v\n", totalRequests1, duration1)
	fmt.Printf("With deduplication:    %d requests, ~%v\n", totalRequests2, duration2)

	requestReduction := float64(totalRequests1-totalRequests2) / float64(totalRequests1) * 100
	timeReduction := float64(duration1-duration2) / float64(duration1) * 100

	fmt.Printf("\n💰 Savings:\n")
	fmt.Printf("   • %.1f%% fewer HTTP requests (%d → %d)\n", requestReduction, totalRequests1, totalRequests2)
	fmt.Printf("   • ~%.1f%% faster processing\n", timeReduction)
	fmt.Printf("   • Better accuracy (clean URLs preferred over corrupted)\n")
	fmt.Printf("   • Cleaner final results\n")

	fmt.Println("\n🔧 Implementation in hapiq:")
	fmt.Println("   The enhanced deduplication is now built into the PDF extractor.")
	fmt.Println("   Just run: ./hapiq extract --validate-links paper.pdf")
	fmt.Println("   The tool will automatically deduplicate before validation.")
}

func printLinksSummary(links []string) {
	doiCount := 0
	corruptedCount := 0
	uniqueDomains := make(map[string]int)

	for _, url := range links {
		if strings.Contains(url, "doi.org") || strings.HasPrefix(url, "doi:") {
			doiCount++
		}
		if strings.Contains(url, "|") || strings.Contains(url, "ARTICLE") ||
			strings.HasSuffix(url, "2") || strings.HasSuffix(url, "4") {
			corruptedCount++
		}

		// Extract domain
		if strings.HasPrefix(url, "http") {
			parts := strings.Split(url, "/")
			if len(parts) >= 3 {
				domain := parts[2]
				uniqueDomains[domain]++
			}
		}
	}

	fmt.Printf("   • %d DOI links\n", doiCount)
	fmt.Printf("   • %d corrupted/duplicated\n", corruptedCount)
	fmt.Printf("   • %d unique domains\n", len(uniqueDomains))
}

func calculateConfidence(url string) float64 {
	confidence := 0.95

	if strings.Contains(url, "|") {
		confidence = 0.3
	} else if strings.HasSuffix(url, "2") || strings.HasSuffix(url, "4") {
		confidence = 0.4
	} else if strings.Contains(strings.ToLower(url), "article") {
		confidence = 0.35
	} else if strings.Contains(strings.ToLower(url), "supplementary") {
		confidence = 0.25
	}

	return confidence
}

func determineLinkType(url string) extractor.LinkType {
	if strings.Contains(url, "doi.org") || strings.HasPrefix(url, "doi:") || strings.HasPrefix(url, "10.") {
		return extractor.LinkTypeDOI
	} else if strings.HasPrefix(url, "GSE") || strings.HasPrefix(url, "GSM") || strings.HasPrefix(url, "GPL") {
		return extractor.LinkTypeGeoID
	} else if strings.Contains(url, "zenodo.org") {
		return extractor.LinkTypeZenodo
	} else if strings.Contains(url, "figshare.com") {
		return extractor.LinkTypeFigshare
	}
	return extractor.LinkTypeURL
}

func smartDeduplication(links []extractor.ExtractedLink) []extractor.ExtractedLink {
	// Group by normalized identifier
	groups := make(map[string][]extractor.ExtractedLink)

	for _, link := range links {
		key := normalizeForDeduplication(link.URL, link.Type)
		groups[key] = append(groups[key], link)
	}

	var deduped []extractor.ExtractedLink
	for _, group := range groups {
		// Pick the best candidate from each group
		best := selectBestCandidate(group)
		deduped = append(deduped, best)
	}

	return deduped
}

func normalizeForDeduplication(url string, linkType extractor.LinkType) string {
	switch linkType {
	case extractor.LinkTypeDOI:
		return normalizeDOI(url)
	case extractor.LinkTypeGeoID:
		return normalizeGeoID(url)
	default:
		return normalizeURL(url)
	}
}

func normalizeDOI(url string) string {
	normalized := strings.ToLower(url)
	normalized = strings.TrimPrefix(normalized, "https://doi.org/")
	normalized = strings.TrimPrefix(normalized, "http://doi.org/")
	normalized = strings.TrimPrefix(normalized, "doi:")

	// Remove corruption
	if idx := strings.Index(normalized, "|"); idx != -1 {
		normalized = normalized[:idx]
	}
	if idx := strings.Index(strings.ToLower(normalized), "article"); idx != -1 {
		normalized = normalized[:idx]
	}
	if idx := strings.Index(strings.ToLower(normalized), "supplementary"); idx != -1 {
		normalized = normalized[:idx]
	}

	return "doi:" + normalized
}

func normalizeGeoID(url string) string {
	upper := strings.ToUpper(url)
	if strings.HasPrefix(upper, "GSE") || strings.HasPrefix(upper, "GSM") || strings.HasPrefix(upper, "GPL") {
		// Remove text after the ID
		for i, r := range upper {
			if i > 3 && (r < '0' || r > '9') {
				return "geo:" + strings.ToLower(upper[:i])
			}
		}
		return "geo:" + strings.ToLower(upper)
	}
	return "geo:" + strings.ToLower(url)
}

func normalizeURL(url string) string {
	normalized := strings.ToLower(url)
	normalized = strings.TrimPrefix(normalized, "https://")
	normalized = strings.TrimPrefix(normalized, "http://")
	return "url:" + normalized
}

func selectBestCandidate(candidates []extractor.ExtractedLink) extractor.ExtractedLink {
	if len(candidates) == 1 {
		return candidates[0]
	}

	best := candidates[0]
	bestScore := scoreCandidate(best)

	for _, candidate := range candidates[1:] {
		score := scoreCandidate(candidate)
		if score > bestScore {
			best = candidate
			bestScore = score
		}
	}

	return best
}

func scoreCandidate(link extractor.ExtractedLink) float64 {
	score := link.Confidence

	// Penalties for corruption
	if strings.Contains(link.URL, "|") {
		score -= 0.3
	}
	if strings.Contains(strings.ToLower(link.URL), "article") {
		score -= 0.2
	}
	if strings.HasSuffix(link.URL, "2") || strings.HasSuffix(link.URL, "4") {
		score -= 0.15
	}

	// Bonus for clean structure
	if strings.HasPrefix(link.URL, "https://") {
		score += 0.1
	}

	// Length penalty (shorter is better)
	score -= float64(len(link.URL)) / 1000.0

	return score
}

func showDeduplicationGroups(original, deduped []extractor.ExtractedLink) {
	// Group original links by their deduplicated key
	groups := make(map[string][]string)
	dedupedMap := make(map[string]bool)

	for _, link := range deduped {
		dedupedMap[link.URL] = true
	}

	for _, link := range original {
		key := normalizeForDeduplication(link.URL, link.Type)
		groups[key] = append(groups[key], link.URL)
	}

	groupCount := 0
	for key, urls := range groups {
		if len(urls) > 1 {
			groupCount++
			if groupCount <= 3 { // Show first 3 groups
				fmt.Printf("   Group %d (%s):\n", groupCount, key)
				for _, url := range urls {
					if dedupedMap[url] {
						fmt.Printf("     ✅ %s (selected)\n", truncateURL(url))
					} else {
						fmt.Printf("     ❌ %s (removed)\n", truncateURL(url))
					}
				}
			}
		}
	}
	if groupCount > 3 {
		fmt.Printf("   ... and %d more groups\n", groupCount-3)
	}
}

func truncateURL(url string) string {
	if len(url) > 60 {
		return url[:57] + "..."
	}
	return url
}
