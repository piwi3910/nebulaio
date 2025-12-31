// Package s3select implements SQL query execution for S3 Select
// supporting CSV, JSON, and Parquet file formats.
package s3select

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Format type constants.
const (
	formatCSV     = "CSV"
	formatJSON    = "JSON"
	formatParquet = "Parquet"
)

// Query represents a parsed S3 Select SQL query.
type Query struct {
	Where     *Condition
	FromAlias string
	Columns   []Column
	Limit     int
	SelectAll bool
}

// Column represents a selected column.
type Column struct {
	Name     string
	Alias    string
	Function string // COUNT, SUM, AVG, MIN, MAX
	IsIndex  bool
	Index    int
}

// Condition represents a WHERE condition.
type Condition struct {
	Right    interface{}
	And      *Condition
	Or       *Condition
	Left     string
	Operator string
}

// InputFormat specifies the input format configuration.
type InputFormat struct {
	Type            string // CSV, JSON, Parquet
	CSVConfig       *CSVConfig
	JSONConfig      *JSONConfig
	ParquetConfig   *ParquetConfig
	CompressionType string // NONE, GZIP, BZIP2, ZSTD
}

// CSVConfig configures CSV parsing.
type CSVConfig struct {
	FileHeaderInfo             string // USE, IGNORE, NONE
	Comments                   string
	QuoteEscapeCharacter       string
	RecordDelimiter            string
	FieldDelimiter             string
	QuoteCharacter             string
	AllowQuotedRecordDelimiter bool
}

// JSONConfig configures JSON parsing.
type JSONConfig struct {
	Type string // DOCUMENT, LINES
}

// ParquetConfig configures Parquet parsing.
type ParquetConfig struct {
	// No additional config needed
}

// OutputFormat specifies the output format configuration.
type OutputFormat struct {
	CSVConfig  *CSVOutputConfig
	JSONConfig *JSONOutputConfig
	Type       string
}

// CSVOutputConfig configures CSV output.
type CSVOutputConfig struct {
	QuoteFields     string // ALWAYS, ASNEEDED
	QuoteEscapeChar string
	RecordDelimiter string
	FieldDelimiter  string
	QuoteCharacter  string
}

// JSONOutputConfig configures JSON output.
type JSONOutputConfig struct {
	RecordDelimiter string
}

// Result contains the query execution result.
type Result struct {
	Records        []byte
	BytesScanned   int64
	BytesProcessed int64
	BytesReturned  int64
}

// Engine executes S3 Select queries.
type Engine struct {
	outputFormat OutputFormat
	inputFormat  InputFormat
}

// NewEngine creates a new S3 Select engine.
func NewEngine(input InputFormat, output OutputFormat) *Engine {
	// Set defaults
	if input.CSVConfig == nil && input.Type == formatCSV {
		input.CSVConfig = &CSVConfig{
			FileHeaderInfo:  "USE",
			RecordDelimiter: "\n",
			FieldDelimiter:  ",",
			QuoteCharacter:  "\"",
		}
	}

	if input.JSONConfig == nil && input.Type == formatJSON {
		input.JSONConfig = &JSONConfig{
			Type: "LINES",
		}
	}

	if output.CSVConfig == nil && output.Type == formatCSV {
		output.CSVConfig = &CSVOutputConfig{
			QuoteFields:     "ASNEEDED",
			RecordDelimiter: "\n",
			FieldDelimiter:  ",",
			QuoteCharacter:  "\"",
		}
	}

	if output.JSONConfig == nil && output.Type == formatJSON {
		output.JSONConfig = &JSONOutputConfig{
			RecordDelimiter: "\n",
		}
	}

	return &Engine{
		inputFormat:  input,
		outputFormat: output,
	}
}

// Execute runs the SQL query on the data.
func (e *Engine) Execute(data []byte, sql string) (*Result, error) {
	// Parse the SQL query
	query, err := ParseSQL(sql)
	if err != nil {
		return nil, fmt.Errorf("failed to parse SQL: %w", err)
	}

	bytesScanned := int64(len(data))

	// Parse records based on input format
	var records []Record

	switch e.inputFormat.Type {
	case formatCSV:
		records, err = e.parseCSV(data)
	case formatJSON:
		records, err = e.parseJSON(data)
	case formatParquet:
		records, err = e.parseParquet(data)
	default:
		return nil, fmt.Errorf("unsupported input format: %s", e.inputFormat.Type)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to parse input: %w", err)
	}

	bytesProcessed := int64(len(data))

	// Execute query
	results, err := e.executeQuery(query, records)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}

	// Format output
	output, err := e.formatOutput(results, query)
	if err != nil {
		return nil, fmt.Errorf("failed to format output: %w", err)
	}

	return &Result{
		Records:        output,
		BytesScanned:   bytesScanned,
		BytesProcessed: bytesProcessed,
		BytesReturned:  int64(len(output)),
	}, nil
}

// Record represents a data record.
type Record struct {
	Fields  map[string]interface{}
	Columns []string // Ordered column names
}

// parseCSV parses CSV data into records.
func (e *Engine) parseCSV(data []byte) ([]Record, error) {
	config := e.inputFormat.CSVConfig

	reader := csv.NewReader(strings.NewReader(string(data)))

	// Configure CSV reader
	if len(config.FieldDelimiter) > 0 {
		reader.Comma = rune(config.FieldDelimiter[0])
	}

	if len(config.Comments) > 0 {
		reader.Comment = rune(config.Comments[0])
	}

	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1 // Variable number of fields

	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	if len(rows) == 0 {
		return nil, nil
	}

	var headers []string

	startRow := 0

	switch config.FileHeaderInfo {
	case "USE":
		headers = rows[0]
		startRow = 1
	case "IGNORE":
		// Skip header but use positional names
		for i := range rows[0] {
			headers = append(headers, fmt.Sprintf("_%d", i+1))
		}

		startRow = 1
	case "NONE":
		// Use positional names
		for i := range rows[0] {
			headers = append(headers, fmt.Sprintf("_%d", i+1))
		}
	default:
		// Default to USE
		headers = rows[0]
		startRow = 1
	}

	var records []Record

	for i := startRow; i < len(rows); i++ {
		row := rows[i]
		fields := make(map[string]interface{})

		for j, val := range row {
			if j < len(headers) {
				fields[headers[j]] = parseValue(val)
				fields[fmt.Sprintf("_%d", j+1)] = parseValue(val) // Also index access
			}
		}

		records = append(records, Record{Fields: fields, Columns: headers})
	}

	return records, nil
}

// parseJSON parses JSON data into records.
func (e *Engine) parseJSON(data []byte) ([]Record, error) {
	config := e.inputFormat.JSONConfig

	var records []Record

	if config.Type == "LINES" {
		// JSON Lines format
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			var obj map[string]interface{}

			err := json.Unmarshal([]byte(line), &obj)
			if err != nil {
				// Try as array
				var arr []interface{}

				err := json.Unmarshal([]byte(line), &arr)
				if err != nil {
					continue
				}

				for _, item := range arr {
					if rec, ok := item.(map[string]interface{}); ok {
						records = append(records, Record{Fields: rec, Columns: getKeys(rec)})
					}
				}

				continue
			}

			records = append(records, Record{Fields: obj, Columns: getKeys(obj)})
		}
	} else {
		// DOCUMENT format - single JSON object or array
		var obj interface{}

		err := json.Unmarshal(data, &obj)
		if err != nil {
			return nil, err
		}

		switch v := obj.(type) {
		case map[string]interface{}:
			records = append(records, Record{Fields: v, Columns: getKeys(v)})
		case []interface{}:
			for _, item := range v {
				if rec, ok := item.(map[string]interface{}); ok {
					records = append(records, Record{Fields: rec, Columns: getKeys(rec)})
				}
			}
		}
	}

	return records, nil
}

// parseParquet parses Parquet data into records.
func (e *Engine) parseParquet(data []byte) ([]Record, error) {
	// Parquet requires a more complex implementation using a library
	// For now, return an error indicating limited support
	return nil, errors.New("parquet format requires the parquet-go library; basic support only")
}

// executeQuery executes the parsed query on records.
func (e *Engine) executeQuery(query *Query, records []Record) ([]Record, error) {
	results := make([]Record, 0, len(records))

	for _, record := range records {
		// Apply WHERE filter
		if query.Where != nil {
			if !evaluateCondition(query.Where, record) {
				continue
			}
		}

		// Check LIMIT
		if query.Limit > 0 && len(results) >= query.Limit {
			break
		}

		results = append(results, record)
	}

	// Handle aggregate functions
	if len(query.Columns) > 0 && query.Columns[0].Function != "" {
		return e.executeAggregates(query, results)
	}

	return results, nil
}

// executeAggregates handles aggregate functions.
func (e *Engine) executeAggregates(query *Query, records []Record) ([]Record, error) {
	result := Record{Fields: make(map[string]interface{})}
	columns := make([]string, 0, len(query.Columns))

	for _, col := range query.Columns {
		alias := e.getColumnAlias(col)
		columns = append(columns, alias)

		value := e.calculateAggregate(col, records)
		result.Fields[alias] = value
	}

	result.Columns = columns

	return []Record{result}, nil
}

// getColumnAlias returns the column alias or generates one from function and name.
func (e *Engine) getColumnAlias(col Column) string {
	if col.Alias != "" {
		return col.Alias
	}
	return fmt.Sprintf("%s(%s)", col.Function, col.Name)
}

// calculateAggregate dispatches to the appropriate aggregate calculation.
func (e *Engine) calculateAggregate(col Column, records []Record) interface{} {
	switch strings.ToUpper(col.Function) {
	case "COUNT":
		return e.calculateCount(col, records)
	case "SUM":
		return e.calculateSum(col, records)
	case "AVG":
		return e.calculateAvg(col, records)
	case "MIN":
		return e.calculateMin(col, records)
	case "MAX":
		return e.calculateMax(col, records)
	default:
		return nil
	}
}

// calculateCount counts records with the specified column.
func (e *Engine) calculateCount(col Column, records []Record) int {
	if col.Name == "*" {
		return len(records)
	}

	count := 0
	for _, r := range records {
		if _, ok := r.Fields[col.Name]; ok {
			count++
		}
	}

	return count
}

// calculateSum sums numeric values in the specified column.
func (e *Engine) calculateSum(col Column, records []Record) float64 {
	var sum float64

	for _, r := range records {
		if v, ok := r.Fields[col.Name]; ok {
			sum += toFloat64(v)
		}
	}

	return sum
}

// calculateAvg calculates the average of numeric values in the specified column.
func (e *Engine) calculateAvg(col Column, records []Record) float64 {
	var sum float64
	var count int

	for _, r := range records {
		if v, ok := r.Fields[col.Name]; ok {
			sum += toFloat64(v)
			count++
		}
	}

	if count > 0 {
		return sum / float64(count)
	}

	return 0.0
}

// calculateMin finds the minimum numeric value in the specified column.
func (e *Engine) calculateMin(col Column, records []Record) interface{} {
	var minVal *float64

	for _, r := range records {
		if v, ok := r.Fields[col.Name]; ok {
			f := toFloat64(v)
			if minVal == nil || f < *minVal {
				minVal = &f
			}
		}
	}

	if minVal != nil {
		return *minVal
	}

	return nil
}

// calculateMax finds the maximum numeric value in the specified column.
func (e *Engine) calculateMax(col Column, records []Record) interface{} {
	var maxVal *float64

	for _, r := range records {
		if v, ok := r.Fields[col.Name]; ok {
			f := toFloat64(v)
			if maxVal == nil || f > *maxVal {
				maxVal = &f
			}
		}
	}

	if maxVal != nil {
		return *maxVal
	}

	return nil
}

// formatOutput formats records to output format.
func (e *Engine) formatOutput(records []Record, query *Query) ([]byte, error) {
	var output strings.Builder

	for _, record := range records {
		keys, values := e.extractColumnValues(record, query)
		if err := e.writeFormattedRecord(&output, keys, values); err != nil {
			return nil, err
		}
	}

	return []byte(output.String()), nil
}

func (e *Engine) extractColumnValues(record Record, query *Query) ([]string, []interface{}) {
	var (
		values []interface{}
		keys   []string
	)

	if query.SelectAll {
		for _, col := range record.Columns {
			keys = append(keys, col)
			values = append(values, record.Fields[col])
		}
		return keys, values
	}

	for _, col := range query.Columns {
		name := e.getColumnName(col)
		keys = append(keys, name)
		values = append(values, e.getColumnValue(record, col, name))
	}

	return keys, values
}

func (e *Engine) getColumnName(col Column) string {
	switch {
	case col.Function != "":
		if col.Alias != "" {
			return col.Alias
		}
		return fmt.Sprintf("%s(%s)", col.Function, col.Name)
	case col.Alias != "":
		return col.Alias
	default:
		return col.Name
	}
}

func (e *Engine) getColumnValue(record Record, col Column, name string) interface{} {
	switch {
	case col.Function != "":
		return record.Fields[name]
	case col.IsIndex:
		key := fmt.Sprintf("_%d", col.Index)
		return record.Fields[key]
	default:
		return record.Fields[col.Name]
	}
}

func (e *Engine) writeFormattedRecord(output *strings.Builder, keys []string, values []interface{}) error {
	switch e.outputFormat.Type {
	case formatJSON:
		return e.writeJSONRecord(output, keys, values)
	case formatCSV:
		e.writeCSVRecord(output, values)
		return nil
	default:
		return e.writeDefaultRecord(output, values)
	}
}

func (e *Engine) writeJSONRecord(output *strings.Builder, keys []string, values []interface{}) error {
	obj := make(map[string]interface{})

	for i, key := range keys {
		if i < len(values) {
			obj[key] = values[i]
		}
	}

	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		return err
	}

	output.Write(jsonBytes)
	output.WriteString(e.outputFormat.JSONConfig.RecordDelimiter)

	return nil
}

func (e *Engine) writeCSVRecord(output *strings.Builder, values []interface{}) {
	for i, val := range values {
		if i > 0 {
			output.WriteString(e.outputFormat.CSVConfig.FieldDelimiter)
		}
		output.WriteString(formatCSVValue(val, e.outputFormat.CSVConfig))
	}

	output.WriteString(e.outputFormat.CSVConfig.RecordDelimiter)
}

func (e *Engine) writeDefaultRecord(output *strings.Builder, values []interface{}) error {
	jsonBytes, err := json.Marshal(values)
	if err != nil {
		return err
	}

	output.Write(jsonBytes)
	output.WriteString("\n")

	return nil
}

// ParseSQL parses an S3 Select SQL query.
func ParseSQL(sql string) (*Query, error) {
	sql = strings.TrimSpace(sql)

	// Case insensitive
	lowerSQL := strings.ToLower(sql)

	if !strings.HasPrefix(lowerSQL, "select") {
		return nil, errors.New("only SELECT queries are supported")
	}

	query := &Query{}

	// Extract LIMIT
	limitRegex := regexp.MustCompile(`(?i)\s+limit\s+(\d+)\s*$`)
	if matches := limitRegex.FindStringSubmatch(sql); len(matches) > 1 {
		query.Limit, _ = strconv.Atoi(matches[1])
		sql = limitRegex.ReplaceAllString(sql, "")
	}

	// Extract WHERE clause
	whereRegex := regexp.MustCompile(`(?i)\s+where\s+(.+)$`)
	if matches := whereRegex.FindStringSubmatch(sql); len(matches) > 1 {
		where, err := parseWhereClause(matches[1])
		if err != nil {
			return nil, err
		}

		query.Where = where
		sql = whereRegex.ReplaceAllString(sql, "")
	}

	// Extract FROM clause
	fromRegex := regexp.MustCompile(`(?i)\s+from\s+(\S+)`)
	if matches := fromRegex.FindStringSubmatch(sql); len(matches) > 1 {
		query.FromAlias = matches[1]
		sql = fromRegex.ReplaceAllString(sql, "")
	}

	// Extract columns (between SELECT and FROM/WHERE/LIMIT)
	selectRegex := regexp.MustCompile(`(?i)^select\s+(.+)$`)
	if matches := selectRegex.FindStringSubmatch(sql); len(matches) > 1 {
		columns, err := parseColumns(strings.TrimSpace(matches[1]))
		if err != nil {
			return nil, err
		}

		query.Columns = columns
		if len(columns) == 1 && columns[0].Name == "*" && columns[0].Function == "" {
			query.SelectAll = true
		}
	}

	return query, nil
}

// parseColumns parses the column list.
func parseColumns(columnsStr string) ([]Column, error) {
	// Handle simple cases
	columnsStr = strings.TrimSpace(columnsStr)
	if columnsStr == "*" {
		return []Column{{Name: "*"}}, nil
	}

	// Split by comma, handling parentheses
	parts := splitByComma(columnsStr)
	columns := make([]Column, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		col := Column{}

		// Check for alias (AS keyword)
		aliasRegex := regexp.MustCompile(`(?i)(.+)\s+as\s+(\w+)$`)
		if matches := aliasRegex.FindStringSubmatch(part); len(matches) > 2 {
			col.Alias = matches[2]
			part = strings.TrimSpace(matches[1])
		}

		// Check for aggregate functions
		funcRegex := regexp.MustCompile(`(?i)(COUNT|SUM|AVG|MIN|MAX)\s*\(\s*(\*|\w+)\s*\)`)
		if matches := funcRegex.FindStringSubmatch(part); len(matches) > 2 {
			col.Function = strings.ToUpper(matches[1])
			col.Name = matches[2]
			columns = append(columns, col)

			continue
		}

		// Check for index access (_1, _2, etc.)
		indexRegex := regexp.MustCompile(`^_(\d+)$`)
		if matches := indexRegex.FindStringSubmatch(part); len(matches) > 1 {
			col.IsIndex = true
			col.Index, _ = strconv.Atoi(matches[1])
			col.Name = part
			columns = append(columns, col)

			continue
		}

		// Check for s.* or alias.*
		if strings.HasSuffix(part, ".*") {
			col.Name = "*"
			columns = append(columns, col)

			continue
		}

		// Check for field access (s.field)
		if strings.Contains(part, ".") {
			parts := strings.SplitN(part, ".", 2)
			if len(parts) == 2 {
				col.Name = parts[1]
			} else {
				col.Name = part
			}
		} else {
			col.Name = part
		}

		columns = append(columns, col)
	}

	return columns, nil
}

// parseWhereClause parses a WHERE condition.
func parseWhereClause(where string) (*Condition, error) {
	where = strings.TrimSpace(where)

	andRegex := regexp.MustCompile(`(?i)\s+and\s+`)
	orRegex := regexp.MustCompile(`(?i)\s+or\s+`)

	if andParts := andRegex.Split(where, 2); len(andParts) == 2 {
		return parseAndCondition(andParts)
	}

	if orParts := orRegex.Split(where, 2); len(orParts) == 2 {
		return parseOrCondition(orParts)
	}

	return parseSimpleCondition(where)
}

func parseAndCondition(parts []string) (*Condition, error) {
	left, err := parseWhereClause(parts[0])
	if err != nil {
		return nil, err
	}

	right, err := parseWhereClause(parts[1])
	if err != nil {
		return nil, err
	}

	left.And = right
	return left, nil
}

func parseOrCondition(parts []string) (*Condition, error) {
	left, err := parseWhereClause(parts[0])
	if err != nil {
		return nil, err
	}

	right, err := parseWhereClause(parts[1])
	if err != nil {
		return nil, err
	}

	left.Or = right
	return left, nil
}

func parseSimpleCondition(where string) (*Condition, error) {
	operators := []string{">=", "<=", "!=", "<>", "=", ">", "<", " LIKE ", " like ", " IS NULL", " is null", " IS NOT NULL", " is not null"}
	for _, op := range operators {
		if strings.Contains(where, op) {
			parts := strings.SplitN(where, op, 2)
			if len(parts) == 2 {
				left := parseLeftOperand(parts[0])
				right := parseRightOperand(parts[1], op)

				return &Condition{
					Left:     left,
					Operator: strings.TrimSpace(op),
					Right:    right,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("failed to parse WHERE clause: %s", where)
}

func parseLeftOperand(leftPart string) string {
	left := strings.TrimSpace(leftPart)

	if strings.Contains(left, ".") {
		leftParts := strings.SplitN(left, ".", 2)
		if len(leftParts) == 2 {
			left = leftParts[1]
		}
	}

	return left
}

func parseRightOperand(rightPart, operator string) interface{} {
	right := strings.TrimSpace(rightPart)
	rightLower := strings.ToLower(right)

	switch {
	case strings.HasPrefix(rightLower, "null") || strings.Contains(strings.ToLower(operator), "null"):
		return nil
	case strings.HasPrefix(right, "'") && strings.HasSuffix(right, "'"):
		return right[1 : len(right)-1]
	case strings.HasPrefix(right, "\"") && strings.HasSuffix(right, "\""):
		return right[1 : len(right)-1]
	default:
		return parseNumericOrBoolOrString(right)
	}
}

func parseNumericOrBoolOrString(value string) interface{} {
	f, floatErr := strconv.ParseFloat(value, 64)
	if floatErr == nil {
		return f
	}

	b, boolErr := strconv.ParseBool(value)
	if boolErr == nil {
		return b
	}

	return value
}

// evaluateCondition evaluates a condition against a record.
func evaluateCondition(cond *Condition, record Record) bool {
	result := evaluateSingleCondition(cond, record)

	if cond.And != nil {
		result = result && evaluateCondition(cond.And, record)
	}

	if cond.Or != nil {
		result = result || evaluateCondition(cond.Or, record)
	}

	return result
}

func evaluateSingleCondition(cond *Condition, record Record) bool {
	leftVal := record.Fields[cond.Left]

	op := strings.ToUpper(strings.TrimSpace(cond.Operator))

	switch op {
	case "=":
		return compareValues(leftVal, cond.Right) == 0
	case "!=", "<>":
		return compareValues(leftVal, cond.Right) != 0
	case ">":
		return compareValues(leftVal, cond.Right) > 0
	case ">=":
		return compareValues(leftVal, cond.Right) >= 0
	case "<":
		return compareValues(leftVal, cond.Right) < 0
	case "<=":
		return compareValues(leftVal, cond.Right) <= 0
	case "LIKE":
		return matchLike(toString(leftVal), toString(cond.Right))
	case "IS NULL":
		return leftVal == nil
	case "IS NOT NULL":
		return leftVal != nil
	}

	return false
}

// Helper functions

func getKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	return keys
}

func parseValue(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	f, floatErr := strconv.ParseFloat(s, 64)
	if floatErr == nil {
		if strings.Contains(s, ".") {
			return f
		}

		return int64(f)
	}

	b, boolErr := strconv.ParseBool(s)
	if boolErr == nil {
		return b
	}

	return s
}

func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case int32:
		return float64(val)
	case string:
		f, err := strconv.ParseFloat(val, 64)
		if err == nil {
			return f
		}
	}

	return 0
}

func toString(v interface{}) string {
	if v == nil {
		return ""
	}

	return fmt.Sprintf("%v", v)
}

func compareValues(a, b interface{}) int {
	aFloat := toFloat64(a)
	bFloat := toFloat64(b)

	// Try numeric comparison first
	if aFloat != 0 || bFloat != 0 {
		if aFloat < bFloat {
			return -1
		} else if aFloat > bFloat {
			return 1
		}

		return 0
	}

	// String comparison
	aStr := toString(a)
	bStr := toString(b)

	return strings.Compare(aStr, bStr)
}

func matchLike(value, pattern string) bool {
	// Convert SQL LIKE pattern to regex
	pattern = regexp.QuoteMeta(pattern)
	pattern = strings.ReplaceAll(pattern, "%", ".*")
	pattern = strings.ReplaceAll(pattern, "_", ".")
	pattern = "^" + pattern + "$"

	matched, _ := regexp.MatchString("(?i)"+pattern, value)

	return matched
}

func formatCSVValue(v interface{}, config *CSVOutputConfig) string {
	s := toString(v)
	needsQuote := config.QuoteFields == "ALWAYS" ||
		strings.Contains(s, config.FieldDelimiter) ||
		strings.Contains(s, config.QuoteCharacter) ||
		strings.Contains(s, config.RecordDelimiter)

	if needsQuote {
		escaped := strings.ReplaceAll(s, config.QuoteCharacter, config.QuoteCharacter+config.QuoteCharacter)
		return config.QuoteCharacter + escaped + config.QuoteCharacter
	}

	return s
}

func splitByComma(s string) []string {
	var (
		parts   []string
		current strings.Builder
	)

	depth := 0

	for _, c := range s {
		switch c {
		case '(':
			depth++

			current.WriteRune(c)
		case ')':
			depth--

			current.WriteRune(c)
		case ',':
			if depth == 0 {
				parts = append(parts, current.String())
				current.Reset()
			} else {
				current.WriteRune(c)
			}
		default:
			current.WriteRune(c)
		}
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// ExecuteCSV is a convenience function for executing queries on CSV data.
func ExecuteCSV(data []byte, sql string, hasHeader bool) (*Result, error) {
	headerInfo := "USE"
	if !hasHeader {
		headerInfo = "NONE"
	}

	engine := NewEngine(
		InputFormat{
			Type: formatCSV,
			CSVConfig: &CSVConfig{
				FileHeaderInfo:  headerInfo,
				RecordDelimiter: "\n",
				FieldDelimiter:  ",",
				QuoteCharacter:  "\"",
			},
		},
		OutputFormat{
			Type: formatJSON,
			JSONConfig: &JSONOutputConfig{
				RecordDelimiter: "\n",
			},
		},
	)

	return engine.Execute(data, sql)
}

// ExecuteJSON is a convenience function for executing queries on JSON data.
func ExecuteJSON(data []byte, sql string, isDocument bool) (*Result, error) {
	jsonType := "LINES"
	if isDocument {
		jsonType = "DOCUMENT"
	}

	engine := NewEngine(
		InputFormat{
			Type: formatJSON,
			JSONConfig: &JSONConfig{
				Type: jsonType,
			},
		},
		OutputFormat{
			Type: formatJSON,
			JSONConfig: &JSONOutputConfig{
				RecordDelimiter: "\n",
			},
		},
	)

	return engine.Execute(data, sql)
}
