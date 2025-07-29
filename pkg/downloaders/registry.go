// Package downloaders provides registry functionality for managing different
// downloader implementations and mapping source types to their handlers.
package downloaders

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Registry manages the collection of available downloaders
type Registry struct {
	mu          sync.RWMutex
	downloaders map[string]Downloader
	aliases     map[string]string // Allow multiple names for same downloader
}

// NewRegistry creates a new downloader registry
func NewRegistry() *Registry {
	return &Registry{
		downloaders: make(map[string]Downloader),
		aliases:     make(map[string]string),
	}
}

// Register adds a downloader to the registry
func (r *Registry) Register(downloader Downloader) error {
	if downloader == nil {
		return fmt.Errorf("downloader cannot be nil")
	}

	sourceType := downloader.GetSourceType()
	if sourceType == "" {
		return fmt.Errorf("downloader must have a non-empty source type")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if already registered
	if _, exists := r.downloaders[sourceType]; exists {
		return fmt.Errorf("downloader for source type '%s' already registered", sourceType)
	}

	r.downloaders[sourceType] = downloader
	return nil
}

// RegisterAlias creates an alias for an existing downloader
func (r *Registry) RegisterAlias(alias, sourceType string) error {
	if alias == "" || sourceType == "" {
		return fmt.Errorf("alias and source type cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if target exists
	if _, exists := r.downloaders[sourceType]; !exists {
		return fmt.Errorf("source type '%s' not found", sourceType)
	}

	// Check if alias already exists
	if _, exists := r.aliases[alias]; exists {
		return fmt.Errorf("alias '%s' already registered", alias)
	}

	// Check if alias conflicts with a real source type
	if _, exists := r.downloaders[alias]; exists {
		return fmt.Errorf("alias '%s' conflicts with existing source type", alias)
	}

	r.aliases[alias] = sourceType
	return nil
}

// Get retrieves a downloader by source type or alias
func (r *Registry) Get(sourceType string) (Downloader, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Normalize source type (case-insensitive)
	normalizedType := strings.ToLower(strings.TrimSpace(sourceType))

	// Try direct lookup first
	if downloader, exists := r.downloaders[normalizedType]; exists {
		return downloader, nil
	}

	// Try alias lookup
	if realType, exists := r.aliases[normalizedType]; exists {
		if downloader, exists := r.downloaders[realType]; exists {
			return downloader, nil
		}
	}

	return nil, fmt.Errorf("no downloader registered for source type '%s'", sourceType)
}

// List returns all registered source types
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.downloaders))
	for sourceType := range r.downloaders {
		types = append(types, sourceType)
	}
	return types
}

// ListWithAliases returns all registered source types and their aliases
func (r *Registry) ListWithAliases() map[string][]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string][]string)

	// Add main source types
	for sourceType := range r.downloaders {
		result[sourceType] = []string{}
	}

	// Add aliases
	for alias, sourceType := range r.aliases {
		if _, exists := result[sourceType]; exists {
			result[sourceType] = append(result[sourceType], alias)
		}
	}

	return result
}

// Validate checks if an ID is valid for any registered downloader
func (r *Registry) Validate(ctx context.Context, sourceType, id string) (*ValidationResult, error) {
	downloader, err := r.Get(sourceType)
	if err != nil {
		return nil, err
	}

	return downloader.Validate(ctx, id)
}

// GetMetadata retrieves metadata for a dataset using the appropriate downloader
func (r *Registry) GetMetadata(ctx context.Context, sourceType, id string) (*Metadata, error) {
	downloader, err := r.Get(sourceType)
	if err != nil {
		return nil, err
	}

	return downloader.GetMetadata(ctx, id)
}

// Download performs a download using the appropriate downloader
func (r *Registry) Download(ctx context.Context, sourceType string, req *DownloadRequest) (*DownloadResult, error) {
	downloader, err := r.Get(sourceType)
	if err != nil {
		return nil, err
	}

	return downloader.Download(ctx, req)
}

// AutoDetect attempts to determine the source type from an ID
func (r *Registry) AutoDetect(ctx context.Context, id string) (string, *ValidationResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var lastErr error
	var candidates []string

	// Try each downloader to see which one accepts the ID
	for sourceType, downloader := range r.downloaders {
		result, err := downloader.Validate(ctx, id)
		if err != nil {
			lastErr = err
			continue
		}

		if result.Valid {
			return sourceType, result, nil
		}

		// Keep track of potential candidates that didn't fail validation
		// but also didn't return valid=true
		if len(result.Errors) == 0 {
			candidates = append(candidates, sourceType)
		}
	}

	// If no exact match, but we have candidates, return the first one with a warning
	if len(candidates) > 0 {
		downloader := r.downloaders[candidates[0]]
		result, err := downloader.Validate(ctx, id)
		if err != nil {
			return "", nil, err
		}
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("auto-detected as %s, but other possibilities exist: %v",
				candidates[0], candidates[1:]))
		return candidates[0], result, nil
	}

	if lastErr != nil {
		return "", nil, fmt.Errorf("no downloader could handle ID '%s': %w", id, lastErr)
	}

	return "", nil, fmt.Errorf("no downloader could handle ID '%s'", id)
}

// DefaultRegistry is the global registry instance
var DefaultRegistry = NewRegistry()

// Register adds a downloader to the default registry
func Register(downloader Downloader) error {
	return DefaultRegistry.Register(downloader)
}

// RegisterAlias creates an alias in the default registry
func RegisterAlias(alias, sourceType string) error {
	return DefaultRegistry.RegisterAlias(alias, sourceType)
}

// Get retrieves a downloader from the default registry
func Get(sourceType string) (Downloader, error) {
	return DefaultRegistry.Get(sourceType)
}

// List returns all source types from the default registry
func List() []string {
	return DefaultRegistry.List()
}

// Validate validates an ID using the default registry
func Validate(ctx context.Context, sourceType, id string) (*ValidationResult, error) {
	return DefaultRegistry.Validate(ctx, sourceType, id)
}

// GetMetadata retrieves metadata using the default registry
func GetMetadata(ctx context.Context, sourceType, id string) (*Metadata, error) {
	return DefaultRegistry.GetMetadata(ctx, sourceType, id)
}

// Download performs a download using the default registry
func Download(ctx context.Context, sourceType string, req *DownloadRequest) (*DownloadResult, error) {
	return DefaultRegistry.Download(ctx, sourceType, req)
}

// AutoDetect attempts to auto-detect source type using the default registry
func AutoDetect(ctx context.Context, id string) (string, *ValidationResult, error) {
	return DefaultRegistry.AutoDetect(ctx, id)
}
