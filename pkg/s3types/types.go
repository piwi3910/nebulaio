package s3types

import "encoding/xml"

// S3 XML types for API responses

// ErrorResponse represents an S3 error response.
type ErrorResponse struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource,omitempty"`
	RequestID string   `xml:"RequestId,omitempty"`
}

// Owner represents a bucket or object owner.
type Owner struct {
	ID          string `xml:"ID"`
	DisplayName string `xml:"DisplayName"`
}

// BucketInfo represents bucket information in list response.
type BucketInfo struct {
	Name         string `xml:"Name"`
	CreationDate string `xml:"CreationDate"`
}

// ListAllMyBucketsResult is the response for ListBuckets.
type ListAllMyBucketsResult struct {
	XMLName xml.Name `xml:"ListAllMyBucketsResult"`
	Owner   Owner    `xml:"Owner"`
	Buckets struct {
		Bucket []BucketInfo `xml:"Bucket"`
	} `xml:"Buckets"`
}

// ObjectInfo represents object information in list response.
type ObjectInfo struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
	Owner        *Owner `xml:"Owner,omitempty"`
}

// CommonPrefix represents a common prefix in list response.
type CommonPrefix struct {
	Prefix string `xml:"Prefix"`
}

// ListBucketResult is the response for ListObjectsV2.
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

// CopyObjectResult is the response for CopyObject.
type CopyObjectResult struct {
	XMLName      xml.Name `xml:"CopyObjectResult"`
	ETag         string   `xml:"ETag"`
	LastModified string   `xml:"LastModified"`
}

// InitiateMultipartUploadResult is the response for CreateMultipartUpload.
type InitiateMultipartUploadResult struct {
	XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	UploadId string   `xml:"UploadId"`
}

// CompleteMultipartUploadRequest is the request for CompleteMultipartUpload.
type CompleteMultipartUploadRequest struct {
	XMLName xml.Name `xml:"CompleteMultipartUpload"`
	Part    []struct {
		PartNumber int    `xml:"PartNumber"`
		ETag       string `xml:"ETag"`
	} `xml:"Part"`
}

// CompleteMultipartUploadResult is the response for CompleteMultipartUpload.
type CompleteMultipartUploadResult struct {
	XMLName  xml.Name `xml:"CompleteMultipartUploadResult"`
	Location string   `xml:"Location"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	ETag     string   `xml:"ETag"`
}

// MultipartUploadInfo represents a multipart upload in list response.
type MultipartUploadInfo struct {
	Key          string `xml:"Key"`
	UploadId     string `xml:"UploadId"`
	Initiator    *Owner `xml:"Initiator,omitempty"`
	Owner        *Owner `xml:"Owner,omitempty"`
	StorageClass string `xml:"StorageClass,omitempty"`
	Initiated    string `xml:"Initiated"`
}

// ListMultipartUploadsResult is the response for ListMultipartUploads.
type ListMultipartUploadsResult struct {
	XMLName    xml.Name              `xml:"ListMultipartUploadsResult"`
	Bucket     string                `xml:"Bucket"`
	KeyMarker  string                `xml:"KeyMarker,omitempty"`
	Upload     []MultipartUploadInfo `xml:"Upload"`
	MaxUploads int                   `xml:"MaxUploads,omitempty"`
}

// PartInfo represents a part in list parts response.
type PartInfo struct {
	PartNumber   int    `xml:"PartNumber"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
}

// ListPartsResult is the response for ListParts.
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

// ListPartsRequest represents query parameters for ListParts API.
type ListPartsRequest struct {
	Bucket           string
	Key              string
	UploadId         string
	MaxParts         int
	PartNumberMarker int
}

// AbortMultipartUploadResult is an empty response for AbortMultipartUpload
// S3 returns 204 No Content on success, but we define this for consistency.
type AbortMultipartUploadResult struct {
	XMLName xml.Name `xml:"AbortMultipartUploadResult"`
}

// VersioningConfiguration represents bucket versioning configuration.
type VersioningConfiguration struct {
	XMLName   xml.Name `xml:"VersioningConfiguration"`
	Status    string   `xml:"Status,omitempty"`
	MFADelete string   `xml:"MfaDelete,omitempty"`
}

// Tag represents a tag.
type Tag struct {
	Key   string `xml:"Key"`
	Value string `xml:"Value"`
}

// TagSet represents a set of tags.
type TagSet struct {
	Tag []Tag `xml:"Tag"`
}

// Tagging represents object or bucket tagging.
type Tagging struct {
	XMLName xml.Name `xml:"Tagging"`
	TagSet  TagSet   `xml:"TagSet"`
}

// CORSRule represents a CORS rule.
type CORSRule struct {
	AllowedOrigin []string `xml:"AllowedOrigin"`
	AllowedMethod []string `xml:"AllowedMethod"`
	AllowedHeader []string `xml:"AllowedHeader,omitempty"`
	ExposeHeader  []string `xml:"ExposeHeader,omitempty"`
	MaxAgeSeconds int      `xml:"MaxAgeSeconds,omitempty"`
}

// CORSConfiguration represents bucket CORS configuration.
type CORSConfiguration struct {
	XMLName  xml.Name   `xml:"CORSConfiguration"`
	CORSRule []CORSRule `xml:"CORSRule"`
}

// LifecycleTag represents a tag filter in lifecycle rules.
type LifecycleTag struct {
	Key   string `xml:"Key"`
	Value string `xml:"Value"`
}

// LifecycleAnd combines multiple filter conditions.
type LifecycleAnd struct {
	Prefix string         `xml:"Prefix,omitempty"`
	Tag    []LifecycleTag `xml:"Tag,omitempty"`
}

// LifecycleFilter specifies which objects the rule applies to.
type LifecycleFilter struct {
	Prefix string        `xml:"Prefix,omitempty"`
	Tag    *LifecycleTag `xml:"Tag,omitempty"`
	And    *LifecycleAnd `xml:"And,omitempty"`
}

// LifecycleExpiration specifies when objects should be deleted.
type LifecycleExpiration struct {
	Days                      int    `xml:"Days,omitempty"`
	Date                      string `xml:"Date,omitempty"`
	ExpiredObjectDeleteMarker bool   `xml:"ExpiredObjectDeleteMarker,omitempty"`
}

// LifecycleTransition specifies when objects should transition to a different storage class.
type LifecycleTransition struct {
	Days         int    `xml:"Days,omitempty"`
	Date         string `xml:"Date,omitempty"`
	StorageClass string `xml:"StorageClass"`
}

// NoncurrentVersionExpiration specifies when noncurrent versions should be deleted.
type NoncurrentVersionExpiration struct {
	NoncurrentDays          int `xml:"NoncurrentDays,omitempty"`
	NewerNoncurrentVersions int `xml:"NewerNoncurrentVersions,omitempty"`
}

// NoncurrentVersionTransition specifies when noncurrent versions should transition.
type NoncurrentVersionTransition struct {
	NoncurrentDays int    `xml:"NoncurrentDays"`
	StorageClass   string `xml:"StorageClass"`
}

// AbortIncompleteMultipartUpload specifies when to abort incomplete multipart uploads.
type AbortIncompleteMultipartUpload struct {
	DaysAfterInitiation int `xml:"DaysAfterInitiation"`
}

// LifecycleRule represents a lifecycle rule.
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

// LifecycleConfiguration represents bucket lifecycle configuration.
type LifecycleConfiguration struct {
	XMLName xml.Name        `xml:"LifecycleConfiguration"`
	Rule    []LifecycleRule `xml:"Rule"`
}

// DeleteObjectRequest represents an object to delete in a batch request.
type DeleteObjectRequest struct {
	Key       string `xml:"Key"`
	VersionId string `xml:"VersionId,omitempty"`
}

// DeleteRequest represents a batch delete request.
type DeleteRequest struct {
	XMLName xml.Name              `xml:"Delete"`
	Quiet   bool                  `xml:"Quiet"`
	Object  []DeleteObjectRequest `xml:"Object"`
}

// DeletedObject represents a successfully deleted object.
type DeletedObject struct {
	Key                   string `xml:"Key"`
	VersionId             string `xml:"VersionId,omitempty"`
	DeleteMarker          bool   `xml:"DeleteMarker,omitempty"`
	DeleteMarkerVersionId string `xml:"DeleteMarkerVersionId,omitempty"`
}

// DeleteError represents a delete error for a specific object.
type DeleteError struct {
	Key       string `xml:"Key"`
	VersionId string `xml:"VersionId,omitempty"`
	Code      string `xml:"Code"`
	Message   string `xml:"Message"`
}

// DeleteResult represents a batch delete response.
type DeleteResult struct {
	XMLName xml.Name        `xml:"DeleteResult"`
	Deleted []DeletedObject `xml:"Deleted,omitempty"`
	Error   []DeleteError   `xml:"Error,omitempty"`
}

// ObjectVersion represents an object version.
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

// DeleteMarker represents a delete marker.
type DeleteMarker struct {
	Key          string `xml:"Key"`
	VersionId    string `xml:"VersionId"`
	IsLatest     bool   `xml:"IsLatest"`
	LastModified string `xml:"LastModified"`
	Owner        *Owner `xml:"Owner,omitempty"`
}

// ListVersionsResult is the response for ListObjectVersions.
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

// LocationConstraint is the response for GetBucketLocation.
type LocationConstraint struct {
	XMLName  xml.Name `xml:"LocationConstraint"`
	Location string   `xml:",chardata"`
}

// Grantee represents an ACL grantee.
type Grantee struct {
	XMLName      xml.Name `xml:"Grantee"`
	Type         string   `xml:"type,attr"`
	ID           string   `xml:"ID,omitempty"`
	DisplayName  string   `xml:"DisplayName,omitempty"`
	EmailAddress string   `xml:"EmailAddress,omitempty"`
	URI          string   `xml:"URI,omitempty"`
}

// Grant represents an ACL grant.
type Grant struct {
	Grantee    Grantee `xml:"Grantee"`
	Permission string  `xml:"Permission"`
}

// AccessControlList represents the ACL.
type AccessControlList struct {
	Grant []Grant `xml:"Grant"`
}

// AccessControlPolicy is the response for GetBucketAcl and GetObjectAcl.
type AccessControlPolicy struct {
	XMLName           xml.Name          `xml:"AccessControlPolicy"`
	Owner             Owner             `xml:"Owner"`
	AccessControlList AccessControlList `xml:"AccessControlList"`
}

// ServerSideEncryptionRule represents an encryption rule.
type ServerSideEncryptionRule struct {
	ApplyServerSideEncryptionByDefault struct {
		SSEAlgorithm   string `xml:"SSEAlgorithm"`
		KMSMasterKeyID string `xml:"KMSMasterKeyID,omitempty"`
	} `xml:"ApplyServerSideEncryptionByDefault"`
	BucketKeyEnabled bool `xml:"BucketKeyEnabled,omitempty"`
}

// ServerSideEncryptionConfiguration is the response for GetBucketEncryption.
type ServerSideEncryptionConfiguration struct {
	XMLName xml.Name                   `xml:"ServerSideEncryptionConfiguration"`
	Rule    []ServerSideEncryptionRule `xml:"Rule"`
}

// RedirectAllRequestsTo represents a website redirect configuration.
type RedirectAllRequestsTo struct {
	HostName string `xml:"HostName"`
	Protocol string `xml:"Protocol,omitempty"`
}

// IndexDocument represents the index document configuration.
type IndexDocument struct {
	Suffix string `xml:"Suffix"`
}

// ErrorDocument represents the error document configuration.
type ErrorDocument struct {
	Key string `xml:"Key"`
}

// RoutingRuleCondition represents a routing rule condition.
type RoutingRuleCondition struct {
	KeyPrefixEquals             string `xml:"KeyPrefixEquals,omitempty"`
	HttpErrorCodeReturnedEquals string `xml:"HttpErrorCodeReturnedEquals,omitempty"`
}

// RoutingRuleRedirect represents a routing rule redirect.
type RoutingRuleRedirect struct {
	Protocol             string `xml:"Protocol,omitempty"`
	HostName             string `xml:"HostName,omitempty"`
	ReplaceKeyPrefixWith string `xml:"ReplaceKeyPrefixWith,omitempty"`
	ReplaceKeyWith       string `xml:"ReplaceKeyWith,omitempty"`
	HttpRedirectCode     string `xml:"HttpRedirectCode,omitempty"`
}

// RoutingRule represents a website routing rule.
type RoutingRule struct {
	Condition RoutingRuleCondition `xml:"Condition,omitempty"`
	Redirect  RoutingRuleRedirect  `xml:"Redirect"`
}

// WebsiteConfiguration is the response for GetBucketWebsite.
type WebsiteConfiguration struct {
	XMLName               xml.Name               `xml:"WebsiteConfiguration"`
	RedirectAllRequestsTo *RedirectAllRequestsTo `xml:"RedirectAllRequestsTo,omitempty"`
	IndexDocument         *IndexDocument         `xml:"IndexDocument,omitempty"`
	ErrorDocument         *ErrorDocument         `xml:"ErrorDocument,omitempty"`
	RoutingRules          *struct {
		RoutingRule []RoutingRule `xml:"RoutingRule"`
	} `xml:"RoutingRules,omitempty"`
}

// LoggingEnabled represents bucket logging configuration.
type LoggingEnabled struct {
	TargetBucket string `xml:"TargetBucket"`
	TargetPrefix string `xml:"TargetPrefix,omitempty"`
	TargetGrants *struct {
		Grant []Grant `xml:"Grant"`
	} `xml:"TargetGrants,omitempty"`
}

// BucketLoggingStatus is the response for GetBucketLogging.
type BucketLoggingStatus struct {
	XMLName        xml.Name        `xml:"BucketLoggingStatus"`
	LoggingEnabled *LoggingEnabled `xml:"LoggingEnabled,omitempty"`
}

// TopicConfiguration represents an SNS topic notification.
type TopicConfiguration struct {
	Id     string   `xml:"Id,omitempty"`
	Topic  string   `xml:"Topic"`
	Event  []string `xml:"Event"`
	Filter *struct {
		S3Key struct {
			FilterRule []struct {
				Name  string `xml:"Name"`
				Value string `xml:"Value"`
			} `xml:"FilterRule"`
		} `xml:"S3Key"`
	} `xml:"Filter,omitempty"`
}

// QueueConfiguration represents an SQS queue notification.
type QueueConfiguration struct {
	Id    string   `xml:"Id,omitempty"`
	Queue string   `xml:"Queue"`
	Event []string `xml:"Event"`
}

// LambdaFunctionConfiguration represents a Lambda function notification.
type LambdaFunctionConfiguration struct {
	Id             string   `xml:"Id,omitempty"`
	LambdaFunction string   `xml:"CloudFunction"`
	Event          []string `xml:"Event"`
}

// NotificationConfiguration is the response for GetBucketNotification.
type NotificationConfiguration struct {
	XMLName                     xml.Name                      `xml:"NotificationConfiguration"`
	TopicConfiguration          []TopicConfiguration          `xml:"TopicConfiguration,omitempty"`
	QueueConfiguration          []QueueConfiguration          `xml:"QueueConfiguration,omitempty"`
	LambdaFunctionConfiguration []LambdaFunctionConfiguration `xml:"CloudFunctionConfiguration,omitempty"`
}

// ReplicationRuleFilter represents a replication rule filter.
type ReplicationRuleFilter struct {
	Prefix string `xml:"Prefix,omitempty"`
	Tag    *Tag   `xml:"Tag,omitempty"`
	And    *struct {
		Prefix string `xml:"Prefix,omitempty"`
		Tag    []Tag  `xml:"Tag,omitempty"`
	} `xml:"And,omitempty"`
}

// ReplicationDestination represents the replication destination.
type ReplicationDestination struct {
	Bucket       string `xml:"Bucket"`
	StorageClass string `xml:"StorageClass,omitempty"`
	Account      string `xml:"Account,omitempty"`
}

// DeleteMarkerReplication represents delete marker replication settings.
type DeleteMarkerReplication struct {
	Status string `xml:"Status"`
}

// ReplicationRule represents a replication rule.
type ReplicationRule struct {
	ID                      string                   `xml:"ID,omitempty"`
	Priority                int                      `xml:"Priority,omitempty"`
	Status                  string                   `xml:"Status"`
	Filter                  *ReplicationRuleFilter   `xml:"Filter,omitempty"`
	Prefix                  string                   `xml:"Prefix,omitempty"` // Deprecated
	Destination             ReplicationDestination   `xml:"Destination"`
	DeleteMarkerReplication *DeleteMarkerReplication `xml:"DeleteMarkerReplication,omitempty"`
}

// ReplicationConfiguration is the response for GetBucketReplication.
type ReplicationConfiguration struct {
	XMLName xml.Name          `xml:"ReplicationConfiguration"`
	Role    string            `xml:"Role"`
	Rule    []ReplicationRule `xml:"Rule"`
}

// ObjectLockRule represents an object lock rule.
type ObjectLockRule struct {
	DefaultRetention *struct {
		Mode  string `xml:"Mode"`
		Days  int    `xml:"Days,omitempty"`
		Years int    `xml:"Years,omitempty"`
	} `xml:"DefaultRetention,omitempty"`
}

// ObjectLockConfiguration is the response for GetObjectLockConfiguration.
type ObjectLockConfiguration struct {
	XMLName           xml.Name        `xml:"ObjectLockConfiguration"`
	ObjectLockEnabled string          `xml:"ObjectLockEnabled,omitempty"`
	Rule              *ObjectLockRule `xml:"Rule,omitempty"`
}

// ObjectRetention is the response for GetObjectRetention.
type ObjectRetention struct {
	XMLName         xml.Name `xml:"Retention"`
	Mode            string   `xml:"Mode"`
	RetainUntilDate string   `xml:"RetainUntilDate"`
}

// LegalHold is the response for GetObjectLegalHold.
type LegalHold struct {
	XMLName xml.Name `xml:"LegalHold"`
	Status  string   `xml:"Status"`
}

// PublicAccessBlockConfiguration is the response for GetPublicAccessBlock.
type PublicAccessBlockConfiguration struct {
	XMLName               xml.Name `xml:"PublicAccessBlockConfiguration"`
	BlockPublicAcls       bool     `xml:"BlockPublicAcls"`
	IgnorePublicAcls      bool     `xml:"IgnorePublicAcls"`
	BlockPublicPolicy     bool     `xml:"BlockPublicPolicy"`
	RestrictPublicBuckets bool     `xml:"RestrictPublicBuckets"`
}

// OwnershipControls is the response for GetBucketOwnershipControls.
type OwnershipControls struct {
	XMLName xml.Name `xml:"OwnershipControls"`
	Rules   []struct {
		ObjectOwnership string `xml:"ObjectOwnership"`
	} `xml:"Rule"`
}

// AccelerateConfiguration is the response for GetBucketAccelerateConfiguration.
type AccelerateConfiguration struct {
	XMLName xml.Name `xml:"AccelerateConfiguration"`
	Status  string   `xml:"Status,omitempty"`
}

// AnalyticsConfiguration represents analytics config.
type AnalyticsConfiguration struct {
	XMLName              xml.Name `xml:"AnalyticsConfiguration"`
	Id                   string   `xml:"Id"`
	StorageClassAnalysis struct {
		DataExport *struct {
			OutputSchemaVersion string `xml:"OutputSchemaVersion"`
			Destination         struct {
				S3BucketDestination struct {
					Format          string `xml:"Format"`
					BucketAccountId string `xml:"BucketAccountId,omitempty"`
					Bucket          string `xml:"Bucket"`
					Prefix          string `xml:"Prefix,omitempty"`
				} `xml:"S3BucketDestination"`
			} `xml:"Destination"`
		} `xml:"DataExport,omitempty"`
	} `xml:"StorageClassAnalysis"`
}

// ListAnalyticsConfigurationsResult is the response for ListBucketAnalyticsConfigurations.
type ListAnalyticsConfigurationsResult struct {
	XMLName                xml.Name                 `xml:"ListBucketAnalyticsConfigurationsResult"`
	IsTruncated            bool                     `xml:"IsTruncated"`
	ContinuationToken      string                   `xml:"ContinuationToken,omitempty"`
	NextContinuationToken  string                   `xml:"NextContinuationToken,omitempty"`
	AnalyticsConfiguration []AnalyticsConfiguration `xml:"AnalyticsConfiguration,omitempty"`
}

// InventoryConfiguration represents inventory config.
type InventoryConfiguration struct {
	XMLName     xml.Name `xml:"InventoryConfiguration"`
	Id          string   `xml:"Id"`
	IsEnabled   bool     `xml:"IsEnabled"`
	Destination struct {
		S3BucketDestination struct {
			AccountId  string `xml:"AccountId,omitempty"`
			Bucket     string `xml:"Bucket"`
			Format     string `xml:"Format"`
			Prefix     string `xml:"Prefix,omitempty"`
			Encryption *struct {
				SSES3  *struct{} `xml:"SSE-S3,omitempty"`
				SSEKMS *struct {
					KeyId string `xml:"KeyId"`
				} `xml:"SSE-KMS,omitempty"`
			} `xml:"Encryption,omitempty"`
		} `xml:"S3BucketDestination"`
	} `xml:"Destination"`
	Schedule struct {
		Frequency string `xml:"Frequency"`
	} `xml:"Schedule"`
	IncludedObjectVersions string   `xml:"IncludedObjectVersions"`
	OptionalFields         []string `xml:"OptionalFields>Field,omitempty"`
}

// ListInventoryConfigurationsResult is the response for ListBucketInventoryConfigurations.
type ListInventoryConfigurationsResult struct {
	XMLName                xml.Name                 `xml:"ListInventoryConfigurationsResult"`
	IsTruncated            bool                     `xml:"IsTruncated"`
	ContinuationToken      string                   `xml:"ContinuationToken,omitempty"`
	NextContinuationToken  string                   `xml:"NextContinuationToken,omitempty"`
	InventoryConfiguration []InventoryConfiguration `xml:"InventoryConfiguration,omitempty"`
}

// MetricsConfiguration represents metrics config.
type MetricsConfiguration struct {
	XMLName xml.Name `xml:"MetricsConfiguration"`
	Id      string   `xml:"Id"`
	Filter  *struct {
		Prefix      string `xml:"Prefix,omitempty"`
		Tag         *Tag   `xml:"Tag,omitempty"`
		AccessPoint string `xml:"AccessPointArn,omitempty"`
	} `xml:"Filter,omitempty"`
}

// ListMetricsConfigurationsResult is the response for ListBucketMetricsConfigurations.
type ListMetricsConfigurationsResult struct {
	XMLName               xml.Name               `xml:"ListBucketMetricsConfigurationsResult"`
	IsTruncated           bool                   `xml:"IsTruncated"`
	ContinuationToken     string                 `xml:"ContinuationToken,omitempty"`
	NextContinuationToken string                 `xml:"NextContinuationToken,omitempty"`
	MetricsConfiguration  []MetricsConfiguration `xml:"MetricsConfiguration,omitempty"`
}
