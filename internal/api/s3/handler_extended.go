package s3

import (
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/piwi3910/nebulaio/internal/api/middleware"
	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/pkg/s3types"
)

// GetObjectAttributes returns object metadata without the object body
// This is a lighter-weight alternative to HeadObject when you only need specific attributes
func (h *Handler) GetObjectAttributes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")
	versionID := r.URL.Query().Get("versionId")

	// Parse x-amz-object-attributes header (comma-separated list)
	requestedAttrs := parseObjectAttributesHeader(r.Header.Get("x-amz-object-attributes"))
	if len(requestedAttrs) == 0 {
		writeS3Error(w, "InvalidArgument", "x-amz-object-attributes header is required", http.StatusBadRequest)
		return
	}

	// Get object metadata
	var meta *metadata.ObjectMeta
	var err error
	if versionID != "" {
		meta, err = h.object.HeadObjectVersion(ctx, bucketName, key, versionID)
	} else {
		meta, err = h.object.HeadObject(ctx, bucketName, key)
	}
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, key)
		return
	}

	// Build response based on requested attributes
	response := s3types.GetObjectAttributesOutput{}

	for _, attr := range requestedAttrs {
		switch attr {
		case "ETag":
			response.ETag = meta.ETag
		case "Checksum":
			// Checksums are stored in metadata if available
			if meta.Metadata != nil {
				checksum := &s3types.Checksum{}
				hasChecksum := false
				if v, ok := meta.Metadata["x-amz-checksum-crc32"]; ok {
					checksum.ChecksumCRC32 = v
					hasChecksum = true
				}
				if v, ok := meta.Metadata["x-amz-checksum-crc32c"]; ok {
					checksum.ChecksumCRC32C = v
					hasChecksum = true
				}
				if v, ok := meta.Metadata["x-amz-checksum-sha1"]; ok {
					checksum.ChecksumSHA1 = v
					hasChecksum = true
				}
				if v, ok := meta.Metadata["x-amz-checksum-sha256"]; ok {
					checksum.ChecksumSHA256 = v
					hasChecksum = true
				}
				if hasChecksum {
					response.Checksum = checksum
				}
			}
		case "ObjectParts":
			// Part count info from metadata
			if meta.Metadata != nil {
				if partsStr, ok := meta.Metadata["x-amz-mp-parts-count"]; ok {
					if partsCount, err := strconv.Atoi(partsStr); err == nil && partsCount > 0 {
						response.ObjectParts = &s3types.ObjectParts{
							PartsCount:      partsCount,
							TotalPartsCount: partsCount,
							IsTruncated:     false,
							MaxParts:        1000,
						}
					}
				}
			}
		case "StorageClass":
			response.StorageClass = meta.StorageClass
			if response.StorageClass == "" {
				response.StorageClass = "STANDARD"
			}
		case "ObjectSize":
			response.ObjectSize = meta.Size
		}
	}

	// Set response headers
	w.Header().Set("Last-Modified", meta.ModifiedAt.Format(http.TimeFormat))
	if versionID != "" {
		w.Header().Set("x-amz-version-id", versionID)
	}
	if meta.DeleteMarker {
		w.Header().Set("x-amz-delete-marker", "true")
	}

	writeXML(w, http.StatusOK, response)
}

// parseObjectAttributesHeader parses the x-amz-object-attributes header
func parseObjectAttributesHeader(header string) []string {
	if header == "" {
		return nil
	}
	attrs := strings.Split(header, ",")
	result := make([]string, 0, len(attrs))
	for _, attr := range attrs {
		trimmed := strings.TrimSpace(attr)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// SelectObjectContent executes SQL queries on object content (CSV, JSON, Parquet)
// This is a simplified implementation that supports basic SELECT queries
func (h *Handler) SelectObjectContent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")

	// Parse request body
	var selectReq s3types.SelectObjectContentInput
	if err := xml.NewDecoder(r.Body).Decode(&selectReq); err != nil {
		writeS3Error(w, "InvalidRequest", "Failed to parse request: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate input format
	if selectReq.InputSerialization.CSV == nil && selectReq.InputSerialization.JSON == nil {
		writeS3Error(w, "InvalidRequest", "InputSerialization must specify CSV or JSON format", http.StatusBadRequest)
		return
	}

	// Get object data
	data, _, err := h.object.GetObject(ctx, bucketName, key)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, key)
		return
	}
	defer func() { _ = data.Close() }()

	// Read all data
	content, err := io.ReadAll(data)
	if err != nil {
		writeS3Error(w, "InternalError", "Failed to read object: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Execute the query based on format
	var result []byte
	if selectReq.InputSerialization.CSV != nil {
		result, err = executeCSVSelect(content, selectReq.Expression, selectReq.OutputSerialization)
	} else if selectReq.InputSerialization.JSON != nil {
		result, err = executeJSONSelect(content, selectReq.Expression, selectReq.OutputSerialization)
	}

	if err != nil {
		writeS3Error(w, "InvalidRequest", "Query execution failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Write response as event stream
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("x-amz-request-id", middleware.GetRequestID(ctx))
	w.WriteHeader(http.StatusOK)

	// Write records message
	recordsMsg := createSelectEventMessage("Records", result)
	_, _ = w.Write(recordsMsg)

	// Write stats message
	statsMsg := createSelectEventMessage("Stats", []byte(fmt.Sprintf(
		`<Stats><BytesScanned>%d</BytesScanned><BytesProcessed>%d</BytesProcessed><BytesReturned>%d</BytesReturned></Stats>`,
		len(content), len(content), len(result),
	)))
	_, _ = w.Write(statsMsg)

	// Write end message
	endMsg := createSelectEventMessage("End", nil)
	_, _ = w.Write(endMsg)
}

// executeCSVSelect executes a SQL query on CSV data
func executeCSVSelect(data []byte, query string, outputFormat s3types.OutputSerialization) ([]byte, error) {
	// Parse CSV
	reader := csv.NewReader(strings.NewReader(string(data)))
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to parse CSV: %w", err)
	}

	if len(records) == 0 {
		return nil, nil
	}

	// Simple SELECT * implementation
	// A full implementation would parse the SQL and apply projections/filters
	query = strings.TrimSpace(strings.ToLower(query))
	if !strings.HasPrefix(query, "select") {
		return nil, fmt.Errorf("only SELECT queries are supported")
	}

	// For now, return all records
	var result strings.Builder
	for _, record := range records {
		if outputFormat.JSON != nil {
			// Output as JSON
			jsonBytes, _ := json.Marshal(record)
			result.Write(jsonBytes)
			result.WriteString("\n")
		} else {
			// Output as CSV
			for i, field := range record {
				if i > 0 {
					result.WriteString(",")
				}
				result.WriteString(field)
			}
			result.WriteString("\n")
		}
	}

	return []byte(result.String()), nil
}

// executeJSONSelect executes a SQL query on JSON data
func executeJSONSelect(data []byte, query string, outputFormat s3types.OutputSerialization) ([]byte, error) {
	// Parse JSON (assume JSON Lines format)
	lines := strings.Split(string(data), "\n")
	var records []map[string]interface{}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var record map[string]interface{}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			// Try parsing as array element
			var arr []interface{}
			if err := json.Unmarshal([]byte(line), &arr); err != nil {
				continue
			}
			for _, item := range arr {
				if rec, ok := item.(map[string]interface{}); ok {
					records = append(records, rec)
				}
			}
			continue
		}
		records = append(records, record)
	}

	// If input is a single JSON object or array
	if len(records) == 0 {
		var singleRecord map[string]interface{}
		if err := json.Unmarshal(data, &singleRecord); err == nil {
			records = append(records, singleRecord)
		} else {
			var arr []interface{}
			if err := json.Unmarshal(data, &arr); err == nil {
				for _, item := range arr {
					if rec, ok := item.(map[string]interface{}); ok {
						records = append(records, rec)
					}
				}
			}
		}
	}

	// Simple SELECT * implementation
	query = strings.TrimSpace(strings.ToLower(query))
	if !strings.HasPrefix(query, "select") {
		return nil, fmt.Errorf("only SELECT queries are supported")
	}

	// For now, return all records as JSON
	var result strings.Builder
	for _, record := range records {
		jsonBytes, _ := json.Marshal(record)
		result.Write(jsonBytes)
		result.WriteString("\n")
	}

	return []byte(result.String()), nil
}

// createSelectEventMessage creates an S3 Select event stream message
func createSelectEventMessage(eventType string, payload []byte) []byte {
	// Simplified event stream format
	// In production, this should follow the AWS binary event stream format
	header := fmt.Sprintf(":event-type:%s\n:content-type:application/octet-stream\n\n", eventType)
	result := make([]byte, 0, len(header)+len(payload)+1)
	result = append(result, []byte(header)...)
	result = append(result, payload...)
	result = append(result, '\n')
	return result
}

// RestoreObject initiates restore of an object from archive storage
func (h *Handler) RestoreObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")
	versionID := r.URL.Query().Get("versionId")

	// Parse restore request
	var restoreReq s3types.RestoreRequest
	if err := xml.NewDecoder(r.Body).Decode(&restoreReq); err != nil && err != io.EOF {
		writeS3Error(w, "InvalidRequest", "Failed to parse restore request: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Default restore days
	if restoreReq.Days == 0 {
		restoreReq.Days = 1
	}

	// Validate days
	if restoreReq.Days < 1 || restoreReq.Days > 365 {
		writeS3Error(w, "InvalidArgument", "Days must be between 1 and 365", http.StatusBadRequest)
		return
	}

	// Check if object exists and is in GLACIER or DEEP_ARCHIVE
	var meta *metadata.ObjectMeta
	var err error
	if versionID != "" {
		meta, err = h.object.HeadObjectVersion(ctx, bucketName, key, versionID)
	} else {
		meta, err = h.object.HeadObject(ctx, bucketName, key)
	}
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, key)
		return
	}

	// Check storage class
	if meta.StorageClass != "GLACIER" && meta.StorageClass != "DEEP_ARCHIVE" {
		writeS3Error(w, "InvalidObjectState", "Object storage class is not GLACIER or DEEP_ARCHIVE", http.StatusForbidden)
		return
	}

	// Check if restore is already in progress or completed (stored in metadata)
	if meta.Metadata != nil {
		if restoreStatus, ok := meta.Metadata["x-amz-restore-status"]; ok {
			if restoreStatus == "ongoing" {
				// Restore already in progress
				w.Header().Set("x-amz-restore", `ongoing-request="true"`)
				w.WriteHeader(http.StatusConflict)
				return
			}
			if restoreStatus == "completed" {
				if expiryStr, ok := meta.Metadata["x-amz-restore-expiry"]; ok {
					if expiryTime, err := time.Parse(time.RFC3339, expiryStr); err == nil && expiryTime.After(time.Now()) {
						// Object already restored
						w.Header().Set("x-amz-restore", fmt.Sprintf(`ongoing-request="false", expiry-date="%s"`,
							expiryTime.Format(time.RFC1123)))
						w.WriteHeader(http.StatusOK)
						return
					}
				}
			}
		}
	}

	// Initiate restore (in a real implementation, this would queue a background job)
	// For now, we'll mark the object as having a restore in progress
	tier := restoreReq.GlacierJobParameters.Tier
	if tier == "" {
		tier = "Standard"
	}

	expiryDate := time.Now().Add(time.Duration(restoreReq.Days) * 24 * time.Hour)

	// In a real implementation, we would:
	// 1. Queue a background job to copy from archive to accessible storage
	// 2. Update the object metadata with restore status
	// 3. The job would update the status when complete

	w.Header().Set("x-amz-restore", `ongoing-request="true"`)
	w.Header().Set("x-amz-restore-expiry-date", expiryDate.Format(time.RFC1123))
	w.Header().Set("x-amz-restore-tier", tier)
	w.WriteHeader(http.StatusAccepted)
}

// WriteGetObjectResponse writes a response on behalf of a Lambda function
// This is used for S3 Object Lambda
func (h *Handler) WriteGetObjectResponse(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	query := r.URL.Query()
	requestRoute := query.Get("x-amz-request-route")
	requestToken := query.Get("x-amz-request-token")

	if requestRoute == "" || requestToken == "" {
		writeS3Error(w, "InvalidArgument", "x-amz-request-route and x-amz-request-token are required", http.StatusBadRequest)
		return
	}

	// Parse response headers from request
	statusCode := http.StatusOK
	if sc := r.Header.Get("x-amz-fwd-status"); sc != "" {
		if parsed, err := strconv.Atoi(sc); err == nil {
			statusCode = parsed
		}
	}

	// Read response body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeS3Error(w, "InternalError", "Failed to read request body", http.StatusInternalServerError)
		return
	}

	// Forward headers
	if contentType := r.Header.Get("x-amz-fwd-header-Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if contentLength := r.Header.Get("x-amz-fwd-header-Content-Length"); contentLength != "" {
		w.Header().Set("Content-Length", contentLength)
	}
	if etag := r.Header.Get("x-amz-fwd-header-ETag"); etag != "" {
		w.Header().Set("ETag", etag)
	}
	if lastModified := r.Header.Get("x-amz-fwd-header-Last-Modified"); lastModified != "" {
		w.Header().Set("Last-Modified", lastModified)
	}

	// Write response
	w.WriteHeader(statusCode)
	_, _ = w.Write(body)
}

// GetBucketIntelligentTieringConfiguration gets intelligent tiering configuration
func (h *Handler) GetBucketIntelligentTieringConfiguration(w http.ResponseWriter, r *http.Request) {
	_ = chi.URLParam(r, "bucket") // bucketName - used for bucket lookup in production
	configID := r.URL.Query().Get("id")

	if configID == "" {
		writeS3Error(w, "InvalidArgument", "Configuration ID is required", http.StatusBadRequest)
		return
	}

	// In a real implementation, this would fetch the configuration from storage
	// For now, return a sample configuration
	response := s3types.IntelligentTieringConfiguration{
		ID:     configID,
		Status: "Enabled",
		Filter: &s3types.IntelligentTieringFilter{
			Prefix: "",
		},
		Tierings: []s3types.Tiering{
			{
				AccessTier: "ARCHIVE_ACCESS",
				Days:       90,
			},
			{
				AccessTier: "DEEP_ARCHIVE_ACCESS",
				Days:       180,
			},
		},
	}

	writeXML(w, http.StatusOK, response)
}

// PutBucketIntelligentTieringConfiguration sets intelligent tiering configuration
func (h *Handler) PutBucketIntelligentTieringConfiguration(w http.ResponseWriter, r *http.Request) {
	bucketName := chi.URLParam(r, "bucket")
	configID := r.URL.Query().Get("id")

	if configID == "" {
		writeS3Error(w, "InvalidArgument", "Configuration ID is required", http.StatusBadRequest)
		return
	}

	var config s3types.IntelligentTieringConfiguration
	if err := xml.NewDecoder(r.Body).Decode(&config); err != nil {
		writeS3Error(w, "MalformedXML", "Failed to parse configuration: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate configuration
	if config.Status != "Enabled" && config.Status != "Disabled" {
		writeS3Error(w, "InvalidArgument", "Status must be 'Enabled' or 'Disabled'", http.StatusBadRequest)
		return
	}

	// In a real implementation, this would save the configuration
	_ = bucketName

	w.WriteHeader(http.StatusOK)
}

// ListBucketIntelligentTieringConfigurations lists all intelligent tiering configurations
func (h *Handler) ListBucketIntelligentTieringConfigurations(w http.ResponseWriter, r *http.Request) {
	bucketName := chi.URLParam(r, "bucket")

	// In a real implementation, this would list configurations from storage
	response := s3types.ListBucketIntelligentTieringConfigurationsOutput{
		IsTruncated: false,
	}

	// Example configuration
	_ = bucketName

	writeXML(w, http.StatusOK, response)
}

// DeleteBucketIntelligentTieringConfiguration deletes an intelligent tiering configuration
func (h *Handler) DeleteBucketIntelligentTieringConfiguration(w http.ResponseWriter, r *http.Request) {
	bucketName := chi.URLParam(r, "bucket")
	configID := r.URL.Query().Get("id")

	if configID == "" {
		writeS3Error(w, "InvalidArgument", "Configuration ID is required", http.StatusBadRequest)
		return
	}

	// In a real implementation, this would delete the configuration
	_ = bucketName

	w.WriteHeader(http.StatusNoContent)
}
