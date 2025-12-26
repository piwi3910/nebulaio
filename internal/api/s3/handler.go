package s3

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/piwi3910/nebulaio/internal/auth"
	"github.com/piwi3910/nebulaio/internal/bucket"
	"github.com/piwi3910/nebulaio/internal/object"
	"github.com/piwi3910/nebulaio/pkg/s3types"
)

// Handler handles S3 API requests
type Handler struct {
	auth   *auth.Service
	bucket *bucket.Service
	object *object.Service
}

// NewHandler creates a new S3 API handler
func NewHandler(authService *auth.Service, bucketService *bucket.Service, objectService *object.Service) *Handler {
	return &Handler{
		auth:   authService,
		bucket: bucketService,
		object: objectService,
	}
}

// RegisterRoutes registers S3 API routes
func (h *Handler) RegisterRoutes(r chi.Router) {
	// Service operations
	r.Get("/", h.ListBuckets)

	// Bucket operations
	r.Route("/{bucket}", func(r chi.Router) {
		r.Put("/", h.CreateBucket)
		r.Delete("/", h.DeleteBucket)
		r.Head("/", h.HeadBucket)
		r.Get("/", h.handleBucketGet)

		// Object operations
		r.Route("/{key:.*}", func(r chi.Router) {
			r.Put("/", h.handleObjectPut)
			r.Get("/", h.GetObject)
			r.Delete("/", h.DeleteObject)
			r.Head("/", h.HeadObject)
			r.Post("/", h.handleObjectPost)
		})
	})
}

// ListBuckets lists all buckets
func (h *Handler) ListBuckets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// TODO: Implement proper auth
	owner := "anonymous"

	buckets, err := h.bucket.ListBuckets(ctx, "")
	if err != nil {
		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)
		return
	}

	response := s3types.ListAllMyBucketsResult{
		Owner: s3types.Owner{
			ID:          owner,
			DisplayName: owner,
		},
	}

	for _, b := range buckets {
		response.Buckets.Bucket = append(response.Buckets.Bucket, s3types.BucketInfo{
			Name:         b.Name,
			CreationDate: b.CreatedAt.Format(time.RFC3339),
		})
	}

	writeXML(w, http.StatusOK, response)
}

// CreateBucket creates a new bucket
func (h *Handler) CreateBucket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	// TODO: Implement proper auth
	owner := "anonymous"

	_, err := h.bucket.CreateBucket(ctx, bucketName, owner, "", "")
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeS3Error(w, "BucketAlreadyExists", err.Error(), http.StatusConflict)
			return
		}
		if strings.Contains(err.Error(), "bucket name") {
			writeS3Error(w, "InvalidBucketName", err.Error(), http.StatusBadRequest)
			return
		}
		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Location", "/"+bucketName)
	w.WriteHeader(http.StatusOK)
}

// DeleteBucket deletes a bucket
func (h *Handler) DeleteBucket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	err := h.bucket.DeleteBucket(ctx, bucketName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
			return
		}
		if strings.Contains(err.Error(), "not empty") {
			writeS3Error(w, "BucketNotEmpty", err.Error(), http.StatusConflict)
			return
		}
		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HeadBucket checks if a bucket exists
func (h *Handler) HeadBucket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	err := h.bucket.HeadBucket(ctx, bucketName)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handleBucketGet handles GET requests on buckets (list objects or bucket subresources)
func (h *Handler) handleBucketGet(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	// Check for bucket subresources
	if _, ok := query["versioning"]; ok {
		h.GetBucketVersioning(w, r)
		return
	}
	if _, ok := query["policy"]; ok {
		h.GetBucketPolicy(w, r)
		return
	}
	if _, ok := query["tagging"]; ok {
		h.GetBucketTagging(w, r)
		return
	}
	if _, ok := query["cors"]; ok {
		h.GetBucketCORS(w, r)
		return
	}
	if _, ok := query["lifecycle"]; ok {
		h.GetBucketLifecycle(w, r)
		return
	}
	if _, ok := query["uploads"]; ok {
		h.ListMultipartUploads(w, r)
		return
	}

	// Default: list objects
	h.ListObjectsV2(w, r)
}

// ListObjectsV2 lists objects in a bucket
func (h *Handler) ListObjectsV2(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	query := r.URL.Query()

	prefix := query.Get("prefix")
	delimiter := query.Get("delimiter")
	maxKeysStr := query.Get("max-keys")
	continuationToken := query.Get("continuation-token")

	maxKeys := 1000
	if maxKeysStr != "" {
		if mk, err := strconv.Atoi(maxKeysStr); err == nil && mk > 0 && mk <= 1000 {
			maxKeys = mk
		}
	}

	listing, err := h.object.ListObjects(ctx, bucketName, prefix, delimiter, maxKeys, continuationToken)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
			return
		}
		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)
		return
	}

	response := s3types.ListBucketResult{
		Name:                  bucketName,
		Prefix:                prefix,
		Delimiter:             delimiter,
		MaxKeys:               maxKeys,
		IsTruncated:           listing.IsTruncated,
		ContinuationToken:     continuationToken,
		NextContinuationToken: listing.NextContinuationToken,
		KeyCount:              len(listing.Objects),
	}

	for _, obj := range listing.Objects {
		response.Contents = append(response.Contents, s3types.ObjectInfo{
			Key:          obj.Key,
			LastModified: obj.ModifiedAt.Format(time.RFC3339),
			ETag:         obj.ETag,
			Size:         obj.Size,
			StorageClass: obj.StorageClass,
			Owner: &s3types.Owner{
				ID:          obj.Owner,
				DisplayName: obj.Owner,
			},
		})
	}

	for _, prefix := range listing.CommonPrefixes {
		response.CommonPrefixes = append(response.CommonPrefixes, s3types.CommonPrefix{
			Prefix: prefix,
		})
	}

	writeXML(w, http.StatusOK, response)
}

// handleObjectPut handles PUT requests on objects
func (h *Handler) handleObjectPut(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	// Check for multipart upload part
	if partNumberStr := query.Get("partNumber"); partNumberStr != "" {
		h.UploadPart(w, r)
		return
	}

	// Check for copy operation
	if copySource := r.Header.Get("x-amz-copy-source"); copySource != "" {
		h.CopyObject(w, r)
		return
	}

	// Default: put object
	h.PutObject(w, r)
}

// PutObject uploads an object
func (h *Handler) PutObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")

	// TODO: Implement proper auth
	owner := "anonymous"

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	contentLength := r.ContentLength

	// Parse user metadata
	userMetadata := make(map[string]string)
	for key, values := range r.Header {
		if strings.HasPrefix(strings.ToLower(key), "x-amz-meta-") {
			metaKey := strings.TrimPrefix(strings.ToLower(key), "x-amz-meta-")
			userMetadata[metaKey] = values[0]
		}
	}

	meta, err := h.object.PutObject(ctx, bucketName, key, r.Body, contentLength, contentType, owner, userMetadata)
	if err != nil {
		if strings.Contains(err.Error(), "bucket not found") {
			writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
			return
		}
		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("ETag", meta.ETag)
	if meta.VersionID != "" {
		w.Header().Set("x-amz-version-id", meta.VersionID)
	}
	w.WriteHeader(http.StatusOK)
}

// GetObject retrieves an object
func (h *Handler) GetObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")

	reader, meta, err := h.object.GetObject(ctx, bucketName, key)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchKey", err.Error(), http.StatusNotFound)
			return
		}
		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	// Set response headers
	w.Header().Set("Content-Type", meta.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(meta.Size, 10))
	w.Header().Set("ETag", meta.ETag)
	w.Header().Set("Last-Modified", meta.ModifiedAt.Format(http.TimeFormat))
	if meta.VersionID != "" {
		w.Header().Set("x-amz-version-id", meta.VersionID)
	}

	// Set user metadata
	for k, v := range meta.Metadata {
		w.Header().Set("x-amz-meta-"+k, v)
	}

	w.WriteHeader(http.StatusOK)
	io.Copy(w, reader)
}

// HeadObject retrieves object metadata
func (h *Handler) HeadObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")

	meta, err := h.object.HeadObject(ctx, bucketName, key)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", meta.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(meta.Size, 10))
	w.Header().Set("ETag", meta.ETag)
	w.Header().Set("Last-Modified", meta.ModifiedAt.Format(http.TimeFormat))
	if meta.VersionID != "" {
		w.Header().Set("x-amz-version-id", meta.VersionID)
	}

	for k, v := range meta.Metadata {
		w.Header().Set("x-amz-meta-"+k, v)
	}

	w.WriteHeader(http.StatusOK)
}

// DeleteObject deletes an object
func (h *Handler) DeleteObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")

	err := h.object.DeleteObject(ctx, bucketName, key)
	if err != nil {
		// S3 returns 204 even if object doesn't exist
		if !strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// CopyObject copies an object
func (h *Handler) CopyObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dstBucket := chi.URLParam(r, "bucket")
	dstKey := chi.URLParam(r, "key")

	// TODO: Implement proper auth
	owner := "anonymous"

	// Parse copy source
	copySource := r.Header.Get("x-amz-copy-source")
	copySource = strings.TrimPrefix(copySource, "/")
	parts := strings.SplitN(copySource, "/", 2)
	if len(parts) != 2 {
		writeS3Error(w, "InvalidArgument", "Invalid copy source", http.StatusBadRequest)
		return
	}
	srcBucket, srcKey := parts[0], parts[1]

	meta, err := h.object.CopyObject(ctx, srcBucket, srcKey, dstBucket, dstKey, owner)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchKey", err.Error(), http.StatusNotFound)
			return
		}
		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)
		return
	}

	response := s3types.CopyObjectResult{
		ETag:         meta.ETag,
		LastModified: meta.ModifiedAt.Format(time.RFC3339),
	}

	writeXML(w, http.StatusOK, response)
}

// handleObjectPost handles POST requests on objects (multipart operations)
func (h *Handler) handleObjectPost(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	if _, ok := query["uploads"]; ok {
		h.CreateMultipartUpload(w, r)
		return
	}

	if uploadID := query.Get("uploadId"); uploadID != "" {
		h.CompleteMultipartUpload(w, r)
		return
	}

	writeS3Error(w, "InvalidRequest", "Invalid POST request", http.StatusBadRequest)
}

// CreateMultipartUpload initiates a multipart upload
func (h *Handler) CreateMultipartUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")

	// TODO: Implement proper auth
	owner := "anonymous"

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	upload, err := h.object.CreateMultipartUpload(ctx, bucketName, key, contentType, owner)
	if err != nil {
		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)
		return
	}

	response := s3types.InitiateMultipartUploadResult{
		Bucket:   bucketName,
		Key:      key,
		UploadId: upload.UploadID,
	}

	writeXML(w, http.StatusOK, response)
}

// UploadPart uploads a part of a multipart upload
func (h *Handler) UploadPart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")
	query := r.URL.Query()

	uploadID := query.Get("uploadId")
	partNumberStr := query.Get("partNumber")

	partNumber, err := strconv.Atoi(partNumberStr)
	if err != nil || partNumber < 1 || partNumber > 10000 {
		writeS3Error(w, "InvalidArgument", "Invalid part number", http.StatusBadRequest)
		return
	}

	part, err := h.object.UploadPart(ctx, bucketName, key, uploadID, partNumber, r.Body, r.ContentLength)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchUpload", err.Error(), http.StatusNotFound)
			return
		}
		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("ETag", part.ETag)
	w.WriteHeader(http.StatusOK)
}

// CompleteMultipartUpload completes a multipart upload
func (h *Handler) CompleteMultipartUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")
	uploadID := r.URL.Query().Get("uploadId")

	// Parse request body
	var req s3types.CompleteMultipartUploadRequest
	if err := xml.NewDecoder(r.Body).Decode(&req); err != nil {
		writeS3Error(w, "MalformedXML", err.Error(), http.StatusBadRequest)
		return
	}

	var parts []int
	for _, part := range req.Part {
		parts = append(parts, part.PartNumber)
	}

	meta, err := h.object.CompleteMultipartUpload(ctx, bucketName, key, uploadID, parts)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchUpload", err.Error(), http.StatusNotFound)
			return
		}
		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)
		return
	}

	response := s3types.CompleteMultipartUploadResult{
		Location: fmt.Sprintf("/%s/%s", bucketName, key),
		Bucket:   bucketName,
		Key:      key,
		ETag:     meta.ETag,
	}

	writeXML(w, http.StatusOK, response)
}

// ListMultipartUploads lists in-progress multipart uploads
func (h *Handler) ListMultipartUploads(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	uploads, err := h.object.ListMultipartUploads(ctx, bucketName)
	if err != nil {
		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)
		return
	}

	response := s3types.ListMultipartUploadsResult{
		Bucket: bucketName,
	}

	for _, upload := range uploads {
		response.Upload = append(response.Upload, s3types.MultipartUploadInfo{
			Key:       upload.Key,
			UploadId:  upload.UploadID,
			Initiator: &s3types.Owner{ID: upload.Initiator, DisplayName: upload.Initiator},
			Owner:     &s3types.Owner{ID: upload.Initiator, DisplayName: upload.Initiator},
			Initiated: upload.CreatedAt.Format(time.RFC3339),
		})
	}

	writeXML(w, http.StatusOK, response)
}

// Bucket subresource handlers

func (h *Handler) GetBucketVersioning(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	status, err := h.bucket.GetVersioning(ctx, bucketName)
	if err != nil {
		writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
		return
	}

	response := s3types.VersioningConfiguration{
		Status: string(status),
	}

	writeXML(w, http.StatusOK, response)
}

func (h *Handler) GetBucketPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	policy, err := h.bucket.GetBucketPolicy(ctx, bucketName)
	if err != nil {
		if strings.Contains(err.Error(), "no bucket policy") {
			writeS3Error(w, "NoSuchBucketPolicy", err.Error(), http.StatusNotFound)
			return
		}
		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(policy))
}

func (h *Handler) GetBucketTagging(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	tags, err := h.bucket.GetBucketTags(ctx, bucketName)
	if err != nil {
		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)
		return
	}

	if len(tags) == 0 {
		writeS3Error(w, "NoSuchTagSet", "No tags found", http.StatusNotFound)
		return
	}

	response := s3types.Tagging{}
	for k, v := range tags {
		response.TagSet.Tag = append(response.TagSet.Tag, s3types.Tag{Key: k, Value: v})
	}

	writeXML(w, http.StatusOK, response)
}

func (h *Handler) GetBucketCORS(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	rules, err := h.bucket.GetCORS(ctx, bucketName)
	if err != nil {
		if strings.Contains(err.Error(), "no CORS") {
			writeS3Error(w, "NoSuchCORSConfiguration", err.Error(), http.StatusNotFound)
			return
		}
		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)
		return
	}

	response := s3types.CORSConfiguration{}
	for _, rule := range rules {
		response.CORSRule = append(response.CORSRule, s3types.CORSRule{
			AllowedOrigin: rule.AllowedOrigins,
			AllowedMethod: rule.AllowedMethods,
			AllowedHeader: rule.AllowedHeaders,
			ExposeHeader:  rule.ExposeHeaders,
			MaxAgeSeconds: rule.MaxAgeSeconds,
		})
	}

	writeXML(w, http.StatusOK, response)
}

func (h *Handler) GetBucketLifecycle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	rules, err := h.bucket.GetLifecycle(ctx, bucketName)
	if err != nil {
		if strings.Contains(err.Error(), "no lifecycle") {
			writeS3Error(w, "NoSuchLifecycleConfiguration", err.Error(), http.StatusNotFound)
			return
		}
		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)
		return
	}

	response := s3types.LifecycleConfiguration{}
	for _, rule := range rules {
		status := "Disabled"
		if rule.Enabled {
			status = "Enabled"
		}
		response.Rule = append(response.Rule, s3types.LifecycleRule{
			ID:     rule.ID,
			Status: status,
			Prefix: rule.Prefix,
		})
	}

	writeXML(w, http.StatusOK, response)
}

// Helper functions

func writeXML(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	w.Write([]byte(xml.Header))
	xml.NewEncoder(w).Encode(v)
}

func writeS3Error(w http.ResponseWriter, code, message string, status int) {
	response := s3types.ErrorResponse{
		Code:    code,
		Message: message,
	}
	writeXML(w, status, response)
}
