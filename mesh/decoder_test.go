package mesh

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"testing"
)

func TestIsPNG(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected bool
	}{
		{
			name:     "valid PNG header",
			data:     []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'},
			expected: true,
		},
		{
			name:     "invalid header",
			data:     []byte{0x00, 0x00, 0x00, 0x00},
			expected: false,
		},
		{
			name:     "too short",
			data:     []byte{0x89, 'P', 'N'},
			expected: false,
		},
		{
			name:     "JSON data",
			data:     []byte(`{"__class":"ValetudoMap"}`),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPNG(tt.data)
			if result != tt.expected {
				t.Errorf("isPNG() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestInflateZlib(t *testing.T) {
	original := []byte(`{"__class":"ValetudoMap","metaData":{"version":1}}`)

	// Compress the data
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write(original); err != nil {
		t.Fatalf("Write error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error = %v", err)
	}

	// Decompress it
	decompressed, err := inflateZlib(compressed.Bytes())
	if err != nil {
		t.Fatalf("inflateZlib() error = %v", err)
	}

	if !bytes.Equal(decompressed, original) {
		t.Errorf("inflateZlib() = %s, want %s", decompressed, original)
	}
}

func TestDecodeMapData_RawJSON(t *testing.T) {
	jsonData := []byte(`{
		"__class": "ValetudoMap",
		"metaData": {
			"version": 1,
			"nonce": "test",
			"totalLayerArea": 1000
		},
		"size": {"x": 100, "y": 100},
		"pixelSize": 5,
		"layers": [],
		"entities": []
	}`)

	mapData, err := DecodeMapData(jsonData)
	if err != nil {
		t.Fatalf("DecodeMapData() error = %v", err)
	}

	if mapData.Class != "ValetudoMap" {
		t.Errorf("Class = %s, want ValetudoMap", mapData.Class)
	}
	if mapData.MetaData.Version != 1 {
		t.Errorf("Version = %d, want 1", mapData.MetaData.Version)
	}
	if mapData.Size.X != 100 || mapData.Size.Y != 100 {
		t.Errorf("Size = %+v, want {100 100}", mapData.Size)
	}
}

func TestDecodeMapData_ZlibCompressed(t *testing.T) {
	jsonData := []byte(`{
		"__class": "ValetudoMap",
		"metaData": {
			"version": 1,
			"nonce": "test",
			"totalLayerArea": 1000
		},
		"size": {"x": 100, "y": 100},
		"pixelSize": 5,
		"layers": [],
		"entities": []
	}`)

	// Compress the JSON
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write(jsonData); err != nil {
		t.Fatalf("Write error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error = %v", err)
	}

	mapData, err := DecodeMapData(compressed.Bytes())
	if err != nil {
		t.Fatalf("DecodeMapData() error = %v", err)
	}

	if mapData.Class != "ValetudoMap" {
		t.Errorf("Class = %s, want ValetudoMap", mapData.Class)
	}
}

func TestDecodeMapData_PNGWithZTXt(t *testing.T) {
	jsonData := []byte(`{
		"__class": "ValetudoMap",
		"metaData": {
			"version": 1,
			"nonce": "test",
			"totalLayerArea": 1000
		},
		"size": {"x": 100, "y": 100},
		"pixelSize": 5,
		"layers": [],
		"entities": []
	}`)

	// Create a minimal PNG with zTXt chunk
	png := createTestPNGWithZTXt(jsonData)

	mapData, err := DecodeMapData(png)
	if err != nil {
		t.Fatalf("DecodeMapData() error = %v", err)
	}

	if mapData.Class != "ValetudoMap" {
		t.Errorf("Class = %s, want ValetudoMap", mapData.Class)
	}
	if mapData.MetaData.Version != 1 {
		t.Errorf("Version = %d, want 1", mapData.MetaData.Version)
	}
}

func TestExtractZTXtData(t *testing.T) {
	jsonData := []byte(`{"test": "data"}`)

	// Compress the JSON
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write(jsonData); err != nil {
		t.Fatalf("Write error = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close error = %v", err)
	}

	// Create zTXt chunk data: keyword\0compression_method compressed_data
	keyword := "valetudo"
	ztxtData := append([]byte(keyword), 0)             // keyword + null terminator
	ztxtData = append(ztxtData, 0)                     // compression method (0 = zlib)
	ztxtData = append(ztxtData, compressed.Bytes()...) // compressed data

	extracted, err := extractZTXtData(ztxtData)
	if err != nil {
		t.Fatalf("extractZTXtData() error = %v", err)
	}

	if !bytes.Equal(extracted, jsonData) {
		t.Errorf("extractZTXtData() = %s, want %s", extracted, jsonData)
	}
}

// createTestPNGWithZTXt creates a minimal valid PNG with a zTXt chunk containing JSON
func createTestPNGWithZTXt(jsonData []byte) []byte {
	var buf bytes.Buffer

	// PNG signature
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})

	// IHDR chunk (minimal 1x1 image)
	ihdrData := []byte{
		0, 0, 0, 1, // width = 1
		0, 0, 0, 1, // height = 1
		8, // bit depth
		2, // color type (RGB)
		0, // compression
		0, // filter
		0, // interlace
	}
	writeChunk(&buf, "IHDR", ihdrData)

	// zTXt chunk with compressed JSON
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write(jsonData); err != nil {
		// In test helper, panic is acceptable
		panic(err)
	}
	if err := w.Close(); err != nil {
		panic(err)
	}

	keyword := "valetudo"
	ztxtData := append([]byte(keyword), 0)             // keyword + null
	ztxtData = append(ztxtData, 0)                     // compression method
	ztxtData = append(ztxtData, compressed.Bytes()...) // compressed data
	writeChunk(&buf, "zTXt", ztxtData)

	// IEND chunk
	writeChunk(&buf, "IEND", nil)

	return buf.Bytes()
}

// writeChunk writes a PNG chunk to the buffer
func writeChunk(buf *bytes.Buffer, chunkType string, data []byte) {
	// Length
	length := uint32(len(data))
	if err := binary.Write(buf, binary.BigEndian, length); err != nil {
		panic(err)
	}

	// Type
	buf.WriteString(chunkType)

	// Data
	if data != nil {
		buf.Write(data)
	}

	// CRC (simplified - use 0 for testing)
	if err := binary.Write(buf, binary.BigEndian, uint32(0)); err != nil {
		panic(err)
	}
}

func TestDecodeMapData_EmptyData(t *testing.T) {
	_, err := DecodeMapData([]byte{})
	if err == nil {
		t.Error("DecodeMapData() with empty data should return error")
	}
}

func TestDecodeMapData_InvalidData(t *testing.T) {
	invalidData := []byte{0xFF, 0xFE, 0xFD, 0xFC}
	_, err := DecodeMapData(invalidData)
	if err == nil {
		t.Error("DecodeMapData() with invalid data should return error")
	}
}

// Benchmark tests
func BenchmarkDecodeMapData_RawJSON(b *testing.B) {
	jsonData := []byte(`{
		"__class": "ValetudoMap",
		"metaData": {"version": 1, "nonce": "test", "totalLayerArea": 1000},
		"size": {"x": 100, "y": 100},
		"pixelSize": 5,
		"layers": [],
		"entities": []
	}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DecodeMapData(jsonData)
	}
}

func BenchmarkDecodeMapData_PNG(b *testing.B) {
	jsonData := []byte(`{
		"__class": "ValetudoMap",
		"metaData": {"version": 1, "nonce": "test", "totalLayerArea": 1000},
		"size": {"x": 100, "y": 100},
		"pixelSize": 5,
		"layers": [],
		"entities": []
	}`)
	pngData := createTestPNGWithZTXt(jsonData)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DecodeMapData(pngData)
	}
}

// Test integration with ParseMapJSON
func TestDecodeMapData_Integration(t *testing.T) {
	// Create a complete map structure
	mapData := &ValetudoMap{
		Class: "ValetudoMap",
		MetaData: MapMetaData{
			Version:        1,
			Nonce:          "test-nonce",
			TotalLayerArea: 5000,
		},
		Size:      Size{X: 512, Y: 512},
		PixelSize: 5,
		Layers: []MapLayer{
			{
				Class: "MapLayer",
				Type:  "floor",
				MetaData: LayerMetaData{
					Area:       1000,
					PixelCount: 200,
				},
				Pixels: []int{100, 100, 110, 110},
			},
		},
		Entities: []MapEntity{
			{
				Class:  "PointMapEntity",
				Type:   "robot_position",
				Points: []int{256, 256},
				MetaData: map[string]interface{}{
					"angle": 90.0,
				},
			},
		},
	}

	// Marshal to JSON
	jsonBytes, err := json.Marshal(mapData)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Test raw JSON decoding
	decoded, err := DecodeMapData(jsonBytes)
	if err != nil {
		t.Fatalf("DecodeMapData() error = %v", err)
	}

	if decoded.Class != mapData.Class {
		t.Errorf("Class = %s, want %s", decoded.Class, mapData.Class)
	}
	if decoded.MetaData.TotalLayerArea != mapData.MetaData.TotalLayerArea {
		t.Errorf("TotalLayerArea = %d, want %d", decoded.MetaData.TotalLayerArea, mapData.MetaData.TotalLayerArea)
	}
	if len(decoded.Layers) != len(mapData.Layers) {
		t.Errorf("Layers count = %d, want %d", len(decoded.Layers), len(mapData.Layers))
	}
	if len(decoded.Entities) != len(mapData.Entities) {
		t.Errorf("Entities count = %d, want %d", len(decoded.Entities), len(mapData.Entities))
	}

	// Test PNG format
	pngData := createTestPNGWithZTXt(jsonBytes)
	decodedPNG, err := DecodeMapData(pngData)
	if err != nil {
		t.Fatalf("DecodeMapData(PNG) error = %v", err)
	}

	if decodedPNG.Class != mapData.Class {
		t.Errorf("PNG: Class = %s, want %s", decodedPNG.Class, mapData.Class)
	}
}
