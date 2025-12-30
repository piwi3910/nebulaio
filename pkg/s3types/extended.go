package s3types

import "encoding/xml"

// GetObjectAttributesOutput is the response for GetObjectAttributes.
type GetObjectAttributesOutput struct {
	XMLName      xml.Name     `xml:"GetObjectAttributesResponse"`
	ETag         string       `xml:"ETag,omitempty"`
	Checksum     *Checksum    `xml:"Checksum,omitempty"`
	ObjectParts  *ObjectParts `xml:"ObjectParts,omitempty"`
	StorageClass string       `xml:"StorageClass,omitempty"`
	ObjectSize   int64        `xml:"ObjectSize,omitempty"`
}

// Checksum contains checksum values for an object.
type Checksum struct {
	ChecksumCRC32  string `xml:"ChecksumCRC32,omitempty"`
	ChecksumCRC32C string `xml:"ChecksumCRC32C,omitempty"`
	ChecksumSHA1   string `xml:"ChecksumSHA1,omitempty"`
	ChecksumSHA256 string `xml:"ChecksumSHA256,omitempty"`
}

// ObjectParts contains information about object parts.
type ObjectParts struct {
	NextPartNumberMarker string `xml:"NextPartNumberMarker,omitempty"`
	PartNumberMarker     string `xml:"PartNumberMarker,omitempty"`
	Parts                []Part `xml:"Part,omitempty"`
	MaxParts             int    `xml:"MaxParts"`
	PartsCount           int    `xml:"PartsCount"`
	TotalPartsCount      int    `xml:"TotalPartsCount"`
	IsTruncated          bool   `xml:"IsTruncated"`
}

// Part represents a single part in a multipart object.
type Part struct {
	Checksum   *Checksum `xml:"Checksum,omitempty"`
	PartNumber int       `xml:"PartNumber"`
	Size       int64     `xml:"Size"`
}

// SelectObjectContentInput is the request for SelectObjectContent.
type SelectObjectContentInput struct {
	InputSerialization  InputSerialization  `xml:"InputSerialization"`
	OutputSerialization OutputSerialization `xml:"OutputSerialization"`
	RequestProgress     *RequestProgress    `xml:"RequestProgress,omitempty"`
	ScanRange           *ScanRange          `xml:"ScanRange,omitempty"`
	XMLName             xml.Name            `xml:"SelectObjectContentRequest"`
	Expression          string              `xml:"Expression"`
	ExpressionType      string              `xml:"ExpressionType"`
}

// InputSerialization defines the format of the input data.
type InputSerialization struct {
	CSV             *CSVInput     `xml:"CSV,omitempty"`
	JSON            *JSONInput    `xml:"JSON,omitempty"`
	Parquet         *ParquetInput `xml:"Parquet,omitempty"`
	CompressionType string        `xml:"CompressionType,omitempty"`
}

// CSVInput defines CSV input format.
type CSVInput struct {
	Comments                   string `xml:"Comments,omitempty"`
	FieldDelimiter             string `xml:"FieldDelimiter,omitempty"`
	FileHeaderInfo             string `xml:"FileHeaderInfo,omitempty"`
	QuoteCharacter             string `xml:"QuoteCharacter,omitempty"`
	QuoteEscapeCharacter       string `xml:"QuoteEscapeCharacter,omitempty"`
	RecordDelimiter            string `xml:"RecordDelimiter,omitempty"`
	AllowQuotedRecordDelimiter bool   `xml:"AllowQuotedRecordDelimiter,omitempty"`
}

// JSONInput defines JSON input format.
type JSONInput struct {
	Type string `xml:"Type,omitempty"` // DOCUMENT or LINES
}

// ParquetInput defines Parquet input format (empty struct).
type ParquetInput struct{}

// OutputSerialization defines the format of the output data.
type OutputSerialization struct {
	CSV  *CSVOutput  `xml:"CSV,omitempty"`
	JSON *JSONOutput `xml:"JSON,omitempty"`
}

// CSVOutput defines CSV output format.
type CSVOutput struct {
	FieldDelimiter       string `xml:"FieldDelimiter,omitempty"`
	QuoteCharacter       string `xml:"QuoteCharacter,omitempty"`
	QuoteEscapeCharacter string `xml:"QuoteEscapeCharacter,omitempty"`
	QuoteFields          string `xml:"QuoteFields,omitempty"`
	RecordDelimiter      string `xml:"RecordDelimiter,omitempty"`
}

// JSONOutput defines JSON output format.
type JSONOutput struct {
	RecordDelimiter string `xml:"RecordDelimiter,omitempty"`
}

// RequestProgress enables progress tracking.
type RequestProgress struct {
	Enabled bool `xml:"Enabled"`
}

// ScanRange specifies a range to scan.
type ScanRange struct {
	Start int64 `xml:"Start,omitempty"`
	End   int64 `xml:"End,omitempty"`
}

// RestoreRequest is the request for RestoreObject.
type RestoreRequest struct {
	SelectParameters     *SelectParameters    `xml:"SelectParameters,omitempty"`
	OutputLocation       *OutputLocation      `xml:"OutputLocation,omitempty"`
	XMLName              xml.Name             `xml:"RestoreRequest"`
	GlacierJobParameters GlacierJobParameters `xml:"GlacierJobParameters,omitempty"`
	Type                 string               `xml:"Type,omitempty"`
	Tier                 string               `xml:"Tier,omitempty"`
	Description          string               `xml:"Description,omitempty"`
	Days                 int                  `xml:"Days,omitempty"`
}

// GlacierJobParameters specifies Glacier restore parameters.
type GlacierJobParameters struct {
	Tier string `xml:"Tier"` // Standard, Bulk, or Expedited
}

// SelectParameters for restore select operations.
type SelectParameters struct {
	InputSerialization  InputSerialization  `xml:"InputSerialization"`
	OutputSerialization OutputSerialization `xml:"OutputSerialization"`
	ExpressionType      string              `xml:"ExpressionType"`
	Expression          string              `xml:"Expression"`
}

// OutputLocation for restore output.
type OutputLocation struct {
	S3 *S3OutputLocation `xml:"S3,omitempty"`
}

// S3OutputLocation for S3 restore output.
type S3OutputLocation struct {
	Encryption        *S3Encryption      `xml:"Encryption,omitempty"`
	AccessControlList *AccessControlList `xml:"AccessControlList,omitempty"`
	Tagging           *TagSet            `xml:"Tagging,omitempty"`
	BucketName        string             `xml:"BucketName"`
	Prefix            string             `xml:"Prefix"`
	CannedACL         string             `xml:"CannedACL,omitempty"`
	StorageClass      string             `xml:"StorageClass,omitempty"`
	UserMetadata      []UserMetadata     `xml:"UserMetadata>MetadataEntry,omitempty"`
}

// S3Encryption for output encryption.
type S3Encryption struct {
	EncryptionType string `xml:"EncryptionType"`
	KMSKeyId       string `xml:"KMSKeyId,omitempty"`
	KMSContext     string `xml:"KMSContext,omitempty"`
}

// UserMetadata for custom metadata.
type UserMetadata struct {
	Name  string `xml:"Name"`
	Value string `xml:"Value"`
}

// IntelligentTieringConfiguration for intelligent tiering.
type IntelligentTieringConfiguration struct {
	XMLName  xml.Name                  `xml:"IntelligentTieringConfiguration"`
	ID       string                    `xml:"Id"`
	Status   string                    `xml:"Status"`
	Filter   *IntelligentTieringFilter `xml:"Filter,omitempty"`
	Tierings []Tiering                 `xml:"Tiering"`
}

// IntelligentTieringFilter defines which objects the config applies to.
type IntelligentTieringFilter struct {
	Tag    *Tag                         `xml:"Tag,omitempty"`
	And    *IntelligentTieringFilterAnd `xml:"And,omitempty"`
	Prefix string                       `xml:"Prefix,omitempty"`
}

// IntelligentTieringFilterAnd combines multiple filter conditions.
type IntelligentTieringFilterAnd struct {
	Prefix string `xml:"Prefix,omitempty"`
	Tags   []Tag  `xml:"Tag,omitempty"`
}

// Tiering defines a tiering level.
type Tiering struct {
	AccessTier string `xml:"AccessTier"` // ARCHIVE_ACCESS, DEEP_ARCHIVE_ACCESS
	Days       int    `xml:"Days"`
}

// ListBucketIntelligentTieringConfigurationsOutput lists tiering configs.
type ListBucketIntelligentTieringConfigurationsOutput struct {
	XMLName                         xml.Name                          `xml:"ListBucketIntelligentTieringConfigurationsResult"`
	ContinuationToken               string                            `xml:"ContinuationToken,omitempty"`
	NextContinuationToken           string                            `xml:"NextContinuationToken,omitempty"`
	IntelligentTieringConfiguration []IntelligentTieringConfiguration `xml:"IntelligentTieringConfiguration,omitempty"`
	IsTruncated                     bool                              `xml:"IsTruncated"`
}

// WriteGetObjectResponseInput for Object Lambda.
type WriteGetObjectResponseInput struct {
	RequestRoute  string
	RequestToken  string
	ContentType   string
	ETag          string
	LastModified  string
	Body          []byte
	StatusCode    int
	ContentLength int64
}
