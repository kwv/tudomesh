package mesh

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// DecodeMapData decodes Valetudo map data from various formats:
// - PNG with zTXt chunk (primary format from MQTT)
// - Raw JSON (fallback for testing)
// - Zlib-compressed JSON without PNG wrapper
func DecodeMapData(data []byte) (*ValetudoMap, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data")
	}

	var jsonBytes []byte
	var err error

	// Try PNG format first (most common from MQTT)
	if IsPNG(data) {
		jsonBytes, err = extractPNGzTXt(data)
		if err != nil {
			return nil, fmt.Errorf("extracting PNG zTXt: %w", err)
		}
	} else if data[0] == '{' {
		// Try raw JSON (starts with '{')
		jsonBytes = data
	} else {
		// Try zlib-compressed JSON
		jsonBytes, err = inflateZlib(data)
		if err != nil {
			return nil, fmt.Errorf("unknown format: not PNG, JSON, or zlib-compressed")
		}
	}

	if len(jsonBytes) == 0 {
		return nil, fmt.Errorf("decoded JSON payload is empty")
	}

	m, err := ParseMapJSON(jsonBytes)
	if err != nil {
		return nil, err
	}
	NormalizeToMM(m)
	return m, nil
}

// IsPNG checks if data starts with PNG magic bytes
func IsPNG(data []byte) bool {
	if len(data) < 8 {
		return false
	}
	// PNG magic bytes: 0x89 'P' 'N' 'G' '\r' '\n' 0x1a '\n'
	return data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G'
}

// extractPNGzTXt extracts and decompresses JSON from PNG zTXt chunk
// PNG structure: 8-byte header, then chunks (length, type, data, CRC)
// zTXt chunk format: keyword\0compression_method compressed_text
func extractPNGzTXt(data []byte) ([]byte, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("data too short for PNG")
	}

	// Skip PNG header (8 bytes)
	pos := 8

	// Parse PNG chunks
	for pos+12 <= len(data) {
		// Read chunk length (4 bytes, big-endian)
		chunkLen := binary.BigEndian.Uint32(data[pos : pos+4])
		pos += 4

		// Read chunk type (4 bytes)
		chunkType := string(data[pos : pos+4])
		pos += 4

		// Check if we have enough data for chunk data + CRC
		if pos+int(chunkLen)+4 > len(data) {
			return nil, fmt.Errorf("truncated PNG chunk")
		}

		// If this is a zTXt chunk, extract and decompress
		if chunkType == "zTXt" {
			chunkData := data[pos : pos+int(chunkLen)]
			jsonBytes, err := extractZTXtData(chunkData)
			if err != nil {
				return nil, fmt.Errorf("extracting zTXt data: %w", err)
			}
			return jsonBytes, nil
		}

		// Skip chunk data and CRC (4 bytes)
		pos += int(chunkLen) + 4

		// Stop at IEND chunk
		if chunkType == "IEND" {
			break
		}
	}

	return nil, fmt.Errorf("no zTXt chunk found in PNG")
}

// extractZTXtData parses and decompresses zTXt chunk data
// Format: keyword\0compression_method compressed_text
func extractZTXtData(data []byte) ([]byte, error) {
	// Find null terminator after keyword
	nullIdx := bytes.IndexByte(data, 0)
	if nullIdx == -1 {
		return nil, fmt.Errorf("no null terminator in zTXt chunk")
	}

	// Check compression method (should be 0 for zlib)
	if nullIdx+1 >= len(data) {
		return nil, fmt.Errorf("truncated zTXt chunk")
	}

	compressionMethod := data[nullIdx+1]
	if compressionMethod != 0 {
		return nil, fmt.Errorf("unsupported compression method: %d", compressionMethod)
	}

	// Decompress the rest of the data
	compressedData := data[nullIdx+2:]
	return inflateZlib(compressedData)
}

// inflateZlib decompresses zlib-compressed data
func inflateZlib(data []byte) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("creating zlib reader: %w", err)
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			// Log error but don't fail since we already have the data
			_ = closeErr
		}
	}()

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("decompressing zlib data: %w", err)
	}

	return decompressed, nil
}

// DecodePNGMapFile reads and decodes a Valetudo PNG map file
// This is a convenience function for testing with PNG files
func DecodePNGMapFile(path string) (*ValetudoMap, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}
	return DecodeMapData(data)
}

// readFile is a helper to read file contents
func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
