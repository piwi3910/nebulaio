package s3select

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Test data constants.
const testCSVData = `name,age,city
Alice,30,New York
Bob,25,Los Angeles
Charlie,35,Chicago`

func TestParseSQL(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		wantErr   bool
		selectAll bool
		limit     int
		numCols   int
	}{
		{
			name:      "SELECT *",
			sql:       "SELECT * FROM s3object",
			selectAll: true,
			numCols:   1,
		},
		{
			name:    "SELECT specific columns",
			sql:     "SELECT name, age FROM s3object",
			numCols: 2,
		},
		{
			name:    "SELECT with alias",
			sql:     "SELECT s.name, s.age FROM s3object s",
			numCols: 2,
		},
		{
			name:  "SELECT with LIMIT",
			sql:   "SELECT * FROM s3object LIMIT 10",
			limit: 10,
		},
		{
			name:    "SELECT with WHERE",
			sql:     "SELECT * FROM s3object WHERE age > 21",
			numCols: 1,
		},
		{
			name:    "SELECT with aggregate",
			sql:     "SELECT COUNT(*) FROM s3object",
			numCols: 1,
		},
		{
			name:    "SELECT with multiple aggregates",
			sql:     "SELECT SUM(amount), AVG(price) FROM s3object",
			numCols: 2,
		},
		{
			name:    "Invalid query",
			sql:     "INSERT INTO table VALUES (1)",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, err := ParseSQL(tt.sql)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.selectAll && !query.SelectAll {
				t.Error("expected SelectAll to be true")
			}

			if tt.limit != 0 && query.Limit != tt.limit {
				t.Errorf("expected limit %d, got %d", tt.limit, query.Limit)
			}

			if tt.numCols != 0 && len(query.Columns) != tt.numCols {
				t.Errorf("expected %d columns, got %d", tt.numCols, len(query.Columns))
			}
		})
	}
}

func TestExecuteCSVSelectStar(t *testing.T) {
	//nolint:unqueryvet // Testing SELECT * functionality which is valid in S3 Select
	result, err := ExecuteCSV([]byte(testCSVData), "SELECT * FROM s3object", true)
	if err != nil {
		t.Fatalf("ExecuteCSV failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(result.Records)), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 records, got %d", len(lines))
	}

	// Verify first record
	var record map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if record["name"] != "Alice" {
		t.Errorf("expected name Alice, got %v", record["name"])
	}
}

func TestExecuteCSVSelectColumns(t *testing.T) {
	result, err := ExecuteCSV([]byte(testCSVData), "SELECT name, city FROM s3object", true)
	if err != nil {
		t.Fatalf("ExecuteCSV failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(result.Records)), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 records, got %d", len(lines))
	}

	var record map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if len(record) != 2 {
		t.Errorf("expected 2 fields, got %d", len(record))
	}
}

func TestExecuteCSVWithWhere(t *testing.T) {
	result, err := ExecuteCSV([]byte(testCSVData), "SELECT * FROM s3object WHERE age > 28", true)
	if err != nil {
		t.Fatalf("ExecuteCSV failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(result.Records)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 records (age > 28), got %d", len(lines))
	}
}

func TestExecuteCSVWithLimit(t *testing.T) {
	csvData := `name,age,city
Alice,30,New York
Bob,25,Los Angeles
Charlie,35,Chicago
David,28,Seattle
Eve,32,Boston`

	result, err := ExecuteCSV([]byte(csvData), "SELECT * FROM s3object LIMIT 2", true)
	if err != nil {
		t.Fatalf("ExecuteCSV failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(result.Records)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 records, got %d", len(lines))
	}
}

func TestExecuteCSVCount(t *testing.T) {
	result, err := ExecuteCSV([]byte(testCSVData), "SELECT COUNT(*) FROM s3object", true)
	if err != nil {
		t.Fatalf("ExecuteCSV failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(result.Records)), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 record for COUNT, got %d", len(lines))
	}

	var record map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if record["COUNT(*)"] != float64(3) {
		t.Errorf("expected count 3, got %v", record["COUNT(*)"])
	}
}

func TestExecuteCSVSum(t *testing.T) {
	csvData := `product,price,quantity
Widget,10,5
Gadget,20,3
Gizmo,15,4`

	result, err := ExecuteCSV([]byte(csvData), "SELECT SUM(price) FROM s3object", true)
	if err != nil {
		t.Fatalf("ExecuteCSV failed: %v", err)
	}

	var record map[string]interface{}
	if err := json.Unmarshal(result.Records, &record); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if record["SUM(price)"] != float64(45) {
		t.Errorf("expected sum 45, got %v", record["SUM(price)"])
	}
}

func TestExecuteCSVAvg(t *testing.T) {
	csvData := `product,price,quantity
Widget,10,5
Gadget,20,3
Gizmo,15,4`

	result, err := ExecuteCSV([]byte(csvData), "SELECT AVG(price) FROM s3object", true)
	if err != nil {
		t.Fatalf("ExecuteCSV failed: %v", err)
	}

	var record map[string]interface{}
	if err := json.Unmarshal(result.Records, &record); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if record["AVG(price)"] != float64(15) {
		t.Errorf("expected avg 15, got %v", record["AVG(price)"])
	}
}

func TestExecuteJSONLines(t *testing.T) {
	jsonData := `{"name":"Alice","age":30}
{"name":"Bob","age":25}
{"name":"Charlie","age":35}`

	result, err := ExecuteJSON([]byte(jsonData), "SELECT * FROM s3object", false)
	if err != nil {
		t.Fatalf("ExecuteJSON failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(result.Records)), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 records, got %d", len(lines))
	}
}

func TestExecuteJSONDocument(t *testing.T) {
	jsonData := `[{"name":"Alice","age":30},{"name":"Bob","age":25}]`

	result, err := ExecuteJSON([]byte(jsonData), "SELECT * FROM s3object", true)
	if err != nil {
		t.Fatalf("ExecuteJSON failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(result.Records)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 records, got %d", len(lines))
	}
}

func TestExecuteJSONWithWhere(t *testing.T) {
	jsonData := `{"name":"Alice","age":30,"active":true}
{"name":"Bob","age":25,"active":false}
{"name":"Charlie","age":35,"active":true}`

	result, err := ExecuteJSON([]byte(jsonData), "SELECT name FROM s3object WHERE active = true", false)
	if err != nil {
		t.Fatalf("ExecuteJSON failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(result.Records)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 active records, got %d", len(lines))
	}
}

func TestExecuteWithLIKE(t *testing.T) {
	csvData := `name,email
Alice,alice@example.com
Bob,bob@test.org
Charlie,charlie@example.com`

	result, err := ExecuteCSV([]byte(csvData), "SELECT name FROM s3object WHERE email LIKE '%@example.com'", true)
	if err != nil {
		t.Fatalf("ExecuteCSV failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(result.Records)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 records with @example.com, got %d", len(lines))
	}
}

func TestExecuteCSVNoHeader(t *testing.T) {
	csvData := `Alice,30,New York
Bob,25,Los Angeles`

	result, err := ExecuteCSV([]byte(csvData), "SELECT _1, _2 FROM s3object", false)
	if err != nil {
		t.Fatalf("ExecuteCSV failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(result.Records)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 records, got %d", len(lines))
	}
}

func TestExecuteWithANDCondition(t *testing.T) {
	csvData := `name,age,city
Alice,30,New York
Bob,25,New York
Charlie,35,Chicago`

	result, err := ExecuteCSV([]byte(csvData), "SELECT name FROM s3object WHERE age > 28 AND city = 'New York'", true)
	if err != nil {
		t.Fatalf("ExecuteCSV failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(result.Records)), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 record (Alice), got %d", len(lines))
	}

	var record map[string]interface{}
	json.Unmarshal([]byte(lines[0]), &record)

	if record["name"] != "Alice" {
		t.Errorf("expected name Alice, got %v", record["name"])
	}
}

func TestExecuteWithORCondition(t *testing.T) {
	result, err := ExecuteCSV([]byte(testCSVData), "SELECT name FROM s3object WHERE age < 26 OR age > 34", true)
	if err != nil {
		t.Fatalf("ExecuteCSV failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(result.Records)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 records (Bob and Charlie), got %d", len(lines))
	}
}

func TestExecuteMinMax(t *testing.T) {
	csvData := `product,price
Widget,10
Gadget,50
Gizmo,15`

	// Test MIN
	result, err := ExecuteCSV([]byte(csvData), "SELECT MIN(price) FROM s3object", true)
	if err != nil {
		t.Fatalf("ExecuteCSV MIN failed: %v", err)
	}

	var record map[string]interface{}
	json.Unmarshal(result.Records, &record)

	if record["MIN(price)"] != float64(10) {
		t.Errorf("expected min 10, got %v", record["MIN(price)"])
	}

	// Test MAX
	result, err = ExecuteCSV([]byte(csvData), "SELECT MAX(price) FROM s3object", true)
	if err != nil {
		t.Fatalf("ExecuteCSV MAX failed: %v", err)
	}

	json.Unmarshal(result.Records, &record)

	if record["MAX(price)"] != float64(50) {
		t.Errorf("expected max 50, got %v", record["MAX(price)"])
	}
}

func TestOutputCSV(t *testing.T) {
	csvData := `name,age
Alice,30
Bob,25`

	engine := NewEngine(
		InputFormat{
			Type: "CSV",
			CSVConfig: &CSVConfig{
				FileHeaderInfo:  "USE",
				RecordDelimiter: "\n",
				FieldDelimiter:  ",",
			},
		},
		OutputFormat{
			Type: "CSV",
			CSVConfig: &CSVOutputConfig{
				QuoteFields:     "ASNEEDED",
				RecordDelimiter: "\n",
				FieldDelimiter:  ",",
			},
		},
	)

	result, err := engine.Execute([]byte(csvData), "SELECT * FROM s3object")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(result.Records)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 records, got %d", len(lines))
	}

	// CSV output should have comma-separated values
	if !strings.Contains(lines[0], ",") {
		t.Error("expected CSV output with commas")
	}
}

func TestBytesScannedProcessed(t *testing.T) {
	csvData := `name,age
Alice,30`

	result, err := ExecuteCSV([]byte(csvData), "SELECT * FROM s3object", true)
	if err != nil {
		t.Fatalf("ExecuteCSV failed: %v", err)
	}

	if result.BytesScanned != int64(len(csvData)) {
		t.Errorf("expected BytesScanned %d, got %d", len(csvData), result.BytesScanned)
	}

	if result.BytesProcessed != int64(len(csvData)) {
		t.Errorf("expected BytesProcessed %d, got %d", len(csvData), result.BytesProcessed)
	}

	if result.BytesReturned <= 0 {
		t.Error("expected BytesReturned > 0")
	}
}

func TestEmptyData(t *testing.T) {
	result, err := ExecuteCSV([]byte(""), "SELECT * FROM s3object", true)
	if err != nil {
		t.Fatalf("ExecuteCSV failed on empty data: %v", err)
	}

	if len(result.Records) != 0 {
		t.Errorf("expected empty result, got %d bytes", len(result.Records))
	}
}

func TestSpecialCharactersInCSV(t *testing.T) {
	csvData := `name,description
"Alice, Jr.","She said ""Hello"""
Bob,Simple text`

	result, err := ExecuteCSV([]byte(csvData), "SELECT * FROM s3object", true)
	if err != nil {
		t.Fatalf("ExecuteCSV failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(result.Records)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 records, got %d", len(lines))
	}

	var record map[string]interface{}
	json.Unmarshal([]byte(lines[0]), &record)

	if record["name"] != "Alice, Jr." {
		t.Errorf("expected name 'Alice, Jr.', got %v", record["name"])
	}
}

func TestNestedJSONAccess(t *testing.T) {
	jsonData := `{"user":{"name":"Alice","age":30},"active":true}
{"user":{"name":"Bob","age":25},"active":false}`

	result, err := ExecuteJSON([]byte(jsonData), "SELECT * FROM s3object WHERE active = true", false)
	if err != nil {
		t.Fatalf("ExecuteJSON failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(result.Records)), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 active record, got %d", len(lines))
	}
}

func BenchmarkExecuteCSV(b *testing.B) {
	// Generate test data
	var builder strings.Builder
	builder.WriteString("id,name,value\n")

	for i := range 1000 {
		builder.WriteString(fmt.Sprintf("%d,Item%d,%d\n", i, i, i*10))
	}

	data := []byte(builder.String())

	b.ResetTimer()

	for range b.N {
		_, _ = ExecuteCSV(data, "SELECT * FROM s3object WHERE value > 5000", true)
	}
}

func BenchmarkExecuteJSON(b *testing.B) {
	// Generate test data
	var builder strings.Builder
	for i := range 1000 {
		builder.WriteString(fmt.Sprintf(`{"id":%d,"name":"Item%d","value":%d}`, i, i, i*10))
		builder.WriteString("\n")
	}

	data := []byte(builder.String())

	b.ResetTimer()

	for range b.N {
		_, _ = ExecuteJSON(data, "SELECT * FROM s3object WHERE value > 5000", false)
	}
}
