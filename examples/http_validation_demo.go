package main

import (
	"context"
	"fmt"
	"time"

	"github.com/btraven00/hapiq/internal/extractor"
)

func main() {
	fmt.Println("🔗 HTTP Validation Demo")
	fmt.Println("========================")
	fmt.Println("Demonstrating browser spoofing, smart request handling, and dataset detection")
	fmt.Println()

	// Test URLs with different characteristics
	testURLs := []string{
		"https://httpbin.org/json",                           // JSON response
		"https://httpbin.org/status/404",                     // 404 error
		"https://httpbin.org/redirect/3",                     // Multiple redirects
		"https://zenodo.org/record/1234567",                  // Dataset repository (might 404 but shows pattern)
		"https://figshare.com/articles/dataset/title/123456", // Another dataset pattern
		"https://httpbin.org/delay/1",                        // Slow response
		"https://this-domain-does-not-exist.invalid",         // DNS failure
		"not-a-valid-url",                                    // Invalid URL
	}

	validator := extractor.NewHTTPValidator(10 * time.Second)
	ctx := context.Background()

	fmt.Printf("Testing %d URLs with browser spoofing and smart request handling...\n\n", len(testURLs))

	for i, url := range testURLs {
		fmt.Printf("[%d/%d] Testing: %s\n", i+1, len(testURLs), url)

		start := time.Now()
		result, err := validator.ValidateURL(ctx, url)
		duration := time.Since(start)

		if err != nil {
			fmt.Printf("   ❌ Error: %v\n", err)
			continue
		}

		// Status
		if result.Accessible {
			fmt.Printf("   ✅ Accessible (HTTP %d)", result.StatusCode)
		} else {
			fmt.Printf("   ❌ Not accessible")
			if result.StatusCode > 0 {
				fmt.Printf(" (HTTP %d)", result.StatusCode)
			}
		}

		// Request method used
		if result.RequestMethod != "" {
			fmt.Printf(" via %s", result.RequestMethod)
		}

		fmt.Printf(" [%v]\n", duration)

		// Additional details if accessible
		if result.Accessible {
			if result.ContentType != "" {
				fmt.Printf("   📄 Content-Type: %s\n", result.ContentType)
			}

			if result.ContentLength > 0 {
				fmt.Printf("   📏 Size: %s\n", formatBytes(result.ContentLength))
			}

			if len(result.RedirectChain) > 0 {
				fmt.Printf("   🔄 Redirects: %d\n", len(result.RedirectChain))
				for _, redirect := range result.RedirectChain {
					fmt.Printf("      -> %s\n", redirect)
				}
			}

			if result.FinalURL != "" && result.FinalURL != result.URL {
				fmt.Printf("   🎯 Final URL: %s\n", result.FinalURL)
			}

			// Dataset analysis
			if result.IsDataset {
				fmt.Printf("   📊 Dataset likelihood: %.1f%% ✅\n", result.DatasetScore*100)
			} else if result.DatasetScore > 0 {
				fmt.Printf("   📊 Dataset likelihood: %.1f%%\n", result.DatasetScore*100)
			}

			// Content category
			category := extractor.GetContentTypeCategory(result.ContentType)
			if category != "unknown" {
				fmt.Printf("   🏷️  Category: %s\n", category)
			}
		} else if result.Error != "" {
			fmt.Printf("   💬 Error: %s\n", result.Error)
		}

		fmt.Println()
	}

	// Test batch validation
	fmt.Println("🚀 Batch Validation Demo")
	fmt.Println("=========================")

	batchURLs := []string{
		"https://httpbin.org/json",
		"https://httpbin.org/xml",
		"https://httpbin.org/html",
		"https://httpbin.org/status/200",
	}

	fmt.Printf("Testing %d URLs concurrently...\n", len(batchURLs))

	start := time.Now()
	batchResults := validator.ValidateLinkBatch(ctx, batchURLs, 3)
	batchDuration := time.Since(start)

	fmt.Printf("Completed in %v\n\n", batchDuration)

	successCount := 0
	for url, result := range batchResults {
		status := "❌"
		if result.Accessible {
			status = "✅"
			successCount++
		}
		fmt.Printf("%s %s -> HTTP %d (%s)\n", status, url, result.StatusCode, result.RequestMethod)
	}

	fmt.Printf("\nBatch Results: %d/%d successful\n", successCount, len(batchURLs))

	// Test user agent rotation
	fmt.Println("\n🎭 User Agent Demo")
	fmt.Println("==================")

	fmt.Println("Testing user agent rotation (showing first few):")
	for i := 0; i < 3; i++ {
		validator := extractor.NewHTTPValidator(5 * time.Second)
		// Test that we can access a service that checks user agents
		result, _ := validator.ValidateURL(ctx, "https://httpbin.org/user-agent")
		if result.Accessible {
			fmt.Printf("  %d. Browser-like user agent used ✅\n", i+1)
		} else {
			fmt.Printf("  %d. User agent test failed ❌\n", i+1)
		}
	}

	fmt.Println("\n✅ HTTP Validation Demo Complete!")
	fmt.Println("\nFeatures demonstrated:")
	fmt.Println("  • Browser user-agent spoofing to avoid bot detection")
	fmt.Println("  • HEAD-first with GET fallback to minimize bandwidth")
	fmt.Println("  • Range requests to avoid downloading large files")
	fmt.Println("  • Redirect following (up to 10 hops)")
	fmt.Println("  • Dataset likelihood scoring based on content type and URL patterns")
	fmt.Println("  • Concurrent batch validation for efficiency")
	fmt.Println("  • Comprehensive error handling for network issues")
	fmt.Println("  • Response time tracking for performance analysis")
	fmt.Println("  • Content type analysis and categorization")
	fmt.Println("\nUsage in link extraction:")
	fmt.Println("  ./hapiq extract --validate-links paper.pdf")
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
