// Package s3 implements an Amazon S3-compatible API for NebulaIO.
//
// The package provides HTTP handlers for S3 operations including:
//   - Bucket operations: CreateBucket, DeleteBucket, ListBuckets, HeadBucket
//   - Object operations: PutObject, GetObject, DeleteObject, CopyObject, HeadObject
//   - Multipart uploads: CreateMultipartUpload, UploadPart, CompleteMultipartUpload
//   - Object listing: ListObjects, ListObjectsV2
//   - Access control: PutBucketPolicy, GetBucketPolicy, PutObjectAcl
//
// Authentication is handled via AWS Signature V4, supporting both header-based
// and query-string (presigned URL) authentication methods.
//
// Example usage:
//
//	handler := s3.NewHandler(authSvc, bucketSvc, objectSvc)
//	router.Mount("/", handler.Routes())
package s3

import (
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
	"github.com/piwi3910/nebulaio/internal/auth"
	"github.com/piwi3910/nebulaio/internal/bucket"
	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/internal/object"
	"github.com/piwi3910/nebulaio/internal/policy"
	"github.com/piwi3910/nebulaio/pkg/s3errors"
	"github.com/piwi3910/nebulaio/pkg/s3types"
)

// Status constants for S3 features.
const (
	statusEnabled = "Enabled"
)

// Handler handles S3 API requests.
type Handler struct {
	auth   *auth.Service
	bucket *bucket.Service
	object *object.Service
}

// NewHandler creates a new S3 API handler.
func NewHandler(authService *auth.Service, bucketService *bucket.Service, objectService *object.Service) *Handler {
	return &Handler{
		auth:   authService,
		bucket: bucketService,
		object: objectService,
	}
}

// RegisterRoutes registers S3 API routes.
func (h *Handler) RegisterRoutes(r chi.Router) {
	// Service operations
	r.Get("/", h.ListBuckets)

	// Bucket operations
	r.Route("/{bucket}", func(r chi.Router) {
		r.Put("/", h.handleBucketPut)
		r.Delete("/", h.handleBucketDelete)
		r.Head("/", h.HeadBucket)
		r.Get("/", h.handleBucketGet)
		r.Post("/", h.handleBucketPost)

		// Object operations
		r.Route("/{key:.*}", func(r chi.Router) {
			r.Put("/", h.handleObjectPut)
			r.Get("/", h.handleObjectGet)
			r.Delete("/", h.handleObjectDelete)
			r.Head("/", h.HeadObject)
			r.Post("/", h.handleObjectPost)
		})
	})
}

// ListBuckets lists all buckets.
func (h *Handler) ListBuckets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get owner from authenticated context
	owner := middleware.GetOwnerID(ctx)
	ownerDisplayName := middleware.GetOwnerDisplayName(ctx)

	buckets, err := h.bucket.ListBuckets(ctx, owner)
	if err != nil {
		writeS3ErrorTyped(w, r, err)
		return
	}

	response := s3types.ListAllMyBucketsResult{
		Owner: s3types.Owner{
			ID:          owner,
			DisplayName: ownerDisplayName,
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

// CreateBucket creates a new bucket.
func (h *Handler) CreateBucket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	// Get owner from authenticated context
	owner := middleware.GetOwnerID(ctx)

	_, err := h.bucket.CreateBucket(ctx, bucketName, owner, "", "")
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	w.Header().Set("Location", "/"+bucketName)
	w.WriteHeader(http.StatusOK)
}

// DeleteBucket deletes a bucket.
func (h *Handler) DeleteBucket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	err := h.bucket.DeleteBucket(ctx, bucketName)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HeadBucket checks if a bucket exists.
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

// handleBucketPut handles PUT requests on buckets (create bucket or bucket subresources).
func (h *Handler) handleBucketPut(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	// Check for bucket subresources
	if _, ok := query["versioning"]; ok {
		h.PutBucketVersioning(w, r)
		return
	}

	if _, ok := query["policy"]; ok {
		h.PutBucketPolicy(w, r)
		return
	}

	if _, ok := query["tagging"]; ok {
		h.PutBucketTagging(w, r)
		return
	}

	if _, ok := query["cors"]; ok {
		h.PutBucketCORS(w, r)
		return
	}

	if _, ok := query["lifecycle"]; ok {
		h.PutBucketLifecycle(w, r)
		return
	}

	if _, ok := query["acl"]; ok {
		h.PutBucketAcl(w, r)
		return
	}

	if _, ok := query["encryption"]; ok {
		h.PutBucketEncryption(w, r)
		return
	}

	if _, ok := query["website"]; ok {
		h.PutBucketWebsite(w, r)
		return
	}

	if _, ok := query["logging"]; ok {
		h.PutBucketLogging(w, r)
		return
	}

	if _, ok := query["notification"]; ok {
		h.PutBucketNotificationConfiguration(w, r)
		return
	}

	if _, ok := query["replication"]; ok {
		h.PutBucketReplication(w, r)
		return
	}

	if _, ok := query["object-lock"]; ok {
		h.PutObjectLockConfiguration(w, r)
		return
	}

	if _, ok := query["publicAccessBlock"]; ok {
		h.PutPublicAccessBlock(w, r)
		return
	}

	if _, ok := query["ownershipControls"]; ok {
		h.PutBucketOwnershipControls(w, r)
		return
	}

	if _, ok := query["accelerate"]; ok {
		h.PutBucketAccelerateConfiguration(w, r)
		return
	}

	if _, ok := query["intelligent-tiering"]; ok {
		h.PutBucketIntelligentTieringConfiguration(w, r)
		return
	}

	// Default: create bucket
	h.CreateBucket(w, r)
}

// handleBucketDelete handles DELETE requests on buckets (delete bucket or bucket subresources).
func (h *Handler) handleBucketDelete(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	// Check for bucket subresources
	if _, ok := query["policy"]; ok {
		h.DeleteBucketPolicy(w, r)
		return
	}

	if _, ok := query["tagging"]; ok {
		h.DeleteBucketTagging(w, r)
		return
	}

	if _, ok := query["cors"]; ok {
		h.DeleteBucketCORS(w, r)
		return
	}

	if _, ok := query["lifecycle"]; ok {
		h.DeleteBucketLifecycle(w, r)
		return
	}

	if _, ok := query["encryption"]; ok {
		h.DeleteBucketEncryption(w, r)
		return
	}

	if _, ok := query["website"]; ok {
		h.DeleteBucketWebsite(w, r)
		return
	}

	if _, ok := query["replication"]; ok {
		h.DeleteBucketReplication(w, r)
		return
	}

	if _, ok := query["publicAccessBlock"]; ok {
		h.DeletePublicAccessBlock(w, r)
		return
	}

	if _, ok := query["ownershipControls"]; ok {
		h.DeleteBucketOwnershipControls(w, r)
		return
	}

	if _, ok := query["intelligent-tiering"]; ok {
		h.DeleteBucketIntelligentTieringConfiguration(w, r)
		return
	}

	// Default: delete bucket
	h.DeleteBucket(w, r)
}

// handleBucketGet handles GET requests on buckets (list objects or bucket subresources).
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

	if _, ok := query["versions"]; ok {
		h.ListObjectVersions(w, r)
		return
	}

	if _, ok := query["location"]; ok {
		h.GetBucketLocation(w, r)
		return
	}

	if _, ok := query["acl"]; ok {
		h.GetBucketAcl(w, r)
		return
	}

	if _, ok := query["encryption"]; ok {
		h.GetBucketEncryption(w, r)
		return
	}

	if _, ok := query["website"]; ok {
		h.GetBucketWebsite(w, r)
		return
	}

	if _, ok := query["logging"]; ok {
		h.GetBucketLogging(w, r)
		return
	}

	if _, ok := query["notification"]; ok {
		h.GetBucketNotificationConfiguration(w, r)
		return
	}

	if _, ok := query["replication"]; ok {
		h.GetBucketReplication(w, r)
		return
	}

	if _, ok := query["object-lock"]; ok {
		h.GetObjectLockConfiguration(w, r)
		return
	}

	if _, ok := query["publicAccessBlock"]; ok {
		h.GetPublicAccessBlock(w, r)
		return
	}

	if _, ok := query["ownershipControls"]; ok {
		h.GetBucketOwnershipControls(w, r)
		return
	}

	if _, ok := query["accelerate"]; ok {
		h.GetBucketAccelerateConfiguration(w, r)
		return
	}

	if _, ok := query["intelligent-tiering"]; ok {
		// Check if id parameter is present for single config
		if id := query.Get("id"); id != "" {
			h.GetBucketIntelligentTieringConfiguration(w, r)
		} else {
			h.ListBucketIntelligentTieringConfigurations(w, r)
		}

		return
	}

	// Default: list objects
	h.ListObjectsV2(w, r)
}

// handleBucketPost handles POST requests on buckets (e.g., ?delete for batch delete).
func (h *Handler) handleBucketPost(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	// Check for delete operation (batch delete)
	if _, ok := query["delete"]; ok {
		h.DeleteObjects(w, r)
		return
	}

	// No other bucket-level POST operations are currently supported
	writeS3Error(w, "InvalidRequest", "Invalid POST request on bucket", http.StatusBadRequest)
}

// ListObjectsV2 lists objects in a bucket.
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

// handleObjectPut handles PUT requests on objects.
func (h *Handler) handleObjectPut(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	// Check for object tagging
	if _, ok := query["tagging"]; ok {
		h.PutObjectTagging(w, r)
		return
	}

	// Check for object ACL
	if _, ok := query["acl"]; ok {
		h.PutObjectAcl(w, r)
		return
	}

	// Check for object retention
	if _, ok := query["retention"]; ok {
		h.PutObjectRetention(w, r)
		return
	}

	// Check for object legal-hold
	if _, ok := query["legal-hold"]; ok {
		h.PutObjectLegalHold(w, r)
		return
	}

	// Check for multipart upload part
	if partNumberStr := query.Get("partNumber"); partNumberStr != "" {
		h.UploadPart(w, r)
		return
	}

	// Check for copy operation
	if copySource := r.Header.Get("X-Amz-Copy-Source"); copySource != "" {
		h.CopyObject(w, r)
		return
	}

	// Default: put object
	h.PutObject(w, r)
}

// handleObjectGet handles GET requests on objects (get object or object subresources).
func (h *Handler) handleObjectGet(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	// Check for object attributes (GetObjectAttributes)
	if _, ok := query["attributes"]; ok {
		h.GetObjectAttributes(w, r)
		return
	}

	// Check for object tagging
	if _, ok := query["tagging"]; ok {
		h.GetObjectTagging(w, r)
		return
	}

	// Check for object ACL
	if _, ok := query["acl"]; ok {
		h.GetObjectAcl(w, r)
		return
	}

	// Check for object retention
	if _, ok := query["retention"]; ok {
		h.GetObjectRetention(w, r)
		return
	}

	// Check for object legal-hold
	if _, ok := query["legal-hold"]; ok {
		h.GetObjectLegalHold(w, r)
		return
	}

	// Check for version ID
	if versionID := query.Get("versionId"); versionID != "" {
		h.GetObjectVersion(w, r)
		return
	}

	// Default: get object
	h.GetObject(w, r)
}

// handleObjectDelete handles DELETE requests on objects (delete object or object subresources).
func (h *Handler) handleObjectDelete(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	// Check for object tagging
	if _, ok := query["tagging"]; ok {
		h.DeleteObjectTagging(w, r)
		return
	}

	// Check for version ID - handles both deletion of specific version and regular delete
	// The DeleteObject handler will check for versionId query param
	h.DeleteObject(w, r)
}

// PutObject uploads an object.
func (h *Handler) PutObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")

	// Get owner from authenticated context
	owner := middleware.GetOwnerID(ctx)

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

	// Parse x-amz-tagging header if present
	var opts *object.PutObjectOptions
	if taggingHeader := r.Header.Get("X-Amz-Tagging"); taggingHeader != "" {
		tags, err := object.ParseTaggingHeader(taggingHeader)
		if err != nil {
			writeS3Error(w, "InvalidArgument", err.Error(), http.StatusBadRequest)
			return
		}

		opts = &object.PutObjectOptions{Tags: tags}
	}

	meta, err := h.object.PutObjectWithOptions(ctx, bucketName, key, r.Body, contentLength, contentType, owner, userMetadata, opts)
	if err != nil {
		if strings.Contains(err.Error(), "bucket not found") {
			writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
			return
		}

		if strings.Contains(err.Error(), "invalid tags") {
			writeS3Error(w, "InvalidTag", err.Error(), http.StatusBadRequest)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	w.Header().Set("ETag", meta.ETag)

	if meta.VersionID != "" {
		w.Header().Set("X-Amz-Version-Id", meta.VersionID)
	}

	w.WriteHeader(http.StatusOK)
}

// GetObject retrieves an object or lists parts for a multipart upload.
func (h *Handler) GetObject(w http.ResponseWriter, r *http.Request) {
	// Check for ListParts
	if uploadID := r.URL.Query().Get("uploadId"); uploadID != "" {
		h.ListParts(w, r)
		return
	}

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

	defer func() { _ = reader.Close() }()

	// Set response headers
	w.Header().Set("Content-Type", meta.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(meta.Size, 10))
	w.Header().Set("ETag", meta.ETag)
	w.Header().Set("Last-Modified", meta.ModifiedAt.Format(http.TimeFormat))

	if meta.VersionID != "" {
		w.Header().Set("X-Amz-Version-Id", meta.VersionID)
	}

	// Set user metadata
	for k, v := range meta.Metadata {
		w.Header().Set("x-amz-meta-"+k, v)
	}

	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, reader)
}

// ListParts lists the uploaded parts for a multipart upload.
func (h *Handler) ListParts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")
	query := r.URL.Query()

	uploadID := query.Get("uploadId")

	// Parse pagination parameters
	maxParts := 1000

	if maxPartsStr := query.Get("max-parts"); maxPartsStr != "" {
		mp, err := strconv.Atoi(maxPartsStr)
		if err == nil && mp > 0 && mp <= 1000 {
			maxParts = mp
		}
	}

	partNumberMarker := 0

	if markerStr := query.Get("part-number-marker"); markerStr != "" {
		pm, err := strconv.Atoi(markerStr)
		if err == nil && pm >= 0 {
			partNumberMarker = pm
		}
	}

	result, err := h.object.ListParts(ctx, bucketName, key, uploadID, maxParts, partNumberMarker)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchUpload", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	response := s3types.ListPartsResult{
		Bucket:               bucketName,
		Key:                  key,
		UploadId:             uploadID,
		Initiator:            &s3types.Owner{ID: result.Initiator, DisplayName: result.Initiator},
		Owner:                &s3types.Owner{ID: result.Initiator, DisplayName: result.Initiator},
		PartNumberMarker:     partNumberMarker,
		NextPartNumberMarker: result.NextPartNumberMarker,
		MaxParts:             maxParts,
		IsTruncated:          result.IsTruncated,
	}

	for _, part := range result.Parts {
		response.Part = append(response.Part, s3types.PartInfo{
			PartNumber:   part.PartNumber,
			LastModified: part.LastModified.Format(time.RFC3339),
			ETag:         part.ETag,
			Size:         part.Size,
		})
	}

	writeXML(w, http.StatusOK, response)
}

// HeadObject retrieves object metadata.
func (h *Handler) HeadObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")
	versionID := r.URL.Query().Get("versionId")

	var (
		meta *metadata.ObjectMeta
		err  error
	)

	if versionID != "" {
		meta, err = h.object.HeadObjectVersion(ctx, bucketName, key, versionID)
	} else {
		meta, err = h.object.HeadObject(ctx, bucketName, key)
	}

	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusInternalServerError)

		return
	}

	// Handle delete markers
	if meta.DeleteMarker {
		w.Header().Set("X-Amz-Delete-Marker", "true")

		if meta.VersionID != "" {
			w.Header().Set("X-Amz-Version-Id", meta.VersionID)
		}

		w.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	w.Header().Set("Content-Type", meta.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(meta.Size, 10))
	w.Header().Set("ETag", meta.ETag)
	w.Header().Set("Last-Modified", meta.ModifiedAt.Format(http.TimeFormat))

	if meta.VersionID != "" {
		w.Header().Set("X-Amz-Version-Id", meta.VersionID)
	}

	for k, v := range meta.Metadata {
		w.Header().Set("x-amz-meta-"+k, v)
	}

	w.WriteHeader(http.StatusOK)
}

// DeleteObject deletes an object or aborts a multipart upload.
func (h *Handler) DeleteObject(w http.ResponseWriter, r *http.Request) {
	// Check for AbortMultipartUpload
	if uploadID := r.URL.Query().Get("uploadId"); uploadID != "" {
		h.AbortMultipartUpload(w, r)
		return
	}

	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")
	versionID := r.URL.Query().Get("versionId")

	result, err := h.object.DeleteObjectVersion(ctx, bucketName, key, versionID)
	if err != nil {
		// S3 returns 204 even if object doesn't exist
		if !strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Set version-related response headers
	if result != nil {
		if result.VersionID != "" {
			w.Header().Set("X-Amz-Version-Id", result.VersionID)
		}

		if result.DeleteMarker {
			w.Header().Set("X-Amz-Delete-Marker", "true")

			if result.DeleteMarkerVersionID != "" {
				w.Header().Set("X-Amz-Version-Id", result.DeleteMarkerVersionID)
			}
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// AbortMultipartUpload aborts an in-progress multipart upload.
func (h *Handler) AbortMultipartUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")
	uploadID := r.URL.Query().Get("uploadId")

	err := h.object.AbortMultipartUpload(ctx, bucketName, key, uploadID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchUpload", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// CopyObject copies an object.
func (h *Handler) CopyObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dstBucket := chi.URLParam(r, "bucket")
	dstKey := chi.URLParam(r, "key")

	// Get owner from authenticated context
	owner := middleware.GetOwnerID(ctx)

	// Parse copy source
	copySource := r.Header.Get("X-Amz-Copy-Source")
	copySource = strings.TrimPrefix(copySource, "/")

	parts := strings.SplitN(copySource, "/", 2)
	if len(parts) != 2 {
		writeS3Error(w, "InvalidArgument", "Invalid copy source", http.StatusBadRequest)
		return
	}

	srcBucket, srcKey := parts[0], parts[1]

	// Parse tagging directive
	var opts *object.CopyObjectOptions

	taggingDirective := r.Header.Get("X-Amz-Tagging-Directive")
	if taggingDirective != "" {
		opts = &object.CopyObjectOptions{}

		switch strings.ToUpper(taggingDirective) {
		case "COPY":
			opts.TaggingDirective = object.TaggingDirectiveCopy
		case "REPLACE":
			opts.TaggingDirective = object.TaggingDirectiveReplace
			// Parse tags from x-amz-tagging header
			if taggingHeader := r.Header.Get("X-Amz-Tagging"); taggingHeader != "" {
				tags, err := object.ParseTaggingHeader(taggingHeader)
				if err != nil {
					writeS3Error(w, "InvalidArgument", err.Error(), http.StatusBadRequest)
					return
				}

				opts.Tags = tags
			}
		default:
			writeS3Error(w, "InvalidArgument", "Invalid x-amz-tagging-directive value", http.StatusBadRequest)
			return
		}
	}

	meta, err := h.object.CopyObjectWithOptions(ctx, srcBucket, srcKey, dstBucket, dstKey, owner, opts)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchKey", err.Error(), http.StatusNotFound)
			return
		}

		if strings.Contains(err.Error(), "invalid tags") {
			writeS3Error(w, "InvalidTag", err.Error(), http.StatusBadRequest)
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

// handleObjectPost handles POST requests on objects (multipart operations, select, restore).
func (h *Handler) handleObjectPost(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	// Check for S3 Select
	if _, ok := query["select"]; ok {
		h.SelectObjectContent(w, r)
		return
	}

	// Check for restore from GLACIER/DEEP_ARCHIVE
	if _, ok := query["restore"]; ok {
		h.RestoreObject(w, r)
		return
	}

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

// CreateMultipartUpload initiates a multipart upload.
func (h *Handler) CreateMultipartUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")

	// Get owner from authenticated context
	owner := middleware.GetOwnerID(ctx)

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Parse user metadata from headers
	userMetadata := make(map[string]string)

	for key, values := range r.Header {
		if strings.HasPrefix(strings.ToLower(key), "x-amz-meta-") {
			metaKey := strings.TrimPrefix(strings.ToLower(key), "x-amz-meta-")
			userMetadata[metaKey] = values[0]
		}
	}

	upload, err := h.object.CreateMultipartUpload(ctx, bucketName, key, contentType, owner, userMetadata)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
			return
		}

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

// UploadPart uploads a part of a multipart upload.
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

// CompleteMultipartUpload completes a multipart upload.
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

	// Validate request has at least one part
	if len(req.Part) == 0 {
		writeS3Error(w, "MalformedXML", "At least one part must be specified", http.StatusBadRequest)
		return
	}

	// Convert to service layer format with ETag validation support
	var parts []object.CompletePart
	for _, part := range req.Part {
		parts = append(parts, object.CompletePart{
			PartNumber: part.PartNumber,
			ETag:       part.ETag,
		})
	}

	meta, err := h.object.CompleteMultipartUpload(ctx, bucketName, key, uploadID, parts)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchUpload", err.Error(), http.StatusNotFound)
			return
		}

		if strings.Contains(err.Error(), "ETag mismatch") {
			writeS3Error(w, "InvalidPart", err.Error(), http.StatusBadRequest)
			return
		}

		if strings.Contains(err.Error(), "ascending order") {
			writeS3Error(w, "InvalidPartOrder", err.Error(), http.StatusBadRequest)
			return
		}

		if strings.Contains(err.Error(), "too small") {
			writeS3Error(w, "EntityTooSmall", err.Error(), http.StatusBadRequest)
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

// ListMultipartUploads lists in-progress multipart uploads.
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

// PutBucketVersioning enables or suspends versioning for a bucket.
func (h *Handler) PutBucketVersioning(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	// Parse the versioning configuration from request body
	var config s3types.VersioningConfiguration

	err := xml.NewDecoder(r.Body).Decode(&config)
	if err != nil {
		writeS3Error(w, "MalformedXML", err.Error(), http.StatusBadRequest)
		return
	}

	// Validate status
	var status metadata.VersioningStatus

	switch config.Status {
	case statusEnabled:
		status = metadata.VersioningEnabled
	case "Suspended":
		status = metadata.VersioningSuspended
	case "":
		// Empty status is not allowed
		writeS3Error(w, "MalformedXML", "VersioningConfiguration must include Status", http.StatusBadRequest)
		return
	default:
		writeS3Error(w, "MalformedXML", "Invalid versioning status", http.StatusBadRequest)
		return
	}

	err = h.bucket.SetVersioning(ctx, bucketName, status)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
			return
		}

		if strings.Contains(err.Error(), "cannot be disabled") {
			writeS3Error(w, "IllegalVersioningConfigurationException", err.Error(), http.StatusBadRequest)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusOK)
}

// ListObjectVersions lists all versions of objects in a bucket.
func (h *Handler) ListObjectVersions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	query := r.URL.Query()

	prefix := query.Get("prefix")
	delimiter := query.Get("delimiter")
	keyMarker := query.Get("key-marker")
	versionIDMarker := query.Get("version-id-marker")
	maxKeysStr := query.Get("max-keys")

	maxKeys := 1000

	if maxKeysStr != "" {
		if mk, err := strconv.Atoi(maxKeysStr); err == nil && mk > 0 && mk <= 1000 {
			maxKeys = mk
		}
	}

	listing, err := h.object.ListObjectVersions(ctx, bucketName, prefix, delimiter, keyMarker, versionIDMarker, maxKeys)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	response := s3types.ListVersionsResult{
		Name:                bucketName,
		Prefix:              prefix,
		Delimiter:           delimiter,
		KeyMarker:           keyMarker,
		VersionIdMarker:     versionIDMarker,
		MaxKeys:             maxKeys,
		IsTruncated:         listing.IsTruncated,
		NextKeyMarker:       listing.NextKeyMarker,
		NextVersionIdMarker: listing.NextVersionIDMarker,
	}

	// Add versions
	for _, v := range listing.Versions {
		response.Version = append(response.Version, s3types.ObjectVersion{
			Key:          v.Key,
			VersionId:    v.VersionID,
			IsLatest:     v.IsLatest,
			LastModified: v.ModifiedAt.Format(time.RFC3339),
			ETag:         v.ETag,
			Size:         v.Size,
			StorageClass: v.StorageClass,
			Owner: &s3types.Owner{
				ID:          v.Owner,
				DisplayName: v.Owner,
			},
		})
	}

	// Add delete markers
	for _, dm := range listing.DeleteMarkers {
		response.DeleteMarker = append(response.DeleteMarker, s3types.DeleteMarker{
			Key:          dm.Key,
			VersionId:    dm.VersionID,
			IsLatest:     dm.IsLatest,
			LastModified: dm.ModifiedAt.Format(time.RFC3339),
			Owner: &s3types.Owner{
				ID:          dm.Owner,
				DisplayName: dm.Owner,
			},
		})
	}

	// Add common prefixes
	for _, cp := range listing.CommonPrefixes {
		response.CommonPrefixes = append(response.CommonPrefixes, s3types.CommonPrefix{
			Prefix: cp,
		})
	}

	writeXML(w, http.StatusOK, response)
}

// GetObjectVersion retrieves a specific version of an object.
func (h *Handler) GetObjectVersion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")
	versionID := r.URL.Query().Get("versionId")

	reader, meta, err := h.object.GetObjectVersion(ctx, bucketName, key, versionID)
	if err != nil {
		// Check if it's a delete marker
		if strings.Contains(err.Error(), "delete marker") {
			w.Header().Set("X-Amz-Delete-Marker", "true")

			if meta != nil && meta.VersionID != "" {
				w.Header().Set("X-Amz-Version-Id", meta.VersionID)
			}

			writeS3Error(w, "MethodNotAllowed", "The specified method is not allowed against this resource", http.StatusMethodNotAllowed)

			return
		}

		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchVersion", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	defer func() { _ = reader.Close() }()

	// Set response headers
	w.Header().Set("Content-Type", meta.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(meta.Size, 10))
	w.Header().Set("ETag", meta.ETag)
	w.Header().Set("Last-Modified", meta.ModifiedAt.Format(http.TimeFormat))

	if meta.VersionID != "" {
		w.Header().Set("X-Amz-Version-Id", meta.VersionID)
	}

	// Set user metadata
	for k, v := range meta.Metadata {
		w.Header().Set("x-amz-meta-"+k, v)
	}

	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, reader)
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
	_, _ = w.Write([]byte(policy))
}

func (h *Handler) GetBucketTagging(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	tags, err := h.bucket.GetBucketTagging(ctx, bucketName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	if len(tags) == 0 {
		writeS3Error(w, "NoSuchTagSet", "The TagSet does not exist", http.StatusNotFound)
		return
	}

	response := s3types.Tagging{}
	for k, v := range tags {
		response.TagSet.Tag = append(response.TagSet.Tag, s3types.Tag{Key: k, Value: v})
	}

	writeXML(w, http.StatusOK, response)
}

// PutBucketTagging sets bucket tags.
func (h *Handler) PutBucketTagging(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	// Parse request body
	var tagging s3types.Tagging
	if err := xml.NewDecoder(r.Body).Decode(&tagging); err != nil {
		writeS3Error(w, "MalformedXML", "The XML you provided was not well-formed", http.StatusBadRequest)
		return
	}

	// Convert tags to map
	tags := make(map[string]string)
	for _, tag := range tagging.TagSet.Tag {
		// Check for duplicate keys
		if _, exists := tags[tag.Key]; exists {
			writeS3Error(w, "InvalidTag", "Duplicate tag key: "+tag.Key, http.StatusBadRequest)
			return
		}

		tags[tag.Key] = tag.Value
	}

	err := h.bucket.PutBucketTagging(ctx, bucketName, tags)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
			return
		}

		if strings.Contains(err.Error(), "exceeds maximum") ||
			strings.Contains(err.Error(), "cannot be empty") ||
			strings.Contains(err.Error(), "reserved") {
			writeS3Error(w, "InvalidTag", err.Error(), http.StatusBadRequest)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// DeleteBucketTagging deletes bucket tags.
func (h *Handler) DeleteBucketTagging(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	err := h.bucket.DeleteBucketTagging(ctx, bucketName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetObjectTagging returns object tags.
func (h *Handler) GetObjectTagging(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")

	tags, err := h.object.GetObjectTagging(ctx, bucketName, key)
	if err != nil {
		if strings.Contains(err.Error(), "bucket not found") {
			writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
			return
		}

		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchKey", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	response := s3types.Tagging{}
	for k, v := range tags {
		response.TagSet.Tag = append(response.TagSet.Tag, s3types.Tag{Key: k, Value: v})
	}

	writeXML(w, http.StatusOK, response)
}

// PutObjectTagging sets object tags.
func (h *Handler) PutObjectTagging(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")

	// Parse request body
	var tagging s3types.Tagging
	if err := xml.NewDecoder(r.Body).Decode(&tagging); err != nil {
		writeS3Error(w, "MalformedXML", "The XML you provided was not well-formed", http.StatusBadRequest)
		return
	}

	// Convert tags to map
	tags := make(map[string]string)
	for _, tag := range tagging.TagSet.Tag {
		// Check for duplicate keys
		if _, exists := tags[tag.Key]; exists {
			writeS3Error(w, "InvalidTag", "Duplicate tag key: "+tag.Key, http.StatusBadRequest)
			return
		}

		tags[tag.Key] = tag.Value
	}

	err := h.object.PutObjectTagging(ctx, bucketName, key, tags)
	if err != nil {
		if strings.Contains(err.Error(), "bucket not found") {
			writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
			return
		}

		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchKey", err.Error(), http.StatusNotFound)
			return
		}

		if strings.Contains(err.Error(), "exceeds maximum") ||
			strings.Contains(err.Error(), "cannot be empty") ||
			strings.Contains(err.Error(), "reserved") ||
			strings.Contains(err.Error(), "delete marker") {
			writeS3Error(w, "InvalidTag", err.Error(), http.StatusBadRequest)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusOK)
}

// DeleteObjectTagging deletes object tags.
func (h *Handler) DeleteObjectTagging(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")

	err := h.object.DeleteObjectTagging(ctx, bucketName, key)
	if err != nil {
		if strings.Contains(err.Error(), "bucket not found") {
			writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
			return
		}

		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "delete marker") {
			writeS3Error(w, "NoSuchKey", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusNoContent)
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
			status = statusEnabled
		}

		s3Rule := s3types.LifecycleRule{
			ID:     rule.ID,
			Status: status,
			Filter: &s3types.LifecycleFilter{
				Prefix: rule.Prefix,
			},
		}

		// Convert expiration
		if rule.ExpirationDays > 0 {
			s3Rule.Expiration = &s3types.LifecycleExpiration{
				Days: rule.ExpirationDays,
			}
		}

		// Convert noncurrent version expiration
		if rule.NoncurrentVersionExpirationDays > 0 {
			s3Rule.NoncurrentVersionExpiration = &s3types.NoncurrentVersionExpiration{
				NoncurrentDays: rule.NoncurrentVersionExpirationDays,
			}
		}

		// Convert transitions
		for _, t := range rule.Transitions {
			s3Rule.Transition = append(s3Rule.Transition, s3types.LifecycleTransition{
				Days:         t.Days,
				StorageClass: t.StorageClass,
			})
		}

		response.Rule = append(response.Rule, s3Rule)
	}

	writeXML(w, http.StatusOK, response)
}

// PutBucketPolicy sets the bucket policy.
func (h *Handler) PutBucketPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	// Read the policy JSON
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeS3Error(w, "InvalidRequest", "Failed to read request body", http.StatusBadRequest)
		return
	}

	policyStr := string(body)

	// Parse and validate the policy
	p, err := policy.ParsePolicy(policyStr)
	if err != nil {
		writeS3Error(w, "MalformedPolicy", err.Error(), http.StatusBadRequest)
		return
	}

	if err := p.Validate(); err != nil {
		writeS3Error(w, "MalformedPolicy", err.Error(), http.StatusBadRequest)
		return
	}

	// Store the policy
	if err := h.bucket.SetBucketPolicy(ctx, bucketName, policyStr); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// DeleteBucketPolicy deletes the bucket policy.
func (h *Handler) DeleteBucketPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	err := h.bucket.DeleteBucketPolicy(ctx, bucketName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// PutBucketCORS sets CORS configuration for a bucket.
func (h *Handler) PutBucketCORS(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	var corsConfig s3types.CORSConfiguration
	if err := xml.NewDecoder(r.Body).Decode(&corsConfig); err != nil {
		writeS3Error(w, "MalformedXML", err.Error(), http.StatusBadRequest)
		return
	}

	// Convert to internal CORS rules and validate
	rules, err := h.bucket.ParseAndValidateCORSRules(corsConfig.CORSRule)
	if err != nil {
		writeS3Error(w, "InvalidRequest", err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.bucket.SetCORS(ctx, bucketName, rules); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusOK)
}

// DeleteBucketCORS deletes CORS configuration for a bucket.
func (h *Handler) DeleteBucketCORS(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	err := h.bucket.DeleteCORS(ctx, bucketName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// PutBucketLifecycle sets lifecycle rules for a bucket.
func (h *Handler) PutBucketLifecycle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	var lifecycleConfig s3types.LifecycleConfiguration

	err := xml.NewDecoder(r.Body).Decode(&lifecycleConfig)
	if err != nil {
		writeS3Error(w, "MalformedXML", err.Error(), http.StatusBadRequest)
		return
	}

	// Validate lifecycle configuration
	if len(lifecycleConfig.Rule) == 0 {
		writeS3Error(w, "MalformedXML", "Lifecycle configuration must have at least one rule", http.StatusBadRequest)
		return
	}

	if len(lifecycleConfig.Rule) > 1000 {
		writeS3Error(w, "MalformedXML", "Lifecycle configuration cannot have more than 1000 rules", http.StatusBadRequest)
		return
	}

	// Convert S3 types to metadata types
	rules := make([]metadata.LifecycleRule, len(lifecycleConfig.Rule))
	for i, r := range lifecycleConfig.Rule {
		rule := metadata.LifecycleRule{
			ID:      r.ID,
			Enabled: strings.EqualFold(r.Status, statusEnabled),
		}

		// Get prefix from Filter or deprecated Prefix field
		if r.Filter != nil {
			rule.Prefix = r.Filter.Prefix
			if r.Filter.And != nil {
				rule.Prefix = r.Filter.And.Prefix
			}
		} else {
			rule.Prefix = r.Prefix
		}

		// Convert expiration
		if r.Expiration != nil {
			rule.ExpirationDays = r.Expiration.Days
		}

		// Convert noncurrent version expiration
		if r.NoncurrentVersionExpiration != nil {
			rule.NoncurrentVersionExpirationDays = r.NoncurrentVersionExpiration.NoncurrentDays
		}

		// Convert transitions
		for _, t := range r.Transition {
			rule.Transitions = append(rule.Transitions, metadata.LifecycleTransition{
				Days:         t.Days,
				StorageClass: t.StorageClass,
			})
		}

		rules[i] = rule
	}

	err = h.bucket.SetLifecycle(ctx, bucketName, rules)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusOK)
}

// DeleteBucketLifecycle deletes lifecycle rules for a bucket.
func (h *Handler) DeleteBucketLifecycle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	err := h.bucket.DeleteLifecycle(ctx, bucketName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// DeleteObjects handles the S3 DeleteObjects (batch delete) API.
func (h *Handler) DeleteObjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	// Parse the XML request body
	var req s3types.DeleteRequest
	if err := xml.NewDecoder(r.Body).Decode(&req); err != nil {
		writeS3Error(w, "MalformedXML", "The XML you provided was not well-formed", http.StatusBadRequest)
		return
	}

	// Validate: maximum 1000 objects per request
	if len(req.Object) > 1000 {
		writeS3Error(w, "MalformedXML", "You have exceeded the maximum number of objects (1000) per delete request", http.StatusBadRequest)
		return
	}

	// Validate: at least one object required
	if len(req.Object) == 0 {
		writeS3Error(w, "MalformedXML", "You must specify at least one object to delete", http.StatusBadRequest)
		return
	}

	// Convert request objects to service input
	objects := make([]object.DeleteObjectInput, 0, len(req.Object))
	for _, obj := range req.Object {
		if obj.Key == "" {
			writeS3Error(w, "InvalidArgument", "Object key cannot be empty", http.StatusBadRequest)
			return
		}

		objects = append(objects, object.DeleteObjectInput{
			Key:       obj.Key,
			VersionID: obj.VersionId,
		})
	}

	// Perform the batch delete
	result, err := h.object.DeleteObjects(ctx, bucketName, objects, req.Quiet)
	if err != nil {
		if strings.Contains(err.Error(), "bucket not found") {
			writeS3Error(w, "NoSuchBucket", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	// Build the response
	response := s3types.DeleteResult{}

	// Add deleted objects (unless Quiet mode and no errors for this object)
	if !req.Quiet {
		for _, deleted := range result.Deleted {
			response.Deleted = append(response.Deleted, s3types.DeletedObject{
				Key:                   deleted.Key,
				VersionId:             deleted.VersionID,
				DeleteMarker:          deleted.DeleteMarker,
				DeleteMarkerVersionId: deleted.DeleteMarkerVersionID,
			})
		}
	}

	// Always add errors
	for _, delErr := range result.Errors {
		response.Error = append(response.Error, s3types.DeleteError{
			Key:       delErr.Key,
			VersionId: delErr.VersionID,
			Code:      delErr.Code,
			Message:   delErr.Message,
		})
	}

	writeXML(w, http.StatusOK, response)
}

// GetBucketLocation returns the bucket's region/location.
func (h *Handler) GetBucketLocation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	location, err := h.bucket.GetLocation(ctx, bucketName)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	// AWS returns empty LocationConstraint for us-east-1
	response := s3types.LocationConstraint{}
	if location != "us-east-1" {
		response.Location = location
	}

	writeXML(w, http.StatusOK, response)
}

// GetBucketAcl returns the bucket's ACL.
func (h *Handler) GetBucketAcl(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	acl, err := h.bucket.GetBucketACL(ctx, bucketName)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	response := s3types.AccessControlPolicy{
		Owner: s3types.Owner{
			ID:          acl.OwnerID,
			DisplayName: acl.OwnerDisplayName,
		},
	}

	for _, grant := range acl.Grants {
		g := s3types.Grant{
			Permission: grant.Permission,
			Grantee: s3types.Grantee{
				Type:        grant.GranteeType,
				ID:          grant.GranteeID,
				DisplayName: grant.DisplayName,
				URI:         grant.GranteeURI,
			},
		}
		response.AccessControlList.Grant = append(response.AccessControlList.Grant, g)
	}

	writeXML(w, http.StatusOK, response)
}

// PutBucketAcl sets the bucket's ACL.
func (h *Handler) PutBucketAcl(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	// Check for canned ACL header
	cannedACL := r.Header.Get("X-Amz-Acl")
	if cannedACL != "" {
		// Handle canned ACL
		owner := middleware.GetOwnerID(ctx)
		acl := &metadata.BucketACL{
			OwnerID:          owner,
			OwnerDisplayName: owner,
		}

		switch cannedACL {
		case "private":
			acl.Grants = []metadata.ACLGrant{
				{GranteeType: "CanonicalUser", GranteeID: owner, DisplayName: owner, Permission: "FULL_CONTROL"},
			}
		case "public-read":
			acl.Grants = []metadata.ACLGrant{
				{GranteeType: "CanonicalUser", GranteeID: owner, DisplayName: owner, Permission: "FULL_CONTROL"},
				{GranteeType: "Group", GranteeURI: "http://acs.amazonaws.com/groups/global/AllUsers", Permission: "READ"},
			}
		case "public-read-write":
			acl.Grants = []metadata.ACLGrant{
				{GranteeType: "CanonicalUser", GranteeID: owner, DisplayName: owner, Permission: "FULL_CONTROL"},
				{GranteeType: "Group", GranteeURI: "http://acs.amazonaws.com/groups/global/AllUsers", Permission: "READ"},
				{GranteeType: "Group", GranteeURI: "http://acs.amazonaws.com/groups/global/AllUsers", Permission: "WRITE"},
			}
		case "authenticated-read":
			acl.Grants = []metadata.ACLGrant{
				{GranteeType: "CanonicalUser", GranteeID: owner, DisplayName: owner, Permission: "FULL_CONTROL"},
				{GranteeType: "Group", GranteeURI: "http://acs.amazonaws.com/groups/global/AuthenticatedUsers", Permission: "READ"},
			}
		default:
			writeS3Error(w, "InvalidArgument", "Invalid canned ACL: "+cannedACL, http.StatusBadRequest)
			return
		}

		err := h.bucket.SetBucketACL(ctx, bucketName, acl)
		if err != nil {
			writeS3ErrorTypedWithResource(w, r, err, bucketName)
			return
		}

		w.WriteHeader(http.StatusOK)

		return
	}

	// Parse XML body
	var aclPolicy s3types.AccessControlPolicy

	err := xml.NewDecoder(r.Body).Decode(&aclPolicy)
	if err != nil {
		writeS3Error(w, "MalformedXML", err.Error(), http.StatusBadRequest)
		return
	}

	acl := &metadata.BucketACL{
		OwnerID:          aclPolicy.Owner.ID,
		OwnerDisplayName: aclPolicy.Owner.DisplayName,
	}

	for _, grant := range aclPolicy.AccessControlList.Grant {
		acl.Grants = append(acl.Grants, metadata.ACLGrant{
			GranteeType: grant.Grantee.Type,
			GranteeID:   grant.Grantee.ID,
			GranteeURI:  grant.Grantee.URI,
			DisplayName: grant.Grantee.DisplayName,
			Permission:  grant.Permission,
		})
	}

	err = h.bucket.SetBucketACL(ctx, bucketName, acl)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GetBucketEncryption returns the bucket's encryption configuration.
func (h *Handler) GetBucketEncryption(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	config, err := h.bucket.GetEncryption(ctx, bucketName)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	response := s3types.ServerSideEncryptionConfiguration{}
	for _, rule := range config.Rules {
		sseRule := s3types.ServerSideEncryptionRule{
			BucketKeyEnabled: rule.BucketKeyEnabled,
		}
		sseRule.ApplyServerSideEncryptionByDefault.SSEAlgorithm = rule.SSEAlgorithm
		sseRule.ApplyServerSideEncryptionByDefault.KMSMasterKeyID = rule.KMSMasterKeyID
		response.Rule = append(response.Rule, sseRule)
	}

	writeXML(w, http.StatusOK, response)
}

// PutBucketEncryption sets the bucket's encryption configuration.
func (h *Handler) PutBucketEncryption(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	var sseConfig s3types.ServerSideEncryptionConfiguration

	err := xml.NewDecoder(r.Body).Decode(&sseConfig)
	if err != nil {
		writeS3Error(w, "MalformedXML", err.Error(), http.StatusBadRequest)
		return
	}

	config := &metadata.EncryptionConfig{}
	for _, rule := range sseConfig.Rule {
		config.Rules = append(config.Rules, metadata.EncryptionRule{
			SSEAlgorithm:     rule.ApplyServerSideEncryptionByDefault.SSEAlgorithm,
			KMSMasterKeyID:   rule.ApplyServerSideEncryptionByDefault.KMSMasterKeyID,
			BucketKeyEnabled: rule.BucketKeyEnabled,
		})
	}

	err = h.bucket.SetEncryption(ctx, bucketName, config)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// DeleteBucketEncryption deletes the bucket's encryption configuration.
func (h *Handler) DeleteBucketEncryption(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	err := h.bucket.DeleteEncryption(ctx, bucketName)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetBucketWebsite returns the bucket's website configuration.
func (h *Handler) GetBucketWebsite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	config, err := h.bucket.GetWebsite(ctx, bucketName)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	response := s3types.WebsiteConfiguration{}
	if config.RedirectAllRequestsTo.HostName != "" {
		response.RedirectAllRequestsTo = &s3types.RedirectAllRequestsTo{
			HostName: config.RedirectAllRequestsTo.HostName,
			Protocol: config.RedirectAllRequestsTo.Protocol,
		}
	} else {
		if config.IndexDocument != "" {
			response.IndexDocument = &s3types.IndexDocument{Suffix: config.IndexDocument}
		}

		if config.ErrorDocument != "" {
			response.ErrorDocument = &s3types.ErrorDocument{Key: config.ErrorDocument}
		}

		if len(config.RoutingRules) > 0 {
			response.RoutingRules = &struct {
				RoutingRule []s3types.RoutingRule `xml:"RoutingRule"`
			}{}
			for _, rule := range config.RoutingRules {
				response.RoutingRules.RoutingRule = append(response.RoutingRules.RoutingRule, s3types.RoutingRule{
					Condition: s3types.RoutingRuleCondition{
						KeyPrefixEquals:             rule.Condition.KeyPrefixEquals,
						HttpErrorCodeReturnedEquals: rule.Condition.HttpErrorCodeReturnedEquals,
					},
					Redirect: s3types.RoutingRuleRedirect{
						Protocol:             rule.Redirect.Protocol,
						HostName:             rule.Redirect.HostName,
						ReplaceKeyPrefixWith: rule.Redirect.ReplaceKeyPrefixWith,
						ReplaceKeyWith:       rule.Redirect.ReplaceKeyWith,
						HttpRedirectCode:     rule.Redirect.HttpRedirectCode,
					},
				})
			}
		}
	}

	writeXML(w, http.StatusOK, response)
}

// PutBucketWebsite sets the bucket's website configuration.
func (h *Handler) PutBucketWebsite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	var websiteConfig s3types.WebsiteConfiguration

	err := xml.NewDecoder(r.Body).Decode(&websiteConfig)
	if err != nil {
		writeS3Error(w, "MalformedXML", err.Error(), http.StatusBadRequest)
		return
	}

	config := &metadata.WebsiteConfig{}
	if websiteConfig.RedirectAllRequestsTo != nil {
		config.RedirectAllRequestsTo.HostName = websiteConfig.RedirectAllRequestsTo.HostName
		config.RedirectAllRequestsTo.Protocol = websiteConfig.RedirectAllRequestsTo.Protocol
	} else {
		if websiteConfig.IndexDocument != nil {
			config.IndexDocument = websiteConfig.IndexDocument.Suffix
		}

		if websiteConfig.ErrorDocument != nil {
			config.ErrorDocument = websiteConfig.ErrorDocument.Key
		}

		if websiteConfig.RoutingRules != nil {
			for _, rule := range websiteConfig.RoutingRules.RoutingRule {
				config.RoutingRules = append(config.RoutingRules, metadata.WebsiteRoutingRule{
					Condition: struct {
						KeyPrefixEquals             string `json:"key_prefix_equals"`
						HttpErrorCodeReturnedEquals string `json:"http_error_code_returned_equals"`
					}{
						KeyPrefixEquals:             rule.Condition.KeyPrefixEquals,
						HttpErrorCodeReturnedEquals: rule.Condition.HttpErrorCodeReturnedEquals,
					},
					Redirect: struct {
						Protocol             string `json:"protocol"`
						HostName             string `json:"host_name"`
						ReplaceKeyPrefixWith string `json:"replace_key_prefix_with"`
						ReplaceKeyWith       string `json:"replace_key_with"`
						HttpRedirectCode     string `json:"http_redirect_code"`
					}{
						Protocol:             rule.Redirect.Protocol,
						HostName:             rule.Redirect.HostName,
						ReplaceKeyPrefixWith: rule.Redirect.ReplaceKeyPrefixWith,
						ReplaceKeyWith:       rule.Redirect.ReplaceKeyWith,
						HttpRedirectCode:     rule.Redirect.HttpRedirectCode,
					},
				})
			}
		}
	}

	err = h.bucket.SetWebsite(ctx, bucketName, config)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// DeleteBucketWebsite deletes the bucket's website configuration.
func (h *Handler) DeleteBucketWebsite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	err := h.bucket.DeleteWebsite(ctx, bucketName)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetBucketLogging returns the bucket's logging configuration.
func (h *Handler) GetBucketLogging(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	config, err := h.bucket.GetLogging(ctx, bucketName)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	response := s3types.BucketLoggingStatus{}
	if config.TargetBucket != "" {
		response.LoggingEnabled = &s3types.LoggingEnabled{
			TargetBucket: config.TargetBucket,
			TargetPrefix: config.TargetPrefix,
		}
	}

	writeXML(w, http.StatusOK, response)
}

// PutBucketLogging sets the bucket's logging configuration.
func (h *Handler) PutBucketLogging(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	var loggingStatus s3types.BucketLoggingStatus

	err := xml.NewDecoder(r.Body).Decode(&loggingStatus)
	if err != nil {
		writeS3Error(w, "MalformedXML", err.Error(), http.StatusBadRequest)
		return
	}

	var config *metadata.LoggingConfig
	if loggingStatus.LoggingEnabled != nil {
		config = &metadata.LoggingConfig{
			TargetBucket: loggingStatus.LoggingEnabled.TargetBucket,
			TargetPrefix: loggingStatus.LoggingEnabled.TargetPrefix,
		}
	}

	err = h.bucket.SetLogging(ctx, bucketName, config)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GetBucketNotificationConfiguration returns the bucket's notification configuration.
func (h *Handler) GetBucketNotificationConfiguration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	config, err := h.bucket.GetNotification(ctx, bucketName)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	response := s3types.NotificationConfiguration{}
	for _, topic := range config.TopicConfigurations {
		response.TopicConfiguration = append(response.TopicConfiguration, s3types.TopicConfiguration{
			Id:    topic.ID,
			Topic: topic.TopicArn,
			Event: topic.Events,
		})
	}

	for _, queue := range config.QueueConfigurations {
		response.QueueConfiguration = append(response.QueueConfiguration, s3types.QueueConfiguration{
			Id:    queue.ID,
			Queue: queue.QueueArn,
			Event: queue.Events,
		})
	}

	for _, lambda := range config.LambdaConfigurations {
		response.LambdaFunctionConfiguration = append(response.LambdaFunctionConfiguration, s3types.LambdaFunctionConfiguration{
			Id:             lambda.ID,
			LambdaFunction: lambda.LambdaArn,
			Event:          lambda.Events,
		})
	}

	writeXML(w, http.StatusOK, response)
}

// PutBucketNotificationConfiguration sets the bucket's notification configuration.
func (h *Handler) PutBucketNotificationConfiguration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	var notifConfig s3types.NotificationConfiguration

	err := xml.NewDecoder(r.Body).Decode(&notifConfig)
	if err != nil {
		writeS3Error(w, "MalformedXML", err.Error(), http.StatusBadRequest)
		return
	}

	config := &metadata.NotificationConfig{}
	for _, topic := range notifConfig.TopicConfiguration {
		config.TopicConfigurations = append(config.TopicConfigurations, metadata.TopicNotification{
			ID:       topic.Id,
			TopicArn: topic.Topic,
			Events:   topic.Event,
		})
	}

	for _, queue := range notifConfig.QueueConfiguration {
		config.QueueConfigurations = append(config.QueueConfigurations, metadata.QueueNotification{
			ID:       queue.Id,
			QueueArn: queue.Queue,
			Events:   queue.Event,
		})
	}

	for _, lambda := range notifConfig.LambdaFunctionConfiguration {
		config.LambdaConfigurations = append(config.LambdaConfigurations, metadata.LambdaNotification{
			ID:        lambda.Id,
			LambdaArn: lambda.LambdaFunction,
			Events:    lambda.Event,
		})
	}

	err = h.bucket.SetNotification(ctx, bucketName, config)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GetBucketReplication returns the bucket's replication configuration.
func (h *Handler) GetBucketReplication(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	config, err := h.bucket.GetReplication(ctx, bucketName)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	response := s3types.ReplicationConfiguration{
		Role: config.Role,
	}
	for _, rule := range config.Rules {
		replRule := s3types.ReplicationRule{
			ID:     rule.ID,
			Status: rule.Status,
			Prefix: rule.Prefix,
			Destination: s3types.ReplicationDestination{
				Bucket:       rule.Destination.Bucket,
				StorageClass: rule.Destination.StorageClass,
				Account:      rule.Destination.Account,
			},
		}
		if rule.DeleteMarkerReplication != "" {
			replRule.DeleteMarkerReplication = &s3types.DeleteMarkerReplication{
				Status: rule.DeleteMarkerReplication,
			}
		}

		response.Rule = append(response.Rule, replRule)
	}

	writeXML(w, http.StatusOK, response)
}

// PutBucketReplication sets the bucket's replication configuration.
func (h *Handler) PutBucketReplication(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	var replConfig s3types.ReplicationConfiguration

	err := xml.NewDecoder(r.Body).Decode(&replConfig)
	if err != nil {
		writeS3Error(w, "MalformedXML", err.Error(), http.StatusBadRequest)
		return
	}

	config := &metadata.ReplicationConfig{
		Role: replConfig.Role,
	}
	for _, rule := range replConfig.Rule {
		replRule := metadata.ReplicationRuleConfig{
			ID:       rule.ID,
			Priority: rule.Priority,
			Status:   rule.Status,
			Prefix:   rule.Prefix,
			Destination: metadata.ReplicationDestinationConfig{
				Bucket:       rule.Destination.Bucket,
				StorageClass: rule.Destination.StorageClass,
				Account:      rule.Destination.Account,
			},
		}
		if rule.DeleteMarkerReplication != nil {
			replRule.DeleteMarkerReplication = rule.DeleteMarkerReplication.Status
		}

		config.Rules = append(config.Rules, replRule)
	}

	err = h.bucket.SetReplication(ctx, bucketName, config)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// DeleteBucketReplication deletes the bucket's replication configuration.
func (h *Handler) DeleteBucketReplication(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	err := h.bucket.DeleteReplication(ctx, bucketName)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetObjectLockConfiguration returns the bucket's object lock configuration.
func (h *Handler) GetObjectLockConfiguration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	config, err := h.bucket.GetObjectLockConfiguration(ctx, bucketName)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	response := s3types.ObjectLockConfiguration{
		ObjectLockEnabled: config.ObjectLockEnabled,
	}
	if config.DefaultRetention != nil {
		response.Rule = &s3types.ObjectLockRule{
			DefaultRetention: &struct {
				Mode  string `xml:"Mode"`
				Days  int    `xml:"Days,omitempty"`
				Years int    `xml:"Years,omitempty"`
			}{
				Mode:  config.DefaultRetention.Mode,
				Days:  config.DefaultRetention.Days,
				Years: config.DefaultRetention.Years,
			},
		}
	}

	writeXML(w, http.StatusOK, response)
}

// PutObjectLockConfiguration sets the bucket's object lock configuration.
func (h *Handler) PutObjectLockConfiguration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	var lockConfig s3types.ObjectLockConfiguration

	err := xml.NewDecoder(r.Body).Decode(&lockConfig)
	if err != nil {
		writeS3Error(w, "MalformedXML", err.Error(), http.StatusBadRequest)
		return
	}

	config := &metadata.ObjectLockConfig{
		ObjectLockEnabled: lockConfig.ObjectLockEnabled,
	}
	if lockConfig.Rule != nil && lockConfig.Rule.DefaultRetention != nil {
		config.DefaultRetention = &metadata.ObjectLockRetention{
			Mode:  lockConfig.Rule.DefaultRetention.Mode,
			Days:  lockConfig.Rule.DefaultRetention.Days,
			Years: lockConfig.Rule.DefaultRetention.Years,
		}
	}

	err = h.bucket.SetObjectLockConfiguration(ctx, bucketName, config)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GetPublicAccessBlock returns the bucket's public access block configuration.
func (h *Handler) GetPublicAccessBlock(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	config, err := h.bucket.GetPublicAccessBlock(ctx, bucketName)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	response := s3types.PublicAccessBlockConfiguration{
		BlockPublicAcls:       config.BlockPublicAcls,
		IgnorePublicAcls:      config.IgnorePublicAcls,
		BlockPublicPolicy:     config.BlockPublicPolicy,
		RestrictPublicBuckets: config.RestrictPublicBuckets,
	}

	writeXML(w, http.StatusOK, response)
}

// PutPublicAccessBlock sets the bucket's public access block configuration.
func (h *Handler) PutPublicAccessBlock(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	var pabConfig s3types.PublicAccessBlockConfiguration

	err := xml.NewDecoder(r.Body).Decode(&pabConfig)
	if err != nil {
		writeS3Error(w, "MalformedXML", err.Error(), http.StatusBadRequest)
		return
	}

	config := &metadata.PublicAccessBlockConfig{
		BlockPublicAcls:       pabConfig.BlockPublicAcls,
		IgnorePublicAcls:      pabConfig.IgnorePublicAcls,
		BlockPublicPolicy:     pabConfig.BlockPublicPolicy,
		RestrictPublicBuckets: pabConfig.RestrictPublicBuckets,
	}

	err = h.bucket.SetPublicAccessBlock(ctx, bucketName, config)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// DeletePublicAccessBlock deletes the bucket's public access block configuration.
func (h *Handler) DeletePublicAccessBlock(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	err := h.bucket.DeletePublicAccessBlock(ctx, bucketName)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetBucketOwnershipControls returns the bucket's ownership controls.
func (h *Handler) GetBucketOwnershipControls(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	config, err := h.bucket.GetOwnershipControls(ctx, bucketName)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	response := s3types.OwnershipControls{}
	for _, rule := range config.Rules {
		response.Rules = append(response.Rules, struct {
			ObjectOwnership string `xml:"ObjectOwnership"`
		}{
			ObjectOwnership: rule.ObjectOwnership,
		})
	}

	writeXML(w, http.StatusOK, response)
}

// PutBucketOwnershipControls sets the bucket's ownership controls.
func (h *Handler) PutBucketOwnershipControls(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	var ownershipConfig s3types.OwnershipControls

	err := xml.NewDecoder(r.Body).Decode(&ownershipConfig)
	if err != nil {
		writeS3Error(w, "MalformedXML", err.Error(), http.StatusBadRequest)
		return
	}

	config := &metadata.OwnershipControlsConfig{}
	for _, rule := range ownershipConfig.Rules {
		config.Rules = append(config.Rules, metadata.OwnershipControlsRule{
			ObjectOwnership: rule.ObjectOwnership,
		})
	}

	err = h.bucket.SetOwnershipControls(ctx, bucketName, config)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// DeleteBucketOwnershipControls deletes the bucket's ownership controls.
func (h *Handler) DeleteBucketOwnershipControls(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	err := h.bucket.DeleteOwnershipControls(ctx, bucketName)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetBucketAccelerateConfiguration returns the bucket's accelerate configuration.
func (h *Handler) GetBucketAccelerateConfiguration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	status, err := h.bucket.GetAccelerate(ctx, bucketName)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	response := s3types.AccelerateConfiguration{
		Status: status,
	}

	writeXML(w, http.StatusOK, response)
}

// PutBucketAccelerateConfiguration sets the bucket's accelerate configuration.
func (h *Handler) PutBucketAccelerateConfiguration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")

	var accelConfig s3types.AccelerateConfiguration

	err := xml.NewDecoder(r.Body).Decode(&accelConfig)
	if err != nil {
		writeS3Error(w, "MalformedXML", err.Error(), http.StatusBadRequest)
		return
	}

	err = h.bucket.SetAccelerate(ctx, bucketName, accelConfig.Status)
	if err != nil {
		writeS3ErrorTypedWithResource(w, r, err, bucketName)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GetObjectAcl returns the object's ACL.
func (h *Handler) GetObjectAcl(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")

	meta, err := h.object.HeadObject(ctx, bucketName, key)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchKey", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	// Return default ACL if none set
	acl := meta.ACL
	if acl == nil {
		acl = &metadata.ObjectACL{
			OwnerID:          meta.Owner,
			OwnerDisplayName: meta.Owner,
			Grants: []metadata.ACLGrant{
				{
					GranteeType: "CanonicalUser",
					GranteeID:   meta.Owner,
					DisplayName: meta.Owner,
					Permission:  "FULL_CONTROL",
				},
			},
		}
	}

	response := s3types.AccessControlPolicy{
		Owner: s3types.Owner{
			ID:          acl.OwnerID,
			DisplayName: acl.OwnerDisplayName,
		},
	}
	for _, grant := range acl.Grants {
		response.AccessControlList.Grant = append(response.AccessControlList.Grant, s3types.Grant{
			Permission: grant.Permission,
			Grantee: s3types.Grantee{
				Type:        grant.GranteeType,
				ID:          grant.GranteeID,
				DisplayName: grant.DisplayName,
				URI:         grant.GranteeURI,
			},
		})
	}

	writeXML(w, http.StatusOK, response)
}

// PutObjectAcl sets the object's ACL.
func (h *Handler) PutObjectAcl(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")

	meta, err := h.object.HeadObject(ctx, bucketName, key)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchKey", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	// Check for canned ACL header
	cannedACL := r.Header.Get("X-Amz-Acl")
	if cannedACL != "" {
		acl := &metadata.ObjectACL{
			OwnerID:          meta.Owner,
			OwnerDisplayName: meta.Owner,
		}

		switch cannedACL {
		case "private":
			acl.Grants = []metadata.ACLGrant{
				{GranteeType: "CanonicalUser", GranteeID: meta.Owner, DisplayName: meta.Owner, Permission: "FULL_CONTROL"},
			}
		case "public-read":
			acl.Grants = []metadata.ACLGrant{
				{GranteeType: "CanonicalUser", GranteeID: meta.Owner, DisplayName: meta.Owner, Permission: "FULL_CONTROL"},
				{GranteeType: "Group", GranteeURI: "http://acs.amazonaws.com/groups/global/AllUsers", Permission: "READ"},
			}
		case "public-read-write":
			acl.Grants = []metadata.ACLGrant{
				{GranteeType: "CanonicalUser", GranteeID: meta.Owner, DisplayName: meta.Owner, Permission: "FULL_CONTROL"},
				{GranteeType: "Group", GranteeURI: "http://acs.amazonaws.com/groups/global/AllUsers", Permission: "READ"},
				{GranteeType: "Group", GranteeURI: "http://acs.amazonaws.com/groups/global/AllUsers", Permission: "WRITE"},
			}
		case "authenticated-read":
			acl.Grants = []metadata.ACLGrant{
				{GranteeType: "CanonicalUser", GranteeID: meta.Owner, DisplayName: meta.Owner, Permission: "FULL_CONTROL"},
				{GranteeType: "Group", GranteeURI: "http://acs.amazonaws.com/groups/global/AuthenticatedUsers", Permission: "READ"},
			}
		case "bucket-owner-read":
			acl.Grants = []metadata.ACLGrant{
				{GranteeType: "CanonicalUser", GranteeID: meta.Owner, DisplayName: meta.Owner, Permission: "FULL_CONTROL"},
			}
		case "bucket-owner-full-control":
			acl.Grants = []metadata.ACLGrant{
				{GranteeType: "CanonicalUser", GranteeID: meta.Owner, DisplayName: meta.Owner, Permission: "FULL_CONTROL"},
			}
		default:
			writeS3Error(w, "InvalidArgument", "Invalid canned ACL: "+cannedACL, http.StatusBadRequest)
			return
		}

		err := h.object.SetObjectACL(ctx, bucketName, key, acl)
		if err != nil {
			writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)

		return
	}

	// Parse XML body
	var aclPolicy s3types.AccessControlPolicy
	if err := xml.NewDecoder(r.Body).Decode(&aclPolicy); err != nil {
		writeS3Error(w, "MalformedXML", err.Error(), http.StatusBadRequest)
		return
	}

	acl := &metadata.ObjectACL{
		OwnerID:          aclPolicy.Owner.ID,
		OwnerDisplayName: aclPolicy.Owner.DisplayName,
	}
	for _, grant := range aclPolicy.AccessControlList.Grant {
		acl.Grants = append(acl.Grants, metadata.ACLGrant{
			GranteeType: grant.Grantee.Type,
			GranteeID:   grant.Grantee.ID,
			GranteeURI:  grant.Grantee.URI,
			DisplayName: grant.Grantee.DisplayName,
			Permission:  grant.Permission,
		})
	}

	if err := h.object.SetObjectACL(ctx, bucketName, key, acl); err != nil {
		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GetObjectRetention returns the object's retention configuration.
func (h *Handler) GetObjectRetention(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")
	versionID := r.URL.Query().Get("versionId")

	var (
		meta *metadata.ObjectMeta
		err  error
	)

	if versionID != "" {
		meta, err = h.object.HeadObjectVersion(ctx, bucketName, key, versionID)
	} else {
		meta, err = h.object.HeadObject(ctx, bucketName, key)
	}

	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchKey", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	if meta.ObjectLockMode == "" || meta.ObjectLockRetainUntilDate == nil {
		writeS3Error(w, "NoSuchObjectLockConfiguration", "The object lock configuration does not exist", http.StatusNotFound)
		return
	}

	response := s3types.ObjectRetention{
		Mode:            meta.ObjectLockMode,
		RetainUntilDate: meta.ObjectLockRetainUntilDate.Format(time.RFC3339),
	}

	writeXML(w, http.StatusOK, response)
}

// PutObjectRetention sets the object's retention configuration.
func (h *Handler) PutObjectRetention(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")
	versionID := r.URL.Query().Get("versionId")

	var retention s3types.ObjectRetention
	if err := xml.NewDecoder(r.Body).Decode(&retention); err != nil {
		writeS3Error(w, "MalformedXML", err.Error(), http.StatusBadRequest)
		return
	}

	retainUntil, err := time.Parse(time.RFC3339, retention.RetainUntilDate)
	if err != nil {
		writeS3Error(w, "InvalidArgument", "Invalid RetainUntilDate format", http.StatusBadRequest)
		return
	}

	if err := h.object.SetObjectRetention(ctx, bucketName, key, versionID, retention.Mode, retainUntil); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchKey", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusOK)
}

// GetObjectLegalHold returns the object's legal hold status.
func (h *Handler) GetObjectLegalHold(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")
	versionID := r.URL.Query().Get("versionId")

	var (
		meta *metadata.ObjectMeta
		err  error
	)

	if versionID != "" {
		meta, err = h.object.HeadObjectVersion(ctx, bucketName, key, versionID)
	} else {
		meta, err = h.object.HeadObject(ctx, bucketName, key)
	}

	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchKey", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	status := "OFF"
	if meta.ObjectLockLegalHoldStatus == "ON" {
		status = "ON"
	}

	response := s3types.LegalHold{
		Status: status,
	}

	writeXML(w, http.StatusOK, response)
}

// PutObjectLegalHold sets the object's legal hold status.
func (h *Handler) PutObjectLegalHold(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucketName := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")
	versionID := r.URL.Query().Get("versionId")

	var legalHold s3types.LegalHold

	err := xml.NewDecoder(r.Body).Decode(&legalHold)
	if err != nil {
		writeS3Error(w, "MalformedXML", err.Error(), http.StatusBadRequest)
		return
	}

	if legalHold.Status != "ON" && legalHold.Status != "OFF" {
		writeS3Error(w, "InvalidArgument", "Legal hold status must be ON or OFF", http.StatusBadRequest)
		return
	}

	err = h.object.SetObjectLegalHold(ctx, bucketName, key, versionID, legalHold.Status)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeS3Error(w, "NoSuchKey", err.Error(), http.StatusNotFound)
			return
		}

		writeS3Error(w, "InternalError", err.Error(), http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusOK)
}

// Helper functions

func writeXML(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(v)
}

// writeS3Error writes an S3 error response (legacy signature for backward compatibility).
func writeS3Error(w http.ResponseWriter, code, message string, status int) {
	response := s3types.ErrorResponse{
		Code:    code,
		Message: message,
	}
	writeXML(w, status, response)
}

// writeS3ErrorTyped writes an S3 error response using the typed S3Error.
func writeS3ErrorTyped(w http.ResponseWriter, r *http.Request, err error) {
	requestID := middleware.GetRequestID(r.Context())

	var s3err s3errors.S3Error
	if errors.As(err, &s3err) {
		s3err = s3err.WithRequestID(requestID)
		s3errors.WriteS3Error(w, s3err)
	} else {
		// Wrap unknown errors as internal errors
		internalErr := s3errors.ErrInternalError.WithMessage(err.Error()).WithRequestID(requestID)
		s3errors.WriteS3Error(w, internalErr)
	}
}

// writeS3ErrorTypedWithResource writes an S3 error with resource context.
func writeS3ErrorTypedWithResource(w http.ResponseWriter, r *http.Request, err error, resource string) {
	requestID := middleware.GetRequestID(r.Context())

	var s3err s3errors.S3Error
	if errors.As(err, &s3err) {
		s3err = s3err.WithResource(resource).WithRequestID(requestID)
		s3errors.WriteS3Error(w, s3err)
	} else {
		// Wrap unknown errors as internal errors
		internalErr := s3errors.ErrInternalError.WithMessage(err.Error()).WithResource(resource).WithRequestID(requestID)
		s3errors.WriteS3Error(w, internalErr)
	}
}
