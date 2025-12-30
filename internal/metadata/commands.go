package metadata

import "encoding/json"

// commandType represents the type of Raft command.
type commandType string

const (
	cmdCreateBucket            commandType = "create_bucket"
	cmdDeleteBucket            commandType = "delete_bucket"
	cmdUpdateBucket            commandType = "update_bucket"
	cmdPutObjectMeta           commandType = "put_object_meta"
	cmdDeleteObjectMeta        commandType = "delete_object_meta"
	cmdPutObjectMetaVersioned  commandType = "put_object_meta_versioned"
	cmdDeleteObjectVersion     commandType = "delete_object_version"
	cmdCreateMultipartUpload   commandType = "create_multipart_upload"
	cmdAbortMultipartUpload    commandType = "abort_multipart_upload"
	cmdCompleteMultipartUpload commandType = "complete_multipart_upload"
	cmdAddUploadPart           commandType = "add_upload_part"
	cmdCreateUser              commandType = "create_user"
	cmdUpdateUser              commandType = "update_user"
	cmdDeleteUser              commandType = "delete_user"
	cmdCreateAccessKey         commandType = "create_access_key"
	cmdDeleteAccessKey         commandType = "delete_access_key"
	cmdCreatePolicy            commandType = "create_policy"
	cmdUpdatePolicy            commandType = "update_policy"
	cmdDeletePolicy            commandType = "delete_policy"
	cmdAddNode                 commandType = "add_node"
	cmdRemoveNode              commandType = "remove_node"
	cmdStoreAuditEvent         commandType = "store_audit_event"
	cmdDeleteAuditEvent        commandType = "delete_audit_event"
)

// command represents a Raft command to be applied to the state machine.
type command struct {
	Type commandType     `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Key prefixes for BadgerDB storage.
const (
	prefixBucket        = "bucket:"
	prefixObject        = "object:"
	prefixObjectVersion = "objver:"
	prefixMultipart     = "multipart:"
	prefixUser          = "user:"
	prefixUsername      = "username:"
	prefixAccessKey     = "accesskey:"
	prefixPolicy        = "policy:"
	prefixNode          = "node:"
	prefixAudit         = "audit:"
)
