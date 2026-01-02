package s3

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/piwi3910/nebulaio/internal/api/middleware"
	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/internal/s3select"
	"github.com/piwi3910/nebulaio/pkg/s3types"
)

// Format type constants for S3 Select.
const (
	formatTypeJSON = "JSON"
)

// GetObjectAttributes returns object metadata without the object body
// This is a lighter-weight alternative to HeadObject when you only need specific attributes.
func (h *Handler) GetObjectAttributes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")
	versionID := r.URL.Query().Get("versionId")

	requestedAttrs := parseObjectAttributesHeader(r.Header.Get("X-Amz-Object-Attributes"))
	if len(requestedAttrs) == 0 {
		writeS3Error(w, "InvalidArgument", "x-amz-object-attributes header is required", http.StatusBadRequest)
		return
	}

	meta, err := h.getObjectMetadataForAttributes(ctx, bucketName, key, versionID)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, key)
		return
	}

	response := h.buildObjectAttributesResponse(requestedAttrs, meta)
	h.setObjectAttributesHeaders(w, meta, versionID)

	writeXML(w, http.StatusOK, response)
}

func (h *Handler) getObjectMetadataForAttributes(
	ctx context.Context,
	bucketName, key, versionID string,
) (*metadata.ObjectMeta, error) {
	if versionID != "" {
		return h.object.HeadObjectVersion(ctx, bucketName, key, versionID)
	}
	return h.object.HeadObject(ctx, bucketName, key)
}

func (h *Handler) buildObjectAttributesResponse(
	requestedAttrs []string,
	meta *metadata.ObjectMeta,
) s3types.GetObjectAttributesOutput {
	response := s3types.GetObjectAttributesOutput{}

	for _, attr := range requestedAttrs {
		h.setAttributeInResponse(&response, attr, meta)
	}

	return response
}

func (h *Handler) setAttributeInResponse(
	response *s3types.GetObjectAttributesOutput,
	attr string,
	meta *metadata.ObjectMeta,
) {
	switch attr {
	case "ETag":
		response.ETag = meta.ETag
	case "Checksum":
		response.Checksum = h.extractChecksumFromMetadata(meta)
	case "ObjectParts":
		response.ObjectParts = h.extractObjectPartsFromMetadata(meta)
	case "StorageClass":
		response.StorageClass = h.getStorageClassOrDefault(meta)
	case "ObjectSize":
		response.ObjectSize = meta.Size
	}
}

func (h *Handler) extractChecksumFromMetadata(meta *metadata.ObjectMeta) *s3types.Checksum {
	if meta.Metadata == nil {
		return nil
	}

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

	if !hasChecksum {
		return nil
	}

	return checksum
}

func (h *Handler) extractObjectPartsFromMetadata(meta *metadata.ObjectMeta) *s3types.ObjectParts {
	if meta.Metadata == nil {
		return nil
	}

	partsStr, ok := meta.Metadata["x-amz-mp-parts-count"]
	if !ok {
		return nil
	}

	partsCount, err := strconv.Atoi(partsStr)
	if err != nil || partsCount <= 0 {
		return nil
	}

	return &s3types.ObjectParts{
		PartsCount:      partsCount,
		TotalPartsCount: partsCount,
		IsTruncated:     false,
		MaxParts:        1000,
	}
}

func (h *Handler) getStorageClassOrDefault(meta *metadata.ObjectMeta) string {
	if meta.StorageClass == "" {
		return "STANDARD"
	}
	return meta.StorageClass
}

func (h *Handler) setObjectAttributesHeaders(
	w http.ResponseWriter,
	meta *metadata.ObjectMeta,
	versionID string,
) {
	w.Header().Set("Last-Modified", meta.ModifiedAt.Format(http.TimeFormat))

	if versionID != "" {
		w.Header().Set("X-Amz-Version-Id", versionID)
	}

	if meta.DeleteMarker {
		w.Header().Set("X-Amz-Delete-Marker", "true")
	}
}

// parseObjectAttributesHeader parses the x-amz-object-attributes header.
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
// Supports full SQL SELECT with projections, WHERE filters, aggregates (COUNT, SUM, AVG, MIN, MAX).
func (h *Handler) SelectObjectContent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")

	// Parse request body
	var selectReq s3types.SelectObjectContentInput
	err := xml.NewDecoder(r.Body).Decode(&selectReq)
	if err != nil {
		writeS3Error(w, "InvalidRequest", "Failed to parse request: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate input format
	if selectReq.InputSerialization.CSV == nil && selectReq.InputSerialization.JSON == nil && selectReq.InputSerialization.Parquet == nil {
		writeS3Error(w, "InvalidRequest", "InputSerialization must specify CSV, JSON, or Parquet format", http.StatusBadRequest)
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

	// Build input format configuration
	inputFormat := buildInputFormat(selectReq.InputSerialization)

	// Build output format configuration
	outputFormat := buildOutputFormat(selectReq.OutputSerialization)

	// Create S3 Select engine and execute query
	engine := s3select.NewEngine(inputFormat, outputFormat)

	result, err := engine.Execute(content, selectReq.Expression)
	if err != nil {
		writeS3Error(w, "InvalidRequest", "Query execution failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Write response as event stream
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Amz-Request-Id", middleware.GetRequestID(ctx))
	w.WriteHeader(http.StatusOK)

	// Write records message
	recordsMsg := createSelectEventMessage("Records", result.Records)
	_, _ = w.Write(recordsMsg)

	// Write stats message
	statsMsg := createSelectEventMessage("Stats", fmt.Appendf(nil,
		`<Stats><BytesScanned>%d</BytesScanned><BytesProcessed>%d</BytesProcessed><BytesReturned>%d</BytesReturned></Stats>`,
		result.BytesScanned, result.BytesProcessed, result.BytesReturned,
	))
	_, _ = w.Write(statsMsg)

	// Write end message
	endMsg := createSelectEventMessage("End", nil)
	_, _ = w.Write(endMsg)
}

// buildInputFormat converts S3 InputSerialization to s3select.InputFormat.
func buildInputFormat(input s3types.InputSerialization) s3select.InputFormat {
	format := s3select.InputFormat{}

	switch {
	case input.CSV != nil:
		format.Type = "CSV"

		format.CSVConfig = &s3select.CSVConfig{
			FileHeaderInfo:  input.CSV.FileHeaderInfo,
			Comments:        input.CSV.Comments,
			QuoteCharacter:  input.CSV.QuoteCharacter,
			FieldDelimiter:  input.CSV.FieldDelimiter,
			RecordDelimiter: input.CSV.RecordDelimiter,
		}
		if format.CSVConfig.FileHeaderInfo == "" {
			format.CSVConfig.FileHeaderInfo = "USE"
		}

		if format.CSVConfig.FieldDelimiter == "" {
			format.CSVConfig.FieldDelimiter = ","
		}

		if format.CSVConfig.RecordDelimiter == "" {
			format.CSVConfig.RecordDelimiter = "\n"
		}
	case input.JSON != nil:
		format.Type = formatTypeJSON

		format.JSONConfig = &s3select.JSONConfig{
			Type: input.JSON.Type,
		}
		if format.JSONConfig.Type == "" {
			format.JSONConfig.Type = "LINES"
		}
	case input.Parquet != nil:
		format.Type = "Parquet"
		format.ParquetConfig = &s3select.ParquetConfig{}
	}

	if input.CompressionType != "" {
		format.CompressionType = input.CompressionType
	}

	return format
}

// buildOutputFormat converts S3 OutputSerialization to s3select.OutputFormat.
func buildOutputFormat(output s3types.OutputSerialization) s3select.OutputFormat {
	format := s3select.OutputFormat{}

	switch {
	case output.CSV != nil:
		format.Type = "CSV"

		format.CSVConfig = &s3select.CSVOutputConfig{
			QuoteFields:     output.CSV.QuoteFields,
			FieldDelimiter:  output.CSV.FieldDelimiter,
			RecordDelimiter: output.CSV.RecordDelimiter,
			QuoteCharacter:  output.CSV.QuoteCharacter,
		}
		if format.CSVConfig.FieldDelimiter == "" {
			format.CSVConfig.FieldDelimiter = ","
		}

		if format.CSVConfig.RecordDelimiter == "" {
			format.CSVConfig.RecordDelimiter = "\n"
		}

		if format.CSVConfig.QuoteFields == "" {
			format.CSVConfig.QuoteFields = "ASNEEDED"
		}
	case output.JSON != nil:
		format.Type = formatTypeJSON

		format.JSONConfig = &s3select.JSONOutputConfig{
			RecordDelimiter: output.JSON.RecordDelimiter,
		}
		if format.JSONConfig.RecordDelimiter == "" {
			format.JSONConfig.RecordDelimiter = "\n"
		}
	default:
		// Default to JSON output
		format.Type = formatTypeJSON
		format.JSONConfig = &s3select.JSONOutputConfig{
			RecordDelimiter: "\n",
		}
	}

	return format
}

// createSelectEventMessage creates an S3 Select event stream message.
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

// RestoreObject initiates restore of an object from archive storage.
func (h *Handler) RestoreObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")
	versionID := r.URL.Query().Get("versionId")

	restoreReq, err := h.parseRestoreRequest(r)
	if err != nil {
		writeS3Error(w, "InvalidRequest", err.Error(), http.StatusBadRequest)
		return
	}

	meta, err := h.getObjectMetaForRestore(ctx, bucketName, key, versionID)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, key)
		return
	}

	if err := h.validateStorageClassForRestore(meta); err != nil {
		writeS3Error(w, "InvalidObjectState", err.Error(), http.StatusForbidden)
		return
	}

	if handled := h.handleExistingRestore(w, meta); handled {
		return
	}

	h.initiateRestore(w, restoreReq)
}

func (h *Handler) parseRestoreRequest(r *http.Request) (*s3types.RestoreRequest, error) {
	var restoreReq s3types.RestoreRequest
	err := xml.NewDecoder(r.Body).Decode(&restoreReq)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("Failed to parse restore request: %w", err)
	}

	if restoreReq.Days == 0 {
		restoreReq.Days = 1
	}

	if restoreReq.Days < 1 || restoreReq.Days > 365 {
		return nil, errors.New("Days must be between 1 and 365")
	}

	return &restoreReq, nil
}

func (h *Handler) getObjectMetaForRestore(
	ctx context.Context,
	bucketName, key, versionID string,
) (*metadata.ObjectMeta, error) {
	if versionID != "" {
		return h.object.HeadObjectVersion(ctx, bucketName, key, versionID)
	}
	return h.object.HeadObject(ctx, bucketName, key)
}

func (h *Handler) validateStorageClassForRestore(meta *metadata.ObjectMeta) error {
	if meta.StorageClass != "GLACIER" && meta.StorageClass != "DEEP_ARCHIVE" {
		return errors.New("Object storage class is not GLACIER or DEEP_ARCHIVE")
	}
	return nil
}

func (h *Handler) handleExistingRestore(w http.ResponseWriter, meta *metadata.ObjectMeta) bool {
	if meta.Metadata == nil {
		return false
	}

	restoreStatus, ok := meta.Metadata["x-amz-restore-status"]
	if !ok {
		return false
	}

	if restoreStatus == "ongoing" {
		w.Header().Set("X-Amz-Restore", `ongoing-request="true"`)
		w.WriteHeader(http.StatusConflict)
		return true
	}

	if restoreStatus == "completed" {
		return h.handleCompletedRestore(w, meta)
	}

	return false
}

func (h *Handler) handleCompletedRestore(w http.ResponseWriter, meta *metadata.ObjectMeta) bool {
	expiryStr, ok := meta.Metadata["x-amz-restore-expiry"]
	if !ok {
		return false
	}

	expiryTime, err := time.Parse(time.RFC3339, expiryStr)
	if err != nil || !expiryTime.After(time.Now()) {
		return false
	}

	w.Header().Set("X-Amz-Restore", fmt.Sprintf(`ongoing-request="false", expiry-date="%s"`,
		expiryTime.Format(time.RFC1123)))
	w.WriteHeader(http.StatusOK)
	return true
}

func (h *Handler) initiateRestore(w http.ResponseWriter, restoreReq *s3types.RestoreRequest) {
	tier := restoreReq.GlacierJobParameters.Tier
	if tier == "" {
		tier = "Standard"
	}

	expiryDate := time.Now().Add(time.Duration(restoreReq.Days) * 24 * time.Hour)

	w.Header().Set("X-Amz-Restore", `ongoing-request="true"`)
	w.Header().Set("X-Amz-Restore-Expiry-Date", expiryDate.Format(time.RFC1123))
	w.Header().Set("X-Amz-Restore-Tier", tier)
	w.WriteHeader(http.StatusAccepted)
}

// WriteGetObjectResponse writes a response on behalf of a Lambda function
// This is used for S3 Object Lambda.
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

	if sc := r.Header.Get("X-Amz-Fwd-Status"); sc != "" {
		parsed, err := strconv.Atoi(sc)
		if err == nil {
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
	if contentType := r.Header.Get("X-Amz-Fwd-Header-Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}

	if contentLength := r.Header.Get("X-Amz-Fwd-Header-Content-Length"); contentLength != "" {
		w.Header().Set("Content-Length", contentLength)
	}

	if etag := r.Header.Get("X-Amz-Fwd-Header-Etag"); etag != "" {
		w.Header().Set("ETag", etag)
	}

	if lastModified := r.Header.Get("X-Amz-Fwd-Header-Last-Modified"); lastModified != "" {
		w.Header().Set("Last-Modified", lastModified)
	}

	// Write response
	w.WriteHeader(statusCode)
	_, _ = w.Write(body)
}

// GetBucketIntelligentTieringConfiguration gets intelligent tiering configuration.
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

// PutBucketIntelligentTieringConfiguration sets intelligent tiering configuration.
func (h *Handler) PutBucketIntelligentTieringConfiguration(w http.ResponseWriter, r *http.Request) {
	bucketName := chi.URLParam(r, "bucket")
	configID := r.URL.Query().Get("id")

	if configID == "" {
		writeS3Error(w, "InvalidArgument", "Configuration ID is required", http.StatusBadRequest)
		return
	}

	var config s3types.IntelligentTieringConfiguration

	err := xml.NewDecoder(r.Body).Decode(&config)
	if err != nil {
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

// ListBucketIntelligentTieringConfigurations lists all intelligent tiering configurations.
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

// DeleteBucketIntelligentTieringConfiguration deletes an intelligent tiering configuration.
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
