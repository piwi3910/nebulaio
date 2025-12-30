// Package s3errors provides S3-compatible error types and response handling.
// These errors follow the AWS S3 API specification for error responses.
package s3errors

import (
	"encoding/xml"
	"fmt"
	"net/http"
)

// S3Error represents an S3 API error with code, message, status, and context.
type S3Error struct {
	Code       string
	Message    string
	Resource   string
	RequestID  string
	StatusCode int
}

// Error implements the error interface.
func (e S3Error) Error() string {
	if e.Resource != "" {
		return fmt.Sprintf("%s: %s (Resource: %s)", e.Code, e.Message, e.Resource)
	}

	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// WithResource returns a copy of the error with the resource field set.
func (e S3Error) WithResource(resource string) S3Error {
	e.Resource = resource
	return e
}

// WithRequestID returns a copy of the error with the request ID field set.
func (e S3Error) WithRequestID(requestID string) S3Error {
	e.RequestID = requestID
	return e
}

// WithMessage returns a copy of the error with a custom message.
func (e S3Error) WithMessage(message string) S3Error {
	e.Message = message
	return e
}

// Is implements error matching for errors.Is().
func (e S3Error) Is(target error) bool {
	if t, ok := target.(S3Error); ok {
		return e.Code == t.Code
	}

	return false
}

// ErrorResponse represents the XML structure for S3 error responses.
type ErrorResponse struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource,omitempty"`
	RequestID string   `xml:"RequestId,omitempty"`
	HostID    string   `xml:"HostId,omitempty"`
}

// WriteS3Error writes an S3 error response to the HTTP response writer.
func WriteS3Error(w http.ResponseWriter, err S3Error) {
	response := ErrorResponse{
		Code:      err.Code,
		Message:   err.Message,
		Resource:  err.Resource,
		RequestID: err.RequestID,
	}

	w.Header().Set("Content-Type", "application/xml")

	if err.RequestID != "" {
		w.Header().Set("x-amz-request-id", err.RequestID)
	}

	w.WriteHeader(err.StatusCode)
	w.Write([]byte(xml.Header))
	xml.NewEncoder(w).Encode(response)
}

// WriteS3ErrorWithContext writes an S3 error response with resource and request ID from context.
func WriteS3ErrorWithContext(w http.ResponseWriter, err S3Error, resource, requestID string) {
	WriteS3Error(w, err.WithResource(resource).WithRequestID(requestID))
}

// Standard S3 Error Definitions
// Reference: https://docs.aws.amazon.com/AmazonS3/latest/API/ErrorResponses.html

// Access and Authentication Errors.
var (
	// ErrAccessDenied is returned when access to the resource is denied.
	ErrAccessDenied = S3Error{
		Code:       "AccessDenied",
		Message:    "Access Denied",
		StatusCode: http.StatusForbidden,
	}

	// ErrAccountProblem is returned when there is a problem with the AWS account.
	ErrAccountProblem = S3Error{
		Code:       "AccountProblem",
		Message:    "There is a problem with your AWS account that prevents the operation from completing successfully",
		StatusCode: http.StatusForbidden,
	}

	// ErrAllAccessDisabled is returned when all access to this resource has been disabled.
	ErrAllAccessDisabled = S3Error{
		Code:       "AllAccessDisabled",
		Message:    "All access to this Amazon S3 resource has been disabled",
		StatusCode: http.StatusForbidden,
	}

	// ErrInvalidAccessKeyId is returned when the AWS access key ID does not exist.
	ErrInvalidAccessKeyId = S3Error{
		Code:       "InvalidAccessKeyId",
		Message:    "The AWS Access Key Id you provided does not exist in our records",
		StatusCode: http.StatusForbidden,
	}

	// ErrSignatureDoesNotMatch is returned when the signature does not match.
	ErrSignatureDoesNotMatch = S3Error{
		Code:       "SignatureDoesNotMatch",
		Message:    "The request signature we calculated does not match the signature you provided",
		StatusCode: http.StatusForbidden,
	}

	// ErrRequestTimeTooSkewed is returned when the request time differs too much from the server time.
	ErrRequestTimeTooSkewed = S3Error{
		Code:       "RequestTimeTooSkewed",
		Message:    "The difference between the request time and the server's time is too large",
		StatusCode: http.StatusForbidden,
	}

	// ErrExpiredToken is returned when the provided token has expired.
	ErrExpiredToken = S3Error{
		Code:       "ExpiredToken",
		Message:    "The provided token has expired",
		StatusCode: http.StatusBadRequest,
	}

	// ErrInvalidToken is returned when the provided token is malformed or invalid.
	ErrInvalidToken = S3Error{
		Code:       "InvalidToken",
		Message:    "The provided token is malformed or otherwise invalid",
		StatusCode: http.StatusBadRequest,
	}

	// ErrTokenRefreshRequired is returned when the provided token must be refreshed.
	ErrTokenRefreshRequired = S3Error{
		Code:       "TokenRefreshRequired",
		Message:    "The provided token must be refreshed",
		StatusCode: http.StatusBadRequest,
	}
)

// Bucket Errors.
var (
	// ErrNoSuchBucket is returned when the specified bucket does not exist.
	ErrNoSuchBucket = S3Error{
		Code:       "NoSuchBucket",
		Message:    "The specified bucket does not exist",
		StatusCode: http.StatusNotFound,
	}

	// ErrBucketAlreadyExists is returned when the bucket name is already taken.
	ErrBucketAlreadyExists = S3Error{
		Code:       "BucketAlreadyExists",
		Message:    "The requested bucket name is not available. The bucket namespace is shared by all users of the system. Please select a different name and try again",
		StatusCode: http.StatusConflict,
	}

	// ErrBucketAlreadyOwnedByYou is returned when the bucket already exists and is owned by you.
	ErrBucketAlreadyOwnedByYou = S3Error{
		Code:       "BucketAlreadyOwnedByYou",
		Message:    "The bucket you tried to create already exists, and you own it",
		StatusCode: http.StatusConflict,
	}

	// ErrBucketNotEmpty is returned when the bucket is not empty.
	ErrBucketNotEmpty = S3Error{
		Code:       "BucketNotEmpty",
		Message:    "The bucket you tried to delete is not empty",
		StatusCode: http.StatusConflict,
	}

	// ErrInvalidBucketName is returned when the bucket name is invalid.
	ErrInvalidBucketName = S3Error{
		Code:       "InvalidBucketName",
		Message:    "The specified bucket is not valid",
		StatusCode: http.StatusBadRequest,
	}

	// ErrInvalidBucketState is returned when the bucket is not in a valid state.
	ErrInvalidBucketState = S3Error{
		Code:       "InvalidBucketState",
		Message:    "The request is not valid with the current state of the bucket",
		StatusCode: http.StatusConflict,
	}

	// ErrTooManyBuckets is returned when you have attempted to create more buckets than allowed.
	ErrTooManyBuckets = S3Error{
		Code:       "TooManyBuckets",
		Message:    "You have attempted to create more buckets than allowed",
		StatusCode: http.StatusBadRequest,
	}

	// ErrNoSuchBucketPolicy is returned when the specified bucket does not have a bucket policy.
	ErrNoSuchBucketPolicy = S3Error{
		Code:       "NoSuchBucketPolicy",
		Message:    "The specified bucket does not have a bucket policy",
		StatusCode: http.StatusNotFound,
	}

	// ErrNoSuchCORSConfiguration is returned when the CORS configuration does not exist.
	ErrNoSuchCORSConfiguration = S3Error{
		Code:       "NoSuchCORSConfiguration",
		Message:    "The CORS configuration does not exist",
		StatusCode: http.StatusNotFound,
	}

	// ErrNoSuchLifecycleConfiguration is returned when the lifecycle configuration does not exist.
	ErrNoSuchLifecycleConfiguration = S3Error{
		Code:       "NoSuchLifecycleConfiguration",
		Message:    "The lifecycle configuration does not exist",
		StatusCode: http.StatusNotFound,
	}

	// ErrNoSuchTagSet is returned when the TagSet does not exist.
	ErrNoSuchTagSet = S3Error{
		Code:       "NoSuchTagSet",
		Message:    "The TagSet does not exist",
		StatusCode: http.StatusNotFound,
	}

	// ErrNoSuchWebsiteConfiguration is returned when the website configuration does not exist.
	ErrNoSuchWebsiteConfiguration = S3Error{
		Code:       "NoSuchWebsiteConfiguration",
		Message:    "The specified bucket does not have a website configuration",
		StatusCode: http.StatusNotFound,
	}
)

// Object Errors.
var (
	// ErrNoSuchKey is returned when the specified key does not exist.
	ErrNoSuchKey = S3Error{
		Code:       "NoSuchKey",
		Message:    "The specified key does not exist",
		StatusCode: http.StatusNotFound,
	}

	// ErrNoSuchVersion is returned when the specified version does not exist.
	ErrNoSuchVersion = S3Error{
		Code:       "NoSuchVersion",
		Message:    "The version ID specified in the request does not match an existing version",
		StatusCode: http.StatusNotFound,
	}

	// ErrInvalidObjectState is returned when the operation is not valid for the current object state.
	ErrInvalidObjectState = S3Error{
		Code:       "InvalidObjectState",
		Message:    "The operation is not valid for the current state of the object",
		StatusCode: http.StatusForbidden,
	}

	// ErrObjectNotInActiveTierError is returned when the object is archived.
	ErrObjectNotInActiveTierError = S3Error{
		Code:       "ObjectNotInActiveTierError",
		Message:    "The source object of the COPY operation is not in the active tier and is only stored in Amazon Glacier",
		StatusCode: http.StatusForbidden,
	}

	// ErrDeleteMarkerVersionId is returned when a delete marker version ID was used.
	ErrDeleteMarkerVersionId = S3Error{
		Code:       "MethodNotAllowed",
		Message:    "The specified method is not allowed against this resource",
		StatusCode: http.StatusMethodNotAllowed,
	}
)

// Multipart Upload Errors.
var (
	// ErrNoSuchUpload is returned when the specified multipart upload does not exist.
	ErrNoSuchUpload = S3Error{
		Code:       "NoSuchUpload",
		Message:    "The specified upload does not exist. The upload ID may be invalid, or the upload may have been aborted or completed",
		StatusCode: http.StatusNotFound,
	}

	// ErrInvalidPart is returned when one or more of the specified parts could not be found.
	ErrInvalidPart = S3Error{
		Code:       "InvalidPart",
		Message:    "One or more of the specified parts could not be found. The part might not have been uploaded, or the specified entity tag might not have matched the part's entity tag",
		StatusCode: http.StatusBadRequest,
	}

	// ErrInvalidPartOrder is returned when the parts list is not in ascending order.
	ErrInvalidPartOrder = S3Error{
		Code:       "InvalidPartOrder",
		Message:    "The list of parts was not in ascending order. Parts must be ordered by part number",
		StatusCode: http.StatusBadRequest,
	}

	// ErrInvalidPartNumber is returned when the part number is not valid.
	ErrInvalidPartNumber = S3Error{
		Code:       "InvalidPartNumber",
		Message:    "The part number is not valid. Part number must be an integer between 1 and 10000, inclusive",
		StatusCode: http.StatusBadRequest,
	}

	// ErrEntityTooSmall is returned when the proposed upload is smaller than the minimum allowed.
	ErrEntityTooSmall = S3Error{
		Code:       "EntityTooSmall",
		Message:    "Your proposed upload is smaller than the minimum allowed object size",
		StatusCode: http.StatusBadRequest,
	}

	// ErrEntityTooLarge is returned when the proposed upload exceeds the maximum allowed.
	ErrEntityTooLarge = S3Error{
		Code:       "EntityTooLarge",
		Message:    "Your proposed upload exceeds the maximum allowed object size",
		StatusCode: http.StatusBadRequest,
	}
)

// Request Errors.
var (
	// ErrInvalidArgument is returned when an argument is invalid.
	ErrInvalidArgument = S3Error{
		Code:       "InvalidArgument",
		Message:    "Invalid Argument",
		StatusCode: http.StatusBadRequest,
	}

	// ErrInvalidRequest is returned when the request is not valid.
	ErrInvalidRequest = S3Error{
		Code:       "InvalidRequest",
		Message:    "Invalid Request",
		StatusCode: http.StatusBadRequest,
	}

	// ErrMalformedXML is returned when the XML provided was not well-formed.
	ErrMalformedXML = S3Error{
		Code:       "MalformedXML",
		Message:    "The XML you provided was not well-formed or did not validate against our published schema",
		StatusCode: http.StatusBadRequest,
	}

	// ErrMalformedACLError is returned when the ACL was not well-formed.
	ErrMalformedACLError = S3Error{
		Code:       "MalformedACLError",
		Message:    "The XML you provided was not well-formed or did not validate against our published schema",
		StatusCode: http.StatusBadRequest,
	}

	// ErrMalformedPolicy is returned when the policy was not well-formed.
	ErrMalformedPolicy = S3Error{
		Code:       "MalformedPolicy",
		Message:    "The policy document was not well-formed or did not validate against our published schema",
		StatusCode: http.StatusBadRequest,
	}

	// ErrMissingContentLength is returned when the Content-Length header is missing.
	ErrMissingContentLength = S3Error{
		Code:       "MissingContentLength",
		Message:    "You must provide the Content-Length HTTP header",
		StatusCode: http.StatusLengthRequired,
	}

	// ErrMissingRequestBodyError is returned when the request body is missing.
	ErrMissingRequestBodyError = S3Error{
		Code:       "MissingRequestBodyError",
		Message:    "Request body is empty",
		StatusCode: http.StatusBadRequest,
	}

	// ErrMissingSecurityHeader is returned when a required security header is missing.
	ErrMissingSecurityHeader = S3Error{
		Code:       "MissingSecurityHeader",
		Message:    "Your request is missing a required header",
		StatusCode: http.StatusBadRequest,
	}

	// ErrIncompleteBody is returned when the request body was not complete as expected.
	ErrIncompleteBody = S3Error{
		Code:       "IncompleteBody",
		Message:    "You did not provide the number of bytes specified by the Content-Length HTTP header",
		StatusCode: http.StatusBadRequest,
	}

	// ErrInvalidDigest is returned when the Content-MD5 is not valid.
	ErrInvalidDigest = S3Error{
		Code:       "InvalidDigest",
		Message:    "The Content-MD5 you specified is not valid",
		StatusCode: http.StatusBadRequest,
	}

	// ErrInvalidLocationConstraint is returned when the location constraint is not valid.
	ErrInvalidLocationConstraint = S3Error{
		Code:       "InvalidLocationConstraint",
		Message:    "The specified location constraint is not valid",
		StatusCode: http.StatusBadRequest,
	}

	// ErrInvalidRange is returned when the requested range is not satisfiable.
	ErrInvalidRange = S3Error{
		Code:       "InvalidRange",
		Message:    "The requested range is not satisfiable",
		StatusCode: http.StatusRequestedRangeNotSatisfiable,
	}

	// ErrInvalidStorageClass is returned when the storage class is not valid.
	ErrInvalidStorageClass = S3Error{
		Code:       "InvalidStorageClass",
		Message:    "The storage class you specified is not valid",
		StatusCode: http.StatusBadRequest,
	}

	// ErrInvalidTagError is returned when the tag is not valid.
	ErrInvalidTagError = S3Error{
		Code:       "InvalidTag",
		Message:    "The tag provided was not valid",
		StatusCode: http.StatusBadRequest,
	}

	// ErrInvalidTargetBucketForLogging is returned when the target bucket for logging is not valid.
	ErrInvalidTargetBucketForLogging = S3Error{
		Code:       "InvalidTargetBucketForLogging",
		Message:    "The target bucket for logging does not exist, is not owned by you, or does not have the appropriate grants for the log-delivery group",
		StatusCode: http.StatusBadRequest,
	}

	// ErrInvalidURI is returned when the URI could not be parsed.
	ErrInvalidURI = S3Error{
		Code:       "InvalidURI",
		Message:    "Could not parse the specified URI",
		StatusCode: http.StatusBadRequest,
	}

	// ErrKeyTooLongError is returned when the key is too long.
	ErrKeyTooLongError = S3Error{
		Code:       "KeyTooLongError",
		Message:    "Your key is too long",
		StatusCode: http.StatusBadRequest,
	}

	// ErrMetadataTooLarge is returned when the metadata headers exceed the maximum allowed metadata size.
	ErrMetadataTooLarge = S3Error{
		Code:       "MetadataTooLarge",
		Message:    "Your metadata headers exceed the maximum allowed metadata size",
		StatusCode: http.StatusBadRequest,
	}

	// ErrMaxMessageLengthExceeded is returned when the request is too large.
	ErrMaxMessageLengthExceeded = S3Error{
		Code:       "MaxMessageLengthExceeded",
		Message:    "Your request was too big",
		StatusCode: http.StatusBadRequest,
	}

	// ErrMaxPostPreDataLengthExceededError is returned when POST request fields preceding the upload file were too large.
	ErrMaxPostPreDataLengthExceededError = S3Error{
		Code:       "MaxPostPreDataLengthExceededError",
		Message:    "Your POST request fields preceding the upload file were too large",
		StatusCode: http.StatusBadRequest,
	}

	// ErrMethodNotAllowed is returned when the specified method is not allowed.
	ErrMethodNotAllowed = S3Error{
		Code:       "MethodNotAllowed",
		Message:    "The specified method is not allowed against this resource",
		StatusCode: http.StatusMethodNotAllowed,
	}

	// ErrNotImplemented is returned when a header or functionality is not implemented.
	ErrNotImplemented = S3Error{
		Code:       "NotImplemented",
		Message:    "A header you provided implies functionality that is not implemented",
		StatusCode: http.StatusNotImplemented,
	}

	// ErrPreconditionFailed is returned when a condition specified was not met.
	ErrPreconditionFailed = S3Error{
		Code:       "PreconditionFailed",
		Message:    "At least one of the preconditions you specified did not hold",
		StatusCode: http.StatusPreconditionFailed,
	}

	// ErrNotModified is returned when the object has not been modified.
	ErrNotModified = S3Error{
		Code:       "NotModified",
		Message:    "Not Modified",
		StatusCode: http.StatusNotModified,
	}

	// ErrRequestTimeout is returned when the socket connection was not read from.
	ErrRequestTimeout = S3Error{
		Code:       "RequestTimeout",
		Message:    "Your socket connection to the server was not read from or written to within the timeout period",
		StatusCode: http.StatusBadRequest,
	}

	// ErrUnexpectedContent is returned when the request does not support content.
	ErrUnexpectedContent = S3Error{
		Code:       "UnexpectedContent",
		Message:    "This request does not support content",
		StatusCode: http.StatusBadRequest,
	}

	// ErrUnresolvableGrantByEmailAddress is returned when the email address does not match any account.
	ErrUnresolvableGrantByEmailAddress = S3Error{
		Code:       "UnresolvableGrantByEmailAddress",
		Message:    "The email address you provided does not match any account on record",
		StatusCode: http.StatusBadRequest,
	}
)

// Server Errors.
var (
	// ErrInternalError is returned when an internal error occurred.
	ErrInternalError = S3Error{
		Code:       "InternalError",
		Message:    "We encountered an internal error. Please try again",
		StatusCode: http.StatusInternalServerError,
	}

	// ErrServiceUnavailable is returned when the service is not available.
	ErrServiceUnavailable = S3Error{
		Code:       "ServiceUnavailable",
		Message:    "Please reduce your request rate",
		StatusCode: http.StatusServiceUnavailable,
	}

	// ErrSlowDown is returned when the request rate is too high.
	ErrSlowDown = S3Error{
		Code:       "SlowDown",
		Message:    "Please reduce your request rate",
		StatusCode: http.StatusServiceUnavailable,
	}

	// ErrTemporaryRedirect is returned when a temporary redirect is needed.
	ErrTemporaryRedirect = S3Error{
		Code:       "TemporaryRedirect",
		Message:    "You are being redirected to the bucket while DNS updates",
		StatusCode: http.StatusTemporaryRedirect,
	}

	// ErrPermanentRedirect is returned when a permanent redirect is needed.
	ErrPermanentRedirect = S3Error{
		Code:       "PermanentRedirect",
		Message:    "The bucket you are attempting to access must be addressed using the specified endpoint",
		StatusCode: http.StatusMovedPermanently,
	}
)

// Copy Errors.
var (
	// ErrInvalidCopyDest is returned when the copy destination is not valid.
	ErrInvalidCopyDest = S3Error{
		Code:       "InvalidRequest",
		Message:    "This copy request is illegal because it is trying to copy an object to itself without changing the object's metadata, storage class, website redirect location or encryption attributes",
		StatusCode: http.StatusBadRequest,
	}

	// ErrInvalidCopySource is returned when the copy source is not valid.
	ErrInvalidCopySource = S3Error{
		Code:       "InvalidArgument",
		Message:    "Copy Source must mention the source bucket and key: sourcebucket/sourcekey",
		StatusCode: http.StatusBadRequest,
	}
)

// Replication Errors.
var (
	// ErrReplicationConfigurationNotFoundError is returned when the replication configuration was not found.
	ErrReplicationConfigurationNotFoundError = S3Error{
		Code:       "ReplicationConfigurationNotFoundError",
		Message:    "The replication configuration was not found",
		StatusCode: http.StatusNotFound,
	}
)

// Cross-Origin Errors.
var (
	// ErrCrossOriginAccessDenied is returned when cross-origin access is denied.
	ErrCrossOriginAccessDenied = S3Error{
		Code:       "AccessDenied",
		Message:    "CORSResponse: This CORS request is not allowed. This is usually because the evalution of Origin, request method / Access-Control-Request-Method or Access-Control-Request-Headers are not whitelisted",
		StatusCode: http.StatusForbidden,
	}
)

// Restore Errors.
var (
	// ErrRestoreAlreadyInProgress is returned when object restore is already in progress.
	ErrRestoreAlreadyInProgress = S3Error{
		Code:       "RestoreAlreadyInProgress",
		Message:    "Object restore is already in progress",
		StatusCode: http.StatusConflict,
	}
)

// Object Lock Errors.
var (
	// ErrObjectLockConfigurationNotFoundError is returned when the object lock configuration was not found.
	ErrObjectLockConfigurationNotFoundError = S3Error{
		Code:       "ObjectLockConfigurationNotFoundError",
		Message:    "Object Lock configuration does not exist for this bucket",
		StatusCode: http.StatusNotFound,
	}

	// ErrInvalidRetentionDate is returned when the retention date is not valid.
	ErrInvalidRetentionDate = S3Error{
		Code:       "InvalidRetentionDate",
		Message:    "Retention date must be greater than the current date",
		StatusCode: http.StatusBadRequest,
	}

	// ErrObjectLocked is returned when the object is locked.
	ErrObjectLocked = S3Error{
		Code:       "ObjectLocked",
		Message:    "Object is locked and cannot be overwritten or deleted",
		StatusCode: http.StatusForbidden,
	}

	// ErrNoSuchObjectLockConfiguration is returned when object lock configuration does not exist.
	ErrNoSuchObjectLockConfiguration = S3Error{
		Code:       "ObjectLockConfigurationNotFoundError",
		Message:    "Object Lock configuration does not exist for this bucket",
		StatusCode: http.StatusNotFound,
	}
)

// Encryption Errors.
var (
	// ErrServerSideEncryptionConfigurationNotFoundError is returned when the encryption configuration was not found.
	ErrServerSideEncryptionConfigurationNotFoundError = S3Error{
		Code:       "ServerSideEncryptionConfigurationNotFoundError",
		Message:    "The server side encryption configuration was not found",
		StatusCode: http.StatusNotFound,
	}
)

// Logging Errors.
var (
	// ErrNoLoggingConfiguration is returned when the logging configuration does not exist.
	ErrNoLoggingConfiguration = S3Error{
		Code:       "NoLoggingConfiguration",
		Message:    "The logging configuration does not exist",
		StatusCode: http.StatusNotFound,
	}
)

// Notification Errors.
var (
	// ErrNoSuchNotificationConfiguration is returned when the notification configuration does not exist.
	ErrNoSuchNotificationConfiguration = S3Error{
		Code:       "NoSuchNotificationConfiguration",
		Message:    "The notification configuration does not exist",
		StatusCode: http.StatusNotFound,
	}
)

// Public Access Block Errors.
var (
	// ErrNoSuchPublicAccessBlockConfiguration is returned when the public access block configuration does not exist.
	ErrNoSuchPublicAccessBlockConfiguration = S3Error{
		Code:       "NoSuchPublicAccessBlockConfiguration",
		Message:    "The public access block configuration does not exist",
		StatusCode: http.StatusNotFound,
	}
)

// Ownership Controls Errors.
var (
	// ErrOwnershipControlsNotFoundError is returned when ownership controls do not exist.
	ErrOwnershipControlsNotFoundError = S3Error{
		Code:       "OwnershipControlsNotFoundError",
		Message:    "The ownership controls do not exist for this bucket",
		StatusCode: http.StatusNotFound,
	}
)

// Analytics/Inventory/Metrics Errors.
var (
	// ErrNoSuchAnalyticsConfiguration is returned when the analytics configuration does not exist.
	ErrNoSuchAnalyticsConfiguration = S3Error{
		Code:       "NoSuchConfiguration",
		Message:    "The analytics configuration does not exist",
		StatusCode: http.StatusNotFound,
	}

	// ErrNoSuchInventoryConfiguration is returned when the inventory configuration does not exist.
	ErrNoSuchInventoryConfiguration = S3Error{
		Code:       "NoSuchConfiguration",
		Message:    "The inventory configuration does not exist",
		StatusCode: http.StatusNotFound,
	}

	// ErrNoSuchMetricsConfiguration is returned when the metrics configuration does not exist.
	ErrNoSuchMetricsConfiguration = S3Error{
		Code:       "NoSuchConfiguration",
		Message:    "The metrics configuration does not exist",
		StatusCode: http.StatusNotFound,
	}
)

// Intelligent Tiering Errors.
var (
	// ErrNoSuchIntelligentTieringConfiguration is returned when the intelligent tiering configuration does not exist.
	ErrNoSuchIntelligentTieringConfiguration = S3Error{
		Code:       "NoSuchConfiguration",
		Message:    "The intelligent tiering configuration does not exist",
		StatusCode: http.StatusNotFound,
	}
)

// Select Errors.
var (
	// ErrBusy is returned when the service is busy.
	ErrBusy = S3Error{
		Code:       "Busy",
		Message:    "The service is unable to process your request at this time",
		StatusCode: http.StatusServiceUnavailable,
	}

	// ErrExternalSelectFailure is returned when external select processing failed.
	ErrExternalSelectFailure = S3Error{
		Code:       "ExternalSelectFailure",
		Message:    "The external select processing failed",
		StatusCode: http.StatusBadRequest,
	}
)

// IsS3Error checks if an error is an S3Error with a specific code.
func IsS3Error(err error, code string) bool {
	if s3err, ok := err.(S3Error); ok {
		return s3err.Code == code
	}

	return false
}

// GetS3Error attempts to extract an S3Error from an error.
// If the error is not an S3Error, it returns ErrInternalError.
func GetS3Error(err error) S3Error {
	if s3err, ok := err.(S3Error); ok {
		return s3err
	}

	return ErrInternalError.WithMessage(err.Error())
}

// NewS3Error creates a custom S3Error with the specified parameters.
func NewS3Error(code, message string, statusCode int) S3Error {
	return S3Error{
		Code:       code,
		Message:    message,
		StatusCode: statusCode,
	}
}
