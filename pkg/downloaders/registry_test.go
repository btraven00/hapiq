// Package downloaders provides unit tests for the downloader registry
// functionality including registration, retrieval, and auto-detection.
package downloaders

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// MockDownloader implements Downloader interface for testing
type MockDownloader struct {
	sourceType string
	validIDs   map[string]bool
	metadata   map[string]*Metadata
}

func NewMockDownloader(sourceType string) *MockDownloader {
	return &MockDownloader{
		sourceType: sourceType,
		validIDs:   make(map[string]bool),
		metadata:   make(map[string]*Metadata),
	}
}

func (m *MockDownloader) GetSourceType() string {
	return m.sourceType
}

func (m *MockDownloader) Validate(ctx context.Context, id string) (*ValidationResult, error) {
	result := &ValidationResult{
		ID:         id,
		SourceType: m.sourceType,
		Valid:      m.validIDs[id],
		Errors:     []string{},
		Warnings:   []string{},
	}

	if !result.Valid {
		result.Errors = append(result.Errors, "invalid ID for mock downloader")
	}

	return result, nil
}

func (m *MockDownloader) GetMetadata(ctx context.Context, id string) (*Metadata, error) {
	if metadata, exists := m.metadata[id]; exists {
		return metadata, nil
	}

	return &Metadata{
		Source: m.sourceType,
		ID:     id,
		Title:  "Mock Dataset " + id,
	}, nil
}

func (m *MockDownloader) Download(ctx context.Context, req *DownloadRequest) (*DownloadResult, error) {
	return &DownloadResult{
		Success:  true,
		Files:    []FileInfo{},
		Duration: 1 * time.Second,
	}, nil
}

// AddValidID adds a valid ID for testing
func (m *MockDownloader) AddValidID(id string) {
	m.validIDs[id] = true
}

// AddMetadata adds metadata for testing
func (m *MockDownloader) AddMetadata(id string, metadata *Metadata) {
	m.metadata[id] = metadata
}

func TestRegistry_Register(t *testing.T) {
	registry := NewRegistry()

	// Test successful registration
	mockDownloader := NewMockDownloader("test")
	err := registry.Register(mockDownloader)
	if err != nil {
		t.Fatalf("Expected successful registration, got error: %v", err)
	}

	// Test duplicate registration
	err = registry.Register(mockDownloader)
	if err == nil {
		t.Fatal("Expected error for duplicate registration, got nil")
	}

	// Test nil downloader
	err = registry.Register(nil)
	if err == nil {
		t.Fatal("Expected error for nil downloader, got nil")
	}

	// Test empty source type
	emptyDownloader := NewMockDownloader("")
	err = registry.Register(emptyDownloader)
	if err == nil {
		t.Fatal("Expected error for empty source type, got nil")
	}
}

func TestRegistry_RegisterAlias(t *testing.T) {
	registry := NewRegistry()
	mockDownloader := NewMockDownloader("test")

	// Register main downloader first
	err := registry.Register(mockDownloader)
	if err != nil {
		t.Fatalf("Failed to register downloader: %v", err)
	}

	// Test successful alias registration
	err = registry.RegisterAlias("alias", "test")
	if err != nil {
		t.Fatalf("Expected successful alias registration, got error: %v", err)
	}

	// Test duplicate alias
	err = registry.RegisterAlias("alias", "test")
	if err == nil {
		t.Fatal("Expected error for duplicate alias, got nil")
	}

	// Test alias for non-existent source
	err = registry.RegisterAlias("alias2", "nonexistent")
	if err == nil {
		t.Fatal("Expected error for non-existent source, got nil")
	}

	// Test empty parameters
	err = registry.RegisterAlias("", "test")
	if err == nil {
		t.Fatal("Expected error for empty alias, got nil")
	}

	err = registry.RegisterAlias("alias3", "")
	if err == nil {
		t.Fatal("Expected error for empty source type, got nil")
	}

	// Test alias conflicting with real source type
	mockDownloader2 := NewMockDownloader("conflict")
	err = registry.Register(mockDownloader2)
	if err != nil {
		t.Fatalf("Failed to register second downloader: %v", err)
	}

	err = registry.RegisterAlias("conflict", "test")
	if err == nil {
		t.Fatal("Expected error for alias conflicting with source type, got nil")
	}
}

func TestRegistry_Get(t *testing.T) {
	registry := NewRegistry()
	mockDownloader := NewMockDownloader("test")

	err := registry.Register(mockDownloader)
	if err != nil {
		t.Fatalf("Failed to register downloader: %v", err)
	}

	err = registry.RegisterAlias("alias", "test")
	if err != nil {
		t.Fatalf("Failed to register alias: %v", err)
	}

	// Test getting by source type
	downloader, err := registry.Get("test")
	if err != nil {
		t.Fatalf("Expected to get downloader, got error: %v", err)
	}
	if downloader.GetSourceType() != "test" {
		t.Fatalf("Expected source type 'test', got '%s'", downloader.GetSourceType())
	}

	// Test getting by alias
	downloader, err = registry.Get("alias")
	if err != nil {
		t.Fatalf("Expected to get downloader by alias, got error: %v", err)
	}
	if downloader.GetSourceType() != "test" {
		t.Fatalf("Expected source type 'test' via alias, got '%s'", downloader.GetSourceType())
	}

	// Test case insensitive lookup
	downloader, err = registry.Get("TEST")
	if err != nil {
		t.Fatalf("Expected case-insensitive lookup to work, got error: %v", err)
	}

	downloader, err = registry.Get("ALIAS")
	if err != nil {
		t.Fatalf("Expected case-insensitive alias lookup to work, got error: %v", err)
	}

	// Test non-existent source
	_, err = registry.Get("nonexistent")
	if err == nil {
		t.Fatal("Expected error for non-existent source, got nil")
	}
}

func TestRegistry_List(t *testing.T) {
	registry := NewRegistry()

	// Test empty registry
	types := registry.List()
	if len(types) != 0 {
		t.Fatalf("Expected empty list, got %d items", len(types))
	}

	// Add downloaders
	mockDownloader1 := NewMockDownloader("test1")
	mockDownloader2 := NewMockDownloader("test2")

	err := registry.Register(mockDownloader1)
	if err != nil {
		t.Fatalf("Failed to register downloader1: %v", err)
	}

	err = registry.Register(mockDownloader2)
	if err != nil {
		t.Fatalf("Failed to register downloader2: %v", err)
	}

	// Test list
	types = registry.List()
	if len(types) != 2 {
		t.Fatalf("Expected 2 types, got %d", len(types))
	}

	// Verify content (order might vary)
	typeSet := make(map[string]bool)
	for _, t := range types {
		typeSet[t] = true
	}

	if !typeSet["test1"] || !typeSet["test2"] {
		t.Fatalf("Expected types 'test1' and 'test2', got %v", types)
	}
}

func TestRegistry_ListWithAliases(t *testing.T) {
	registry := NewRegistry()
	mockDownloader := NewMockDownloader("test")

	err := registry.Register(mockDownloader)
	if err != nil {
		t.Fatalf("Failed to register downloader: %v", err)
	}

	err = registry.RegisterAlias("alias1", "test")
	if err != nil {
		t.Fatalf("Failed to register alias1: %v", err)
	}

	err = registry.RegisterAlias("alias2", "test")
	if err != nil {
		t.Fatalf("Failed to register alias2: %v", err)
	}

	result := registry.ListWithAliases()

	if len(result) != 1 {
		t.Fatalf("Expected 1 source type, got %d", len(result))
	}

	aliases, exists := result["test"]
	if !exists {
		t.Fatal("Expected 'test' source type to exist")
	}

	if len(aliases) != 2 {
		t.Fatalf("Expected 2 aliases, got %d", len(aliases))
	}

	aliasSet := make(map[string]bool)
	for _, alias := range aliases {
		aliasSet[alias] = true
	}

	if !aliasSet["alias1"] || !aliasSet["alias2"] {
		t.Fatalf("Expected aliases 'alias1' and 'alias2', got %v", aliases)
	}
}

func TestRegistry_Validate(t *testing.T) {
	registry := NewRegistry()
	mockDownloader := NewMockDownloader("test")
	mockDownloader.AddValidID("valid123")

	err := registry.Register(mockDownloader)
	if err != nil {
		t.Fatalf("Failed to register downloader: %v", err)
	}

	ctx := context.Background()

	// Test valid ID
	result, err := registry.Validate(ctx, "test", "valid123")
	if err != nil {
		t.Fatalf("Expected successful validation, got error: %v", err)
	}
	if !result.Valid {
		t.Fatal("Expected valid result, got invalid")
	}

	// Test invalid ID
	result, err = registry.Validate(ctx, "test", "invalid123")
	if err != nil {
		t.Fatalf("Expected validation to complete, got error: %v", err)
	}
	if result.Valid {
		t.Fatal("Expected invalid result, got valid")
	}

	// Test non-existent source
	_, err = registry.Validate(ctx, "nonexistent", "id123")
	if err == nil {
		t.Fatal("Expected error for non-existent source, got nil")
	}
}

func TestRegistry_AutoDetect(t *testing.T) {
	registry := NewRegistry()

	mockDownloader1 := NewMockDownloader("test1")
	mockDownloader1.AddValidID("id123")

	mockDownloader2 := NewMockDownloader("test2")
	mockDownloader2.AddValidID("id456")

	err := registry.Register(mockDownloader1)
	if err != nil {
		t.Fatalf("Failed to register downloader1: %v", err)
	}

	err = registry.Register(mockDownloader2)
	if err != nil {
		t.Fatalf("Failed to register downloader2: %v", err)
	}

	ctx := context.Background()

	// Test successful auto-detection
	sourceType, result, err := registry.AutoDetect(ctx, "id123")
	if err != nil {
		t.Fatalf("Expected successful auto-detection, got error: %v", err)
	}
	if sourceType != "test1" {
		t.Fatalf("Expected source type 'test1', got '%s'", sourceType)
	}
	if !result.Valid {
		t.Fatal("Expected valid result")
	}

	// Test auto-detection for second downloader
	sourceType, result, err = registry.AutoDetect(ctx, "id456")
	if err != nil {
		t.Fatalf("Expected successful auto-detection, got error: %v", err)
	}
	if sourceType != "test2" {
		t.Fatalf("Expected source type 'test2', got '%s'", sourceType)
	}

	// Test no match
	_, _, err = registry.AutoDetect(ctx, "unknown")
	if err == nil {
		t.Fatal("Expected error for unknown ID, got nil")
	}
}

func TestDefaultRegistry(t *testing.T) {
	// Test that default registry functions work
	mockDownloader := NewMockDownloader("default_test")

	err := Register(mockDownloader)
	if err != nil {
		t.Fatalf("Failed to register with default registry: %v", err)
	}

	downloader, err := Get("default_test")
	if err != nil {
		t.Fatalf("Failed to get from default registry: %v", err)
	}

	if downloader.GetSourceType() != "default_test" {
		t.Fatalf("Expected 'default_test', got '%s'", downloader.GetSourceType())
	}

	types := List()
	found := false
	for _, t := range types {
		if t == "default_test" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Expected to find 'default_test' in default registry list")
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	registry := NewRegistry()

	// Test concurrent registration and access
	done := make(chan bool, 10)

	// Concurrent registrations
	for i := 0; i < 5; i++ {
		go func(id int) {
			defer func() { done <- true }()
			mockDownloader := NewMockDownloader(fmt.Sprintf("concurrent%d", id))
			registry.Register(mockDownloader)
		}(i)
	}

	// Concurrent access
	for i := 0; i < 5; i++ {
		go func() {
			defer func() { done <- true }()
			registry.List()
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify final state
	types := registry.List()
	if len(types) != 5 {
		t.Fatalf("Expected 5 registered types after concurrent access, got %d", len(types))
	}
}

func TestValidationResult_String(t *testing.T) {
	result := &ValidationResult{
		Valid:      true,
		ID:         "test123",
		SourceType: "test",
		Errors:     []string{"error1", "error2"},
		Warnings:   []string{"warning1"},
	}

	// Test that the struct can be used (basic smoke test)
	if result.Valid != true {
		t.Fatal("Expected Valid to be true")
	}

	if result.ID != "test123" {
		t.Fatalf("Expected ID 'test123', got '%s'", result.ID)
	}

	if len(result.Errors) != 2 {
		t.Fatalf("Expected 2 errors, got %d", len(result.Errors))
	}

	if len(result.Warnings) != 1 {
		t.Fatalf("Expected 1 warning, got %d", len(result.Warnings))
	}
}
