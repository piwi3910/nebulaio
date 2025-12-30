// Package selectapi provides S3 Select functionality for querying object data.
package selectapi

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
)

// ParquetReader provides functionality to read and query Parquet files.
type ParquetReader struct {
	reader    io.ReadSeeker
	metadata  *ParquetMetadata
	schema    *ParquetSchema
	rowGroups []RowGroup
}

// ParquetMetadata contains file-level metadata.
type ParquetMetadata struct {
	KeyValueMetadata map[string]string
	CreatedBy        string
	RowGroups        []RowGroupMetadata
	Schema           []SchemaElement
	NumRows          int64
	Version          int32
}

// RowGroupMetadata contains metadata for a row group.
type RowGroupMetadata struct {
	Columns       []ColumnChunkMetadata
	NumRows       int64
	TotalByteSize int64
	FileOffset    int64
}

// ColumnChunkMetadata contains metadata for a column chunk.
type ColumnChunkMetadata struct {
	Statistics            *ColumnStatistics
	ColumnPath            []string
	NumValues             int64
	TotalUncompressedSize int64
	TotalCompressedSize   int64
	DataPageOffset        int64
	DictionaryPageOffset  int64
	Type                  ParquetType
	Codec                 CompressionCodec
}

// ColumnStatistics contains min/max statistics for a column.
type ColumnStatistics struct {
	MinValue      []byte
	MaxValue      []byte
	NullCount     int64
	DistinctCount int64
	HasMinMax     bool
}

// SchemaElement represents an element in the Parquet schema.
type SchemaElement struct {
	LogicalType    *LogicalType
	Name           string
	Type           ParquetType
	TypeLength     int32
	RepetitionType RepetitionType
	NumChildren    int32
	ConvertedType  ConvertedType
	Scale          int32
	Precision      int32
}

// ParquetType represents Parquet physical types.
type ParquetType int32

const (
	ParquetTypeBoolean           ParquetType = 0
	ParquetTypeInt32             ParquetType = 1
	ParquetTypeInt64             ParquetType = 2
	ParquetTypeInt96             ParquetType = 3
	ParquetTypeFloat             ParquetType = 4
	ParquetTypeDouble            ParquetType = 5
	ParquetTypeByteArray         ParquetType = 6
	ParquetTypeFixedLenByteArray ParquetType = 7
)

// RepetitionType represents field repetition.
type RepetitionType int32

const (
	RepetitionRequired RepetitionType = 0
	RepetitionOptional RepetitionType = 1
	RepetitionRepeated RepetitionType = 2
)

// ConvertedType represents logical types (deprecated in favor of LogicalType).
type ConvertedType int32

const (
	ConvertedTypeNone            ConvertedType = -1
	ConvertedTypeUTF8            ConvertedType = 0
	ConvertedTypeMap             ConvertedType = 1
	ConvertedTypeMapKeyValue     ConvertedType = 2
	ConvertedTypeList            ConvertedType = 3
	ConvertedTypeEnum            ConvertedType = 4
	ConvertedTypeDecimal         ConvertedType = 5
	ConvertedTypeDate            ConvertedType = 6
	ConvertedTypeTimeMillis      ConvertedType = 7
	ConvertedTypeTimeMicros      ConvertedType = 8
	ConvertedTypeTimestampMillis ConvertedType = 9
	ConvertedTypeTimestampMicros ConvertedType = 10
	ConvertedTypeUint8           ConvertedType = 11
	ConvertedTypeUint16          ConvertedType = 12
	ConvertedTypeUint32          ConvertedType = 13
	ConvertedTypeUint64          ConvertedType = 14
	ConvertedTypeInt8            ConvertedType = 15
	ConvertedTypeInt16           ConvertedType = 16
	ConvertedTypeInt32           ConvertedType = 17
	ConvertedTypeInt64           ConvertedType = 18
	ConvertedTypeJSON            ConvertedType = 19
	ConvertedTypeBSON            ConvertedType = 20
	ConvertedTypeInterval        ConvertedType = 21
)

// LogicalType represents the new logical type system.
type LogicalType struct {
	Type            string
	Unit            string
	Precision       int32
	Scale           int32
	IsAdjustedToUTC bool
}

// CompressionCodec represents compression algorithms.
type CompressionCodec int32

const (
	CodecUncompressed CompressionCodec = 0
	CodecSnappy       CompressionCodec = 1
	CodecGzip         CompressionCodec = 2
	CodecLZO          CompressionCodec = 3
	CodecBrotli       CompressionCodec = 4
	CodecLZ4          CompressionCodec = 5
	CodecZstd         CompressionCodec = 6
)

// ParquetSchema represents the schema of a Parquet file.
type ParquetSchema struct {
	Root    *SchemaNode
	Columns []*ColumnDescriptor
}

// SchemaNode represents a node in the schema tree.
type SchemaNode struct {
	Logical    *LogicalType
	Name       string
	Children   []*SchemaNode
	Type       ParquetType
	Repetition RepetitionType
	Converted  ConvertedType
	Precision  int32
	Scale      int32
	TypeLength int32
}

// ColumnDescriptor describes a leaf column.
type ColumnDescriptor struct {
	Logical     *LogicalType
	Path        []string
	MaxDefLevel int
	MaxRepLevel int
	Type        ParquetType
	Converted   ConvertedType
	Precision   int32
	Scale       int32
}

// RowGroup represents a row group in the file.
type RowGroup struct {
	ColumnChunks  []*ColumnChunk
	Index         int
	NumRows       int64
	TotalByteSize int64
}

// ColumnChunk represents a column chunk in a row group.
type ColumnChunk struct {
	Descriptor *ColumnDescriptor
	Metadata   *ColumnChunkMetadata
	Pages      []*Page
}

// Page represents a data page.
type Page struct {
	Data             []byte
	Type             PageType
	NumValues        int32
	UncompressedSize int32
	CompressedSize   int32
}

// PageType represents the type of page.
type PageType int32

const (
	PageTypeDataPage       PageType = 0
	PageTypeIndexPage      PageType = 1
	PageTypeDictionaryPage PageType = 2
	PageTypeDataPageV2     PageType = 3
)

// Parquet magic number.
var parquetMagic = []byte("PAR1")

// NewParquetReader creates a new Parquet reader.
func NewParquetReader(reader io.ReadSeeker) (*ParquetReader, error) {
	pr := &ParquetReader{
		reader: reader,
	}

	err := pr.readMetadata()
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	return pr, nil
}

// readMetadata reads the Parquet file metadata.
func (pr *ParquetReader) readMetadata() error {
	// Seek to end to read footer
	size, err := pr.reader.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("failed to seek to end: %w", err)
	}

	if size < 8 {
		return errors.New("file too small to be valid Parquet")
	}

	// Read footer magic and metadata length
	if _, err := pr.reader.Seek(size-8, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek to footer: %w", err)
	}

	footer := make([]byte, 8)
	if _, err := io.ReadFull(pr.reader, footer); err != nil {
		return fmt.Errorf("failed to read footer: %w", err)
	}

	// Verify magic
	if !bytes.Equal(footer[4:8], parquetMagic) {
		return errors.New("invalid Parquet magic number")
	}

	metadataLen := binary.LittleEndian.Uint32(footer[0:4])

	// Read metadata
	metadataOffset := size - 8 - int64(metadataLen)
	if _, err := pr.reader.Seek(metadataOffset, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek to metadata: %w", err)
	}

	metadataBytes := make([]byte, metadataLen)
	if _, err := io.ReadFull(pr.reader, metadataBytes); err != nil {
		return fmt.Errorf("failed to read metadata: %w", err)
	}

	// Parse metadata (simplified - real implementation would use Thrift)
	if err := pr.parseMetadata(metadataBytes); err != nil {
		return fmt.Errorf("failed to parse metadata: %w", err)
	}

	return nil
}

// parseMetadata parses the file metadata (simplified implementation).
func (pr *ParquetReader) parseMetadata(data []byte) error {
	// This is a simplified implementation
	// Real implementation would parse Thrift-encoded metadata
	pr.metadata = &ParquetMetadata{
		Version:          1,
		KeyValueMetadata: make(map[string]string),
	}

	// Build schema from metadata
	pr.schema = &ParquetSchema{
		Root: &SchemaNode{
			Name: "root",
		},
	}

	return nil
}

// GetSchema returns the file schema.
func (pr *ParquetReader) GetSchema() *ParquetSchema {
	return pr.schema
}

// GetMetadata returns the file metadata.
func (pr *ParquetReader) GetMetadata() *ParquetMetadata {
	return pr.metadata
}

// GetNumRows returns the total number of rows.
func (pr *ParquetReader) GetNumRows() int64 {
	return pr.metadata.NumRows
}

// ReadRows reads rows starting at the given offset.
func (pr *ParquetReader) ReadRows(offset, limit int64) ([]map[string]interface{}, error) {
	rows := make([]map[string]interface{}, 0, limit)

	currentRow := int64(0)
	for _, rg := range pr.rowGroups {
		if currentRow+rg.NumRows <= offset {
			currentRow += rg.NumRows
			continue
		}

		// Read row group
		rgRows, err := pr.readRowGroup(&rg, offset-currentRow, limit-int64(len(rows)))
		if err != nil {
			return nil, fmt.Errorf("failed to read row group: %w", err)
		}

		rows = append(rows, rgRows...)
		currentRow += rg.NumRows

		if int64(len(rows)) >= limit {
			break
		}
	}

	return rows, nil
}

// readRowGroup reads rows from a row group.
func (pr *ParquetReader) readRowGroup(rg *RowGroup, offset, limit int64) ([]map[string]interface{}, error) {
	if offset < 0 {
		offset = 0
	}

	if limit <= 0 || limit > rg.NumRows-offset {
		limit = rg.NumRows - offset
	}

	rows := make([]map[string]interface{}, 0, limit)

	// Read column data
	columnData := make(map[string][]interface{})

	for _, chunk := range rg.ColumnChunks {
		colName := strings.Join(chunk.Descriptor.Path, ".")

		values, err := pr.readColumnChunk(chunk)
		if err != nil {
			return nil, fmt.Errorf("failed to read column %s: %w", colName, err)
		}

		columnData[colName] = values
	}

	// Build rows
	for i := range limit {
		row := make(map[string]interface{})

		for colName, values := range columnData {
			idx := offset + i
			if idx < int64(len(values)) {
				row[colName] = values[idx]
			}
		}

		rows = append(rows, row)
	}

	return rows, nil
}

// readColumnChunk reads values from a column chunk.
func (pr *ParquetReader) readColumnChunk(chunk *ColumnChunk) ([]interface{}, error) {
	values := make([]interface{}, 0)

	for _, page := range chunk.Pages {
		pageValues, err := pr.readPage(page, chunk.Descriptor)
		if err != nil {
			return nil, fmt.Errorf("failed to read page: %w", err)
		}

		values = append(values, pageValues...)
	}

	return values, nil
}

// readPage reads values from a data page.
func (pr *ParquetReader) readPage(page *Page, desc *ColumnDescriptor) ([]interface{}, error) {
	data := page.Data

	// Decompress if needed
	// (Simplified - real implementation would check metadata)

	values := make([]interface{}, 0, page.NumValues)

	reader := bytes.NewReader(data)
	for range page.NumValues {
		val, err := pr.readValue(reader, desc)
		if err != nil {
			if err == io.EOF {
				break
			}

			return nil, err
		}

		values = append(values, val)
	}

	return values, nil
}

// readValue reads a single value based on the column type.
func (pr *ParquetReader) readValue(reader io.Reader, desc *ColumnDescriptor) (interface{}, error) {
	switch desc.Type {
	case ParquetTypeBoolean:
		var b byte

		err := binary.Read(reader, binary.LittleEndian, &b)
		if err != nil {
			return nil, err
		}

		return b != 0, nil

	case ParquetTypeInt32:
		var val int32

		err := binary.Read(reader, binary.LittleEndian, &val)
		if err != nil {
			return nil, err
		}

		return pr.convertInt32(val, desc), nil

	case ParquetTypeInt64:
		var val int64

		err := binary.Read(reader, binary.LittleEndian, &val)
		if err != nil {
			return nil, err
		}

		return pr.convertInt64(val, desc), nil

	case ParquetTypeFloat:
		var val float32

		err := binary.Read(reader, binary.LittleEndian, &val)
		if err != nil {
			return nil, err
		}

		return float64(val), nil

	case ParquetTypeDouble:
		var val float64

		err := binary.Read(reader, binary.LittleEndian, &val)
		if err != nil {
			return nil, err
		}

		return val, nil

	case ParquetTypeByteArray:
		var length int32

		err := binary.Read(reader, binary.LittleEndian, &length)
		if err != nil {
			return nil, err
		}

		data := make([]byte, length)
		if _, err := io.ReadFull(reader, data); err != nil {
			return nil, err
		}

		return pr.convertByteArray(data, desc), nil

	case ParquetTypeFixedLenByteArray:
		data := make([]byte, desc.Precision) // Use precision as length
		if _, err := io.ReadFull(reader, data); err != nil {
			return nil, err
		}

		return pr.convertByteArray(data, desc), nil

	case ParquetTypeInt96:
		data := make([]byte, 12)
		if _, err := io.ReadFull(reader, data); err != nil {
			return nil, err
		}

		return pr.convertInt96(data), nil

	default:
		return nil, fmt.Errorf("unsupported type: %d", desc.Type)
	}
}

// convertInt32 converts an int32 based on logical type.
func (pr *ParquetReader) convertInt32(val int32, desc *ColumnDescriptor) interface{} {
	switch desc.Converted {
	case ConvertedTypeDate:
		// Days since Unix epoch
		return time.Unix(int64(val)*86400, 0).UTC()
	case ConvertedTypeTimeMillis:
		// Milliseconds since midnight
		return time.Duration(val) * time.Millisecond
	case ConvertedTypeDecimal:
		// Decimal with precision and scale
		return float64(val) / math.Pow10(int(desc.Scale))
	case ConvertedTypeInt8:
		return int8(val) //nolint:gosec // G115: intentional type conversion per Parquet spec
	case ConvertedTypeInt16:
		return int16(val) //nolint:gosec // G115: intentional type conversion per Parquet spec
	case ConvertedTypeUint8:
		return uint8(val) //nolint:gosec // G115: intentional type conversion per Parquet spec
	case ConvertedTypeUint16:
		return uint16(val) //nolint:gosec // G115: intentional type conversion per Parquet spec
	case ConvertedTypeUint32:
		return uint32(val) //nolint:gosec // G115: intentional type conversion per Parquet spec
	default:
		return val
	}
}

// convertInt64 converts an int64 based on logical type.
func (pr *ParquetReader) convertInt64(val int64, desc *ColumnDescriptor) interface{} {
	switch desc.Converted {
	case ConvertedTypeTimestampMillis:
		return time.UnixMilli(val).UTC()
	case ConvertedTypeTimestampMicros:
		return time.UnixMicro(val).UTC()
	case ConvertedTypeTimeMicros:
		return time.Duration(val) * time.Microsecond
	case ConvertedTypeDecimal:
		return float64(val) / math.Pow10(int(desc.Scale))
	case ConvertedTypeUint64:
		return uint64(val) //nolint:gosec // G115: intentional type conversion per Parquet spec
	default:
		return val
	}
}

// convertByteArray converts a byte array based on logical type.
func (pr *ParquetReader) convertByteArray(data []byte, desc *ColumnDescriptor) interface{} {
	switch desc.Converted {
	case ConvertedTypeUTF8:
		return string(data)
	case ConvertedTypeJSON:
		var result interface{}

		err := json.Unmarshal(data, &result)
		if err != nil {
			return string(data)
		}

		return result
	case ConvertedTypeDecimal:
		// Big-endian encoded decimal
		return pr.decodeDecimal(data, desc.Precision, desc.Scale)
	default:
		return data
	}
}

// convertInt96 converts INT96 to timestamp (Spark/Hive format).
func (pr *ParquetReader) convertInt96(data []byte) interface{} {
	// INT96 is nanoseconds since midnight + Julian day
	if len(data) != 12 {
		return nil
	}

	nanos := binary.LittleEndian.Uint64(data[0:8])
	julianDay := binary.LittleEndian.Uint32(data[8:12])

	// Julian day 2440588 is Unix epoch (1970-01-01)
	unixDay := int64(julianDay) - 2440588
	//nolint:gosec // G115: nanos is nanoseconds since midnight, fits in int64
	unixNanos := unixDay*86400*1e9 + int64(nanos)

	return time.Unix(0, unixNanos).UTC()
}

// decodeDecimal decodes a big-endian decimal.
func (pr *ParquetReader) decodeDecimal(data []byte, precision, scale int32) float64 {
	// Simple implementation for small decimals
	var val int64
	for _, b := range data {
		val = (val << 8) | int64(b)
	}

	// Handle negative numbers (two's complement)
	if len(data) > 0 && data[0]&0x80 != 0 {
		// Sign extend
		for i := len(data); i < 8; i++ {
			val |= int64(0xFF) << (i * 8)
		}
	}

	return float64(val) / math.Pow10(int(scale))
}

// ParquetSelectExecutor executes S3 Select queries on Parquet files.
type ParquetSelectExecutor struct {
	reader *ParquetReader
}

// NewParquetSelectExecutor creates a new Parquet select executor.
func NewParquetSelectExecutor(reader io.ReadSeeker) (*ParquetSelectExecutor, error) {
	pr, err := NewParquetReader(reader)
	if err != nil {
		return nil, err
	}

	return &ParquetSelectExecutor{reader: pr}, nil
}

// Execute executes a select query on the Parquet file.
func (e *ParquetSelectExecutor) Execute(query *SelectQuery) (*SelectResult, error) {
	result := &SelectResult{
		Columns: query.Columns,
		Rows:    make([][]interface{}, 0),
	}

	// Read all rows (with pagination for large files)
	offset := int64(0)
	batchSize := int64(10000)

	for {
		rows, err := e.reader.ReadRows(offset, batchSize)
		if err != nil {
			return nil, fmt.Errorf("failed to read rows: %w", err)
		}

		if len(rows) == 0 {
			break
		}

		// Apply query to each row
		for _, row := range rows {
			// Apply WHERE clause
			if query.Where != nil {
				match, err := evaluateCondition(query.Where, row)
				if err != nil {
					return nil, fmt.Errorf("failed to evaluate condition: %w", err)
				}

				if !match {
					continue
				}
			}

			// Project columns
			var resultRow []interface{}

			if len(query.Columns) == 1 && query.Columns[0] == "*" {
				// Select all columns
				for _, col := range e.reader.schema.Columns {
					colName := strings.Join(col.Path, ".")
					resultRow = append(resultRow, row[colName])
				}
			} else {
				for _, col := range query.Columns {
					resultRow = append(resultRow, row[col])
				}
			}

			result.Rows = append(result.Rows, resultRow)

			// Check limit
			if query.Limit > 0 && int64(len(result.Rows)) >= query.Limit {
				return result, nil
			}
		}

		offset += int64(len(rows))
	}

	return result, nil
}

// SelectQuery represents a parsed SELECT query.
type SelectQuery struct {
	Where   *Condition
	From    string
	Columns []string
	Limit   int64
}

// SelectResult contains the query result.
type SelectResult struct {
	Columns []string
	Rows    [][]interface{}
}

// Condition represents a WHERE condition.
type Condition struct {
	Value    interface{}
	Left     *Condition
	Right    *Condition
	Column   string
	Operator string
	Type     ConditionType
}

// ConditionType represents the type of condition.
type ConditionType int

const (
	ConditionTypeComparison ConditionType = iota
	ConditionTypeAnd
	ConditionTypeOr
	ConditionTypeNot
	ConditionTypeIsNull
	ConditionTypeIsNotNull
	ConditionTypeLike
	ConditionTypeBetween
	ConditionTypeIn
)

// evaluateCondition evaluates a condition against a row.
func evaluateCondition(cond *Condition, row map[string]interface{}) (bool, error) {
	if cond == nil {
		return true, nil
	}

	switch cond.Type {
	case ConditionTypeComparison:
		return evaluateComparison(cond, row)
	case ConditionTypeAnd:
		left, err := evaluateCondition(cond.Left, row)
		if err != nil || !left {
			return false, err
		}

		return evaluateCondition(cond.Right, row)
	case ConditionTypeOr:
		left, err := evaluateCondition(cond.Left, row)
		if err != nil {
			return false, err
		}

		if left {
			return true, nil
		}

		return evaluateCondition(cond.Right, row)
	case ConditionTypeNot:
		result, err := evaluateCondition(cond.Left, row)
		return !result, err
	case ConditionTypeIsNull:
		val := row[cond.Column]
		return val == nil, nil
	case ConditionTypeIsNotNull:
		val := row[cond.Column]
		return val != nil, nil
	case ConditionTypeLike:
		return evaluateLike(cond, row)
	case ConditionTypeBetween:
		return evaluateBetween(cond, row)
	case ConditionTypeIn:
		return evaluateIn(cond, row)
	default:
		return false, fmt.Errorf("unsupported condition type: %d", cond.Type)
	}
}

// evaluateComparison evaluates a comparison condition.
func evaluateComparison(cond *Condition, row map[string]interface{}) (bool, error) {
	val := row[cond.Column]
	if val == nil {
		return false, nil
	}

	switch cond.Operator {
	case "=", "==":
		return compareValues(val, cond.Value) == 0, nil
	case "!=", "<>":
		return compareValues(val, cond.Value) != 0, nil
	case "<":
		return compareValues(val, cond.Value) < 0, nil
	case "<=":
		return compareValues(val, cond.Value) <= 0, nil
	case ">":
		return compareValues(val, cond.Value) > 0, nil
	case ">=":
		return compareValues(val, cond.Value) >= 0, nil
	default:
		return false, fmt.Errorf("unsupported operator: %s", cond.Operator)
	}
}

// compareValues compares two values.
func compareValues(a, b interface{}) int {
	// Convert to strings for comparison
	aStr := fmt.Sprint(a)
	bStr := fmt.Sprint(b)

	// Try numeric comparison first
	var aNum, bNum float64
	if _, err := fmt.Sscanf(aStr, "%f", &aNum); err == nil {
		if _, err := fmt.Sscanf(bStr, "%f", &bNum); err == nil {
			if aNum < bNum {
				return -1
			} else if aNum > bNum {
				return 1
			}

			return 0
		}
	}

	// Fall back to string comparison
	return strings.Compare(aStr, bStr)
}

// evaluateLike evaluates a LIKE condition.
func evaluateLike(cond *Condition, row map[string]interface{}) (bool, error) {
	val := row[cond.Column]
	if val == nil {
		return false, nil
	}

	strVal := fmt.Sprint(val)
	pattern := fmt.Sprint(cond.Value)

	// Convert SQL LIKE pattern to regex
	pattern = strings.ReplaceAll(pattern, "%", ".*")
	pattern = strings.ReplaceAll(pattern, "_", ".")
	pattern = "^" + pattern + "$"

	// Simple pattern matching
	return matchPattern(strVal, pattern), nil
}

// matchPattern performs simple pattern matching.
func matchPattern(value, pattern string) bool {
	// Simplified implementation
	if strings.Contains(pattern, ".*") {
		parts := strings.Split(pattern, ".*")
		pos := 0

		for _, part := range parts {
			part = strings.TrimPrefix(part, "^")

			part = strings.TrimSuffix(part, "$")
			if part == "" {
				continue
			}

			idx := strings.Index(value[pos:], part)
			if idx < 0 {
				return false
			}

			pos += idx + len(part)
		}

		return true
	}

	cleanPattern := strings.TrimPrefix(strings.TrimSuffix(pattern, "$"), "^")

	return value == cleanPattern
}

// evaluateBetween evaluates a BETWEEN condition.
func evaluateBetween(cond *Condition, row map[string]interface{}) (bool, error) {
	val := row[cond.Column]
	if val == nil {
		return false, nil
	}

	// Value should be a slice with [min, max]
	bounds, ok := cond.Value.([]interface{})
	if !ok || len(bounds) != 2 {
		return false, errors.New("invalid BETWEEN bounds")
	}

	return compareValues(val, bounds[0]) >= 0 && compareValues(val, bounds[1]) <= 0, nil
}

// evaluateIn evaluates an IN condition.
func evaluateIn(cond *Condition, row map[string]interface{}) (bool, error) {
	val := row[cond.Column]
	if val == nil {
		return false, nil
	}

	// Value should be a slice
	values, ok := cond.Value.([]interface{})
	if !ok {
		return false, errors.New("invalid IN values")
	}

	for _, v := range values {
		if compareValues(val, v) == 0 {
			return true, nil
		}
	}

	return false, nil
}

// Decompress decompresses data based on codec.
func Decompress(data []byte, codec CompressionCodec) ([]byte, error) {
	switch codec {
	case CodecUncompressed:
		return data, nil

	case CodecGzip:
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}

		defer func() { _ = reader.Close() }()

		return io.ReadAll(reader)

	case CodecSnappy:
		// Would need snappy library
		return nil, errors.New("snappy decompression not implemented")

	case CodecLZ4:
		// Would need LZ4 library
		return nil, errors.New("lz4 decompression not implemented")

	case CodecZstd:
		// Would need zstd library
		return nil, errors.New("zstd decompression not implemented")

	default:
		return nil, fmt.Errorf("unsupported codec: %d", codec)
	}
}

// OutputFormat represents the output format for query results.
type OutputFormat string

const (
	OutputFormatJSON OutputFormat = "JSON"
	OutputFormatCSV  OutputFormat = "CSV"
)

// FormatResult formats the query result.
func FormatResult(result *SelectResult, format OutputFormat) ([]byte, error) {
	switch format {
	case OutputFormatJSON:
		return formatJSON(result)
	case OutputFormatCSV:
		return formatCSV(result)
	default:
		return nil, fmt.Errorf("unsupported output format: %s", format)
	}
}

// formatJSON formats result as JSON lines.
func formatJSON(result *SelectResult) ([]byte, error) {
	var buf bytes.Buffer

	for _, row := range result.Rows {
		obj := make(map[string]interface{})

		for i, col := range result.Columns {
			if i < len(row) {
				obj[col] = row[i]
			}
		}

		data, err := json.Marshal(obj)
		if err != nil {
			return nil, err
		}

		buf.Write(data)
		buf.WriteByte('\n')
	}

	return buf.Bytes(), nil
}

// formatCSV formats result as CSV.
func formatCSV(result *SelectResult) ([]byte, error) {
	var buf bytes.Buffer

	// Header
	buf.WriteString(strings.Join(result.Columns, ","))
	buf.WriteByte('\n')

	// Rows
	for _, row := range result.Rows {
		var vals []string
		for _, v := range row {
			vals = append(vals, formatCSVValue(v))
		}

		buf.WriteString(strings.Join(vals, ","))
		buf.WriteByte('\n')
	}

	return buf.Bytes(), nil
}

// formatCSVValue formats a value for CSV output.
func formatCSVValue(v interface{}) string {
	if v == nil {
		return ""
	}

	s := fmt.Sprint(v)

	// Escape quotes and wrap if needed
	if strings.ContainsAny(s, ",\"\n") {
		s = "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
	}

	return s
}
