package s3types

import "encoding/xml"

// S3 XML types for API responses

// ErrorResponse represents an S3 error response
type ErrorResponse struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource,omitempty"`
	RequestID string   `xml:"RequestId,omitempty"`
}

// Owner represents a bucket or object owner
type Owner struct {
	ID          string `xml:"ID"`
	DisplayName string `xml:"DisplayName"`
}

// BucketInfo represents bucket information in list response
type BucketInfo struct {
	Name         string `xml:"Name"`
	CreationDate string `xml:"CreationDate"`
}

// ListAllMyBucketsResult is the response for ListBuckets
type ListAllMyBucketsResult struct {
	XMLName xml.Name `xml:"ListAllMyBucketsResult"`
	Owner   Owner    `xml:"Owner"`
	Buckets struct {
		Bucket []BucketInfo `xml:"Bucket"`
	} `xml:"Buckets"`
}

// ObjectInfo represents object information in list response
type ObjectInfo struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
	Owner        *Owner `xml:"Owner,omitempty"`
}

// CommonPrefix represents a common prefix in list response
type CommonPrefix struct {
	Prefix string `xml:"Prefix"`
}

// ListBucketResult is the response for ListObjectsV2
type ListBucketResult struct {
	XMLName               xml.Name       `xml:"ListBucketResult"`
	Name                  string         `xml:"Name"`
	Prefix                string         `xml:"Prefix"`
	Delimiter             string         `xml:"Delimiter,omitempty"`
	MaxKeys               int            `xml:"MaxKeys"`
	IsTruncated           bool           `xml:"IsTruncated"`
	ContinuationToken     string         `xml:"ContinuationToken,omitempty"`
	NextContinuationToken string         `xml:"NextContinuationToken,omitempty"`
	KeyCount              int            `xml:"KeyCount"`
	Contents              []ObjectInfo   `xml:"Contents"`
	CommonPrefixes        []CommonPrefix `xml:"CommonPrefixes,omitempty"`
}

// CopyObjectResult is the response for CopyObject
type CopyObjectResult struct {
	XMLName      xml.Name `xml:"CopyObjectResult"`
	ETag         string   `xml:"ETag"`
	LastModified string   `xml:"LastModified"`
}

// InitiateMultipartUploadResult is the response for CreateMultipartUpload
type InitiateMultipartUploadResult struct {
	XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	UploadId string   `xml:"UploadId"`
}

// CompleteMultipartUploadRequest is the request for CompleteMultipartUpload
type CompleteMultipartUploadRequest struct {
	XMLName xml.Name `xml:"CompleteMultipartUpload"`
	Part    []struct {
		PartNumber int    `xml:"PartNumber"`
		ETag       string `xml:"ETag"`
	} `xml:"Part"`
}

// CompleteMultipartUploadResult is the response for CompleteMultipartUpload
type CompleteMultipartUploadResult struct {
	XMLName  xml.Name `xml:"CompleteMultipartUploadResult"`
	Location string   `xml:"Location"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	ETag     string   `xml:"ETag"`
}

// MultipartUploadInfo represents a multipart upload in list response
type MultipartUploadInfo struct {
	Key          string `xml:"Key"`
	UploadId     string `xml:"UploadId"`
	Initiator    *Owner `xml:"Initiator,omitempty"`
	Owner        *Owner `xml:"Owner,omitempty"`
	StorageClass string `xml:"StorageClass,omitempty"`
	Initiated    string `xml:"Initiated"`
}

// ListMultipartUploadsResult is the response for ListMultipartUploads
type ListMultipartUploadsResult struct {
	XMLName    xml.Name              `xml:"ListMultipartUploadsResult"`
	Bucket     string                `xml:"Bucket"`
	KeyMarker  string                `xml:"KeyMarker,omitempty"`
	Upload     []MultipartUploadInfo `xml:"Upload"`
	MaxUploads int                   `xml:"MaxUploads,omitempty"`
}

// PartInfo represents a part in list parts response
type PartInfo struct {
	PartNumber   int    `xml:"PartNumber"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
}

// ListPartsResult is the response for ListParts
type ListPartsResult struct {
	XMLName              xml.Name   `xml:"ListPartsResult"`
	Bucket               string     `xml:"Bucket"`
	Key                  string     `xml:"Key"`
	UploadId             string     `xml:"UploadId"`
	Initiator            *Owner     `xml:"Initiator,omitempty"`
	Owner                *Owner     `xml:"Owner,omitempty"`
	StorageClass         string     `xml:"StorageClass,omitempty"`
	PartNumberMarker     int        `xml:"PartNumberMarker"`
	NextPartNumberMarker int        `xml:"NextPartNumberMarker,omitempty"`
	MaxParts             int        `xml:"MaxParts"`
	IsTruncated          bool       `xml:"IsTruncated"`
	Part                 []PartInfo `xml:"Part"`
}

// ListPartsRequest represents query parameters for ListParts API
type ListPartsRequest struct {
	Bucket           string
	Key              string
	UploadId         string
	MaxParts         int
	PartNumberMarker int
}

// AbortMultipartUploadResult is an empty response for AbortMultipartUpload
// S3 returns 204 No Content on success, but we define this for consistency
type AbortMultipartUploadResult struct {
	XMLName xml.Name `xml:"AbortMultipartUploadResult"`
}

// VersioningConfiguration represents bucket versioning configuration
type VersioningConfiguration struct {
	XMLName   xml.Name `xml:"VersioningConfiguration"`
	Status    string   `xml:"Status,omitempty"`
	MFADelete string   `xml:"MfaDelete,omitempty"`
}

// Tag represents a tag
type Tag struct {
	Key   string `xml:"Key"`
	Value string `xml:"Value"`
}

// TagSet represents a set of tags
type TagSet struct {
	Tag []Tag `xml:"Tag"`
}

// Tagging represents object or bucket tagging
type Tagging struct {
	XMLName xml.Name `xml:"Tagging"`
	TagSet  TagSet   `xml:"TagSet"`
}

// CORSRule represents a CORS rule
type CORSRule struct {
	AllowedOrigin []string `xml:"AllowedOrigin"`
	AllowedMethod []string `xml:"AllowedMethod"`
	AllowedHeader []string `xml:"AllowedHeader,omitempty"`
	ExposeHeader  []string `xml:"ExposeHeader,omitempty"`
	MaxAgeSeconds int      `xml:"MaxAgeSeconds,omitempty"`
}

// CORSConfiguration represents bucket CORS configuration
type CORSConfiguration struct {
	XMLName  xml.Name   `xml:"CORSConfiguration"`
	CORSRule []CORSRule `xml:"CORSRule"`
}

// LifecycleTag represents a tag filter in lifecycle rules
type LifecycleTag struct {
	Key   string `xml:"Key"`
	Value string `xml:"Value"`
}

// LifecycleAnd combines multiple filter conditions
type LifecycleAnd struct {
	Prefix string         `xml:"Prefix,omitempty"`
	Tag    []LifecycleTag `xml:"Tag,omitempty"`
}

// LifecycleFilter specifies which objects the rule applies to
type LifecycleFilter struct {
	Prefix string        `xml:"Prefix,omitempty"`
	Tag    *LifecycleTag `xml:"Tag,omitempty"`
	And    *LifecycleAnd `xml:"And,omitempty"`
}

// LifecycleExpiration specifies when objects should be deleted
type LifecycleExpiration struct {
	Days                      int    `xml:"Days,omitempty"`
	Date                      string `xml:"Date,omitempty"`
	ExpiredObjectDeleteMarker bool   `xml:"ExpiredObjectDeleteMarker,omitempty"`
}

// LifecycleTransition specifies when objects should transition to a different storage class
type LifecycleTransition struct {
	Days         int    `xml:"Days,omitempty"`
	Date         string `xml:"Date,omitempty"`
	StorageClass string `xml:"StorageClass"`
}

// NoncurrentVersionExpiration specifies when noncurrent versions should be deleted
type NoncurrentVersionExpiration struct {
	NoncurrentDays          int `xml:"NoncurrentDays,omitempty"`
	NewerNoncurrentVersions int `xml:"NewerNoncurrentVersions,omitempty"`
}

// NoncurrentVersionTransition specifies when noncurrent versions should transition
type NoncurrentVersionTransition struct {
	NoncurrentDays int    `xml:"NoncurrentDays"`
	StorageClass   string `xml:"StorageClass"`
}

// AbortIncompleteMultipartUpload specifies when to abort incomplete multipart uploads
type AbortIncompleteMultipartUpload struct {
	DaysAfterInitiation int `xml:"DaysAfterInitiation"`
}

// LifecycleRule represents a lifecycle rule
type LifecycleRule struct {
	ID                             string                          `xml:"ID,omitempty"`
	Status                         string                          `xml:"Status"`
	Filter                         *LifecycleFilter                `xml:"Filter,omitempty"`
	Prefix                         string                          `xml:"Prefix,omitempty"` // Deprecated, use Filter
	Expiration                     *LifecycleExpiration            `xml:"Expiration,omitempty"`
	Transition                     []LifecycleTransition           `xml:"Transition,omitempty"`
	NoncurrentVersionExpiration    *NoncurrentVersionExpiration    `xml:"NoncurrentVersionExpiration,omitempty"`
	NoncurrentVersionTransition    []NoncurrentVersionTransition   `xml:"NoncurrentVersionTransition,omitempty"`
	AbortIncompleteMultipartUpload *AbortIncompleteMultipartUpload `xml:"AbortIncompleteMultipartUpload,omitempty"`
}

// LifecycleConfiguration represents bucket lifecycle configuration
type LifecycleConfiguration struct {
	XMLName xml.Name        `xml:"LifecycleConfiguration"`
	Rule    []LifecycleRule `xml:"Rule"`
}

// DeleteObjectRequest represents an object to delete in a batch request
type DeleteObjectRequest struct {
	Key       string `xml:"Key"`
	VersionId string `xml:"VersionId,omitempty"`
}

// DeleteRequest represents a batch delete request
type DeleteRequest struct {
	XMLName xml.Name              `xml:"Delete"`
	Quiet   bool                  `xml:"Quiet"`
	Object  []DeleteObjectRequest `xml:"Object"`
}

// DeletedObject represents a successfully deleted object
type DeletedObject struct {
	Key                   string `xml:"Key"`
	VersionId             string `xml:"VersionId,omitempty"`
	DeleteMarker          bool   `xml:"DeleteMarker,omitempty"`
	DeleteMarkerVersionId string `xml:"DeleteMarkerVersionId,omitempty"`
}

// DeleteError represents a delete error for a specific object
type DeleteError struct {
	Key       string `xml:"Key"`
	VersionId string `xml:"VersionId,omitempty"`
	Code      string `xml:"Code"`
	Message   string `xml:"Message"`
}

// DeleteResult represents a batch delete response
type DeleteResult struct {
	XMLName xml.Name        `xml:"DeleteResult"`
	Deleted []DeletedObject `xml:"Deleted,omitempty"`
	Error   []DeleteError   `xml:"Error,omitempty"`
}

// ObjectVersion represents an object version
type ObjectVersion struct {
	Key          string `xml:"Key"`
	VersionId    string `xml:"VersionId"`
	IsLatest     bool   `xml:"IsLatest"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag,omitempty"`
	Size         int64  `xml:"Size,omitempty"`
	Owner        *Owner `xml:"Owner,omitempty"`
	StorageClass string `xml:"StorageClass,omitempty"`
}

// DeleteMarker represents a delete marker
type DeleteMarker struct {
	Key          string `xml:"Key"`
	VersionId    string `xml:"VersionId"`
	IsLatest     bool   `xml:"IsLatest"`
	LastModified string `xml:"LastModified"`
	Owner        *Owner `xml:"Owner,omitempty"`
}

// ListVersionsResult is the response for ListObjectVersions
type ListVersionsResult struct {
	XMLName             xml.Name        `xml:"ListVersionsResult"`
	Name                string          `xml:"Name"`
	Prefix              string          `xml:"Prefix,omitempty"`
	Delimiter           string          `xml:"Delimiter,omitempty"`
	KeyMarker           string          `xml:"KeyMarker,omitempty"`
	VersionIdMarker     string          `xml:"VersionIdMarker,omitempty"`
	NextKeyMarker       string          `xml:"NextKeyMarker,omitempty"`
	NextVersionIdMarker string          `xml:"NextVersionIdMarker,omitempty"`
	MaxKeys             int             `xml:"MaxKeys"`
	IsTruncated         bool            `xml:"IsTruncated"`
	Version             []ObjectVersion `xml:"Version,omitempty"`
	DeleteMarker        []DeleteMarker  `xml:"DeleteMarker,omitempty"`
	CommonPrefixes      []CommonPrefix  `xml:"CommonPrefixes,omitempty"`
}
