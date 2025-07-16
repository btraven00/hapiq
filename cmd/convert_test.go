package cmd

import (
	"strings"
	"testing"
)

func TestPDFConverter_cleanText(t *testing.T) {
	converter := NewPDFConverter(false, false, true)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normalize whitespace",
			input:    "This  has   multiple    spaces",
			expected: "This has multiple spaces",
		},
		{
			name:     "remove non-breaking spaces",
			input:    "Text\u00a0with\u00a0non-breaking\u00a0spaces",
			expected: "Text with non-breaking spaces",
		},
		{
			name:     "normalize unicode quotes",
			input:    "\u201cQuoted text\u201d and \u2018single quotes\u2019",
			expected: "\"Quoted text\" and 'single quotes'",
		},
		{
			name:     "normalize hyphens",
			input:    "Dash\u2013example\u2014test",
			expected: "Dash-example--test",
		},
		{
			name:     "preserve line breaks",
			input:    "Line one\nLine two\n\nNew paragraph",
			expected: "Line one\nLine two\n\nNew paragraph",
		},
		{
			name:     "reduce excessive line breaks",
			input:    "Text\n\n\n\n\nMore text",
			expected: "Text\n\nMore text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.cleanText(tt.input)
			if result != tt.expected {
				t.Errorf("cleanText() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestPDFConverter_isHeader(t *testing.T) {
	converter := NewPDFConverter(false, false, true)

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "numbered section",
			input:    "1. Introduction",
			expected: true,
		},
		{
			name:     "subsection",
			input:    "2.1. Methods",
			expected: true,
		},
		{
			name:     "all caps header",
			input:    "METHODS AND RESULTS",
			expected: true,
		},
		{
			name:     "title case header",
			input:    "Data Analysis Workflow",
			expected: true,
		},
		{
			name:     "not header - sentence",
			input:    "This is a regular sentence with punctuation.",
			expected: false,
		},
		{
			name:     "not header - too long",
			input:    "This is way too long to be a header and contains many words that would not typically appear in a section heading or title",
			expected: false,
		},
		{
			name:     "not header - too short",
			input:    "No",
			expected: false,
		},
		{
			name:     "not header - body text with commas",
			input:    "The results show significant differences, with p < 0.05",
			expected: false,
		},
		{
			name:     "header with mixed case",
			input:    "Results and Discussion",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.isHeader(tt.input)
			if result != tt.expected {
				t.Errorf("isHeader(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestPDFConverter_determineHeaderLevel(t *testing.T) {
	converter := NewPDFConverter(false, false, true)

	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "main section",
			input:    "1. Introduction",
			expected: 2,
		},
		{
			name:     "subsection",
			input:    "2.1. Methods",
			expected: 3,
		},
		{
			name:     "sub-subsection",
			input:    "2.1.1. Data Collection",
			expected: 4,
		},
		{
			name:     "all caps major section",
			input:    "RESULTS",
			expected: 2,
		},
		{
			name:     "regular title case",
			input:    "Statistical Analysis",
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.determineHeaderLevel(tt.input)
			if result != tt.expected {
				t.Errorf("determineHeaderLevel(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestPDFConverter_cleanHeaderText(t *testing.T) {
	converter := NewPDFConverter(false, false, true)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "remove section number",
			input:    "1. Introduction",
			expected: "Introduction",
		},
		{
			name:     "remove subsection number",
			input:    "2.1. Methods",
			expected: "Methods",
		},
		{
			name:     "normalize all caps",
			input:    "RESULTS AND DISCUSSION",
			expected: "Results And Discussion",
		},
		{
			name:     "preserve mixed case",
			input:    "Statistical Analysis",
			expected: "Statistical Analysis",
		},
		{
			name:     "remove complex numbering",
			input:    "3.2.1. Data Processing Pipeline",
			expected: "Data Processing Pipeline",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.cleanHeaderText(tt.input)
			if result != tt.expected {
				t.Errorf("cleanHeaderText(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestPDFConverter_isListItem(t *testing.T) {
	converter := NewPDFConverter(false, false, true)

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "bullet point",
			input:    "• First item",
			expected: true,
		},
		{
			name:     "dash bullet",
			input:    "- Second item",
			expected: true,
		},
		{
			name:     "asterisk bullet",
			input:    "* Third item",
			expected: true,
		},
		{
			name:     "numbered list",
			input:    "1. First numbered item",
			expected: true,
		},
		{
			name:     "parenthetical number",
			input:    "(1) Another numbered item",
			expected: true,
		},
		{
			name:     "lettered list",
			input:    "a) Lettered item",
			expected: true,
		},
		{
			name:     "not a list item",
			input:    "This is regular text",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.isListItem(tt.input)
			if result != tt.expected {
				t.Errorf("isListItem(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestPDFConverter_formatListItem(t *testing.T) {
	converter := NewPDFConverter(false, false, true)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "unicode bullet",
			input:    "• First item",
			expected: "- First item",
		},
		{
			name:     "dash bullet",
			input:    "- Second item",
			expected: "- Second item",
		},
		{
			name:     "numbered list preserved",
			input:    "1. First numbered item",
			expected: "1. First numbered item",
		},
		{
			name:     "parenthetical to bullet",
			input:    "(1) Another numbered item",
			expected: "- Another numbered item",
		},
		{
			name:     "lettered to bullet",
			input:    "a) Lettered item",
			expected: "- Lettered item",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.formatListItem(tt.input)
			if result != tt.expected {
				t.Errorf("formatListItem(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestPDFConverter_processPageText(t *testing.T) {
	converter := NewPDFConverter(false, false, true)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "simple text with header",
			input: `1. Introduction
This is a paragraph of text that should be formatted properly.

2. Methods
Another paragraph here.`,
			expected: `## Introduction

This is a paragraph of text that should be formatted properly.

## Methods

Another paragraph here.

`,
		},
		{
			name: "text with list items",
			input: `The following items are important:
• First item
• Second item
• Third item

This is the conclusion.`,
			expected: `The following items are important:

- First item
- Second item
- Third item

This is the conclusion.

`,
		},
		{
			name: "preserve layout mode",
			input: `Line one
Line two

Line four`,
			expected: `Line one

Line two


Line four

`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "preserve layout mode" {
				converter = NewPDFConverter(true, false, true)
			} else {
				converter = NewPDFConverter(false, false, true)
			}

			result := converter.processPageText(tt.input)
			if strings.TrimSpace(result) != strings.TrimSpace(tt.expected) {
				t.Errorf("processPageText() result doesn't match expected.\nGot:\n%q\nWant:\n%q", result, tt.expected)
			}
		})
	}
}

func TestNewPDFConverter(t *testing.T) {
	tests := []struct {
		name           string
		preserveLayout bool
		includePages   bool
		extractHeaders bool
	}{
		{
			name:           "default options",
			preserveLayout: false,
			includePages:   false,
			extractHeaders: true,
		},
		{
			name:           "preserve layout",
			preserveLayout: true,
			includePages:   false,
			extractHeaders: true,
		},
		{
			name:           "include pages",
			preserveLayout: false,
			includePages:   true,
			extractHeaders: true,
		},
		{
			name:           "all options enabled",
			preserveLayout: true,
			includePages:   true,
			extractHeaders: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			converter := NewPDFConverter(tt.preserveLayout, tt.includePages, tt.extractHeaders)

			if converter.preserveLayout != tt.preserveLayout {
				t.Errorf("preserveLayout = %v, want %v", converter.preserveLayout, tt.preserveLayout)
			}
			if converter.includePages != tt.includePages {
				t.Errorf("includePages = %v, want %v", converter.includePages, tt.includePages)
			}
			if converter.extractHeaders != tt.extractHeaders {
				t.Errorf("extractHeaders = %v, want %v", converter.extractHeaders, tt.extractHeaders)
			}

			// Check that regex patterns are initialized
			if len(converter.headerPatterns) == 0 {
				t.Error("headerPatterns should be initialized")
			}
			if len(converter.bulletPatterns) == 0 {
				t.Error("bulletPatterns should be initialized")
			}
			if len(converter.numberPatterns) == 0 {
				t.Error("numberPatterns should be initialized")
			}
			if converter.whitespaceRegex == nil {
				t.Error("whitespaceRegex should be initialized")
			}
		})
	}
}

func TestPDFConverter_addBasicSpacing(t *testing.T) {
	converter := NewPDFConverter(false, false, true)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "concatenated academic text",
			input:    "SupplementaryinformationTheonlineversioncontainssupplementarymaterial",
			expected: "Supplementary information The online version contains supplementary material",
		},
		{
			name:     "text with URLs",
			input:    "availableathttps://doi.org/10.1038/s41467-021-23778-6Correspondence",
			expected: "available at https://doi.org/10.1038/s 41467-021-23778-6 Correspondence",
		},
		{
			name:     "mixed concatenated text",
			input:    "PeerreviewinformationNatureCommunicationsthankstheanonymousreviewers",
			expected: "Peer review information Nature Communications thanks the anonymous reviewers",
		},
		{
			name:     "text with punctuation boundaries",
			input:    "workPeerreviewerreportsareavailableReprintsandpermission",
			expected: "work Peer reviewer reports are available Reprints and permission",
		},
		{
			name:     "already properly spaced text",
			input:    "This text is already properly spaced.",
			expected: "This text is already properly spaced.",
		},
		{
			name:     "camelCase and number boundaries",
			input:    "TestCase1andTestCase2forExperiment3analysis",
			expected: "Test Case 1 and Test Case 2 for Experiment 3 analysis",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.addBasicSpacing(tt.input)
			if result != tt.expected {
				t.Errorf("addBasicSpacing() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestPDFConverter_dynamicWordSegmentation(t *testing.T) {
	converter := NewPDFConverter(false, false, true)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple concatenated words",
			input:    "theonlineversion",
			expected: "the online version",
		},
		{
			name:     "scientific terms",
			input:    "supplementaryinformation",
			expected: "supplementary information",
		},
		{
			name:     "already segmented",
			input:    "already good",
			expected: "already good",
		},
		{
			name:     "single word",
			input:    "test",
			expected: "test",
		},
		{
			name:     "unsegmentable gibberish",
			input:    "xyzqwerty",
			expected: "xyzqwerty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.dynamicWordSegmentation(tt.input)
			if result != tt.expected {
				t.Errorf("dynamicWordSegmentation() = %q, want %q", result, tt.expected)
			}
		})
	}
}
