// Package common provides JSON utilities for encoding and decoding
// downloader-related data structures with proper formatting.
package common

import (
	"encoding/json"
	"io"
)

// JSONEncoder wraps json.Encoder with additional functionality.
type JSONEncoder struct {
	encoder *json.Encoder
}

// NewJSONEncoder creates a new JSON encoder.
func NewJSONEncoder(w io.Writer) *JSONEncoder {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false) // Don't escape HTML characters

	return &JSONEncoder{encoder: encoder}
}

// SetIndent sets the indentation for pretty-printing.
func (e *JSONEncoder) SetIndent(prefix, indent string) {
	e.encoder.SetIndent(prefix, indent)
}

// Encode encodes the value to JSON.
func (e *JSONEncoder) Encode(v interface{}) error {
	return e.encoder.Encode(v)
}

// JSONDecoder wraps json.Decoder with additional functionality.
type JSONDecoder struct {
	decoder *json.Decoder
}

// NewJSONDecoder creates a new JSON decoder.
func NewJSONDecoder(r io.Reader) *JSONDecoder {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields() // Strict parsing

	return &JSONDecoder{decoder: decoder}
}

// Decode decodes JSON into the value.
func (d *JSONDecoder) Decode(v interface{}) error {
	return d.decoder.Decode(v)
}

// MarshalIndent returns indented JSON encoding of v.
func MarshalIndent(v interface{}) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// Unmarshal parses JSON data and stores the result in v.
func Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
