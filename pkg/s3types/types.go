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
	XMLName  xml.Name   `xml:"ListPartsResult"`
	Bucket   string     `xml:"Bucket"`
	Key      string     `xml:"Key"`
	UploadId string     `xml:"UploadId"`
	Part     []PartInfo `xml:"Part"`
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

// Tagging represents object or bucket tagging
type Tagging struct {
	XMLName xml.Name `xml:"Tagging"`
	TagSet  struct {
		Tag []Tag `xml:"Tag"`
	} `xml:"TagSet"`
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

// LifecycleRule represents a lifecycle rule
type LifecycleRule struct {
	ID         string `xml:"ID,omitempty"`
	Status     string `xml:"Status"`
	Prefix     string `xml:"Prefix"`
	Expiration struct {
		Days int `xml:"Days,omitempty"`
	} `xml:"Expiration,omitempty"`
	Transition []struct {
		Days         int    `xml:"Days"`
		StorageClass string `xml:"StorageClass"`
	} `xml:"Transition,omitempty"`
}

// LifecycleConfiguration represents bucket lifecycle configuration
type LifecycleConfiguration struct {
	XMLName xml.Name        `xml:"LifecycleConfiguration"`
	Rule    []LifecycleRule `xml:"Rule"`
}

// DeleteRequest represents a batch delete request
type DeleteRequest struct {
	XMLName xml.Name `xml:"Delete"`
	Quiet   bool     `xml:"Quiet"`
	Object  []struct {
		Key       string `xml:"Key"`
		VersionId string `xml:"VersionId,omitempty"`
	} `xml:"Object"`
}

// DeleteResult represents a batch delete response
type DeleteResult struct {
	XMLName xml.Name `xml:"DeleteResult"`
	Deleted []struct {
		Key       string `xml:"Key"`
		VersionId string `xml:"VersionId,omitempty"`
	} `xml:"Deleted"`
	Error []struct {
		Key     string `xml:"Key"`
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	} `xml:"Error,omitempty"`
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
	KeyMarker           string          `xml:"KeyMarker,omitempty"`
	VersionIdMarker     string          `xml:"VersionIdMarker,omitempty"`
	NextKeyMarker       string          `xml:"NextKeyMarker,omitempty"`
	NextVersionIdMarker string          `xml:"NextVersionIdMarker,omitempty"`
	MaxKeys             int             `xml:"MaxKeys"`
	IsTruncated         bool            `xml:"IsTruncated"`
	Version             []ObjectVersion `xml:"Version,omitempty"`
	DeleteMarker        []DeleteMarker  `xml:"DeleteMarker,omitempty"`
}
