package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/piwi3910/nebulaio/internal/audit"
)

// Bucket operations

func (s *RaftStore) CreateBucket(ctx context.Context, bucket *Bucket) error {
	data, err := json.Marshal(bucket)
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdCreateBucket, Data: data})
}

func (s *RaftStore) GetBucket(ctx context.Context, name string) (*Bucket, error) {
	key := []byte(prefixBucket + name)
	data, err := s.get(key)
	if err == badger.ErrKeyNotFound {
		return nil, fmt.Errorf("bucket not found: %s", name)
	}
	if err != nil {
		return nil, err
	}

	var bucket Bucket
	if err := json.Unmarshal(data, &bucket); err != nil {
		return nil, err
	}
	return &bucket, nil
}

func (s *RaftStore) DeleteBucket(ctx context.Context, name string) error {
	data, err := json.Marshal(name)
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdDeleteBucket, Data: data})
}

func (s *RaftStore) ListBuckets(ctx context.Context, owner string) ([]*Bucket, error) {
	results, err := s.scan([]byte(prefixBucket))
	if err != nil {
		return nil, err
	}

	var buckets []*Bucket
	for _, data := range results {
		var bucket Bucket
		if err := json.Unmarshal(data, &bucket); err != nil {
			continue
		}
		// Filter by owner if specified
		if owner == "" || bucket.Owner == owner {
			buckets = append(buckets, &bucket)
		}
	}

	// Sort by name
	sort.Slice(buckets, func(i, j int) bool {
		return buckets[i].Name < buckets[j].Name
	})

	return buckets, nil
}

func (s *RaftStore) UpdateBucket(ctx context.Context, bucket *Bucket) error {
	data, err := json.Marshal(bucket)
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdUpdateBucket, Data: data})
}

// Object metadata operations

func (s *RaftStore) PutObjectMeta(ctx context.Context, meta *ObjectMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdPutObjectMeta, Data: data})
}

func (s *RaftStore) GetObjectMeta(ctx context.Context, bucket, key string) (*ObjectMeta, error) {
	dbKey := []byte(fmt.Sprintf("%s%s/%s", prefixObject, bucket, key))
	data, err := s.get(dbKey)
	if err == badger.ErrKeyNotFound {
		return nil, fmt.Errorf("object not found: %s/%s", bucket, key)
	}
	if err != nil {
		return nil, err
	}

	var meta ObjectMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (s *RaftStore) DeleteObjectMeta(ctx context.Context, bucket, key string) error {
	data, err := json.Marshal(struct {
		Bucket string `json:"bucket"`
		Key    string `json:"key"`
	}{Bucket: bucket, Key: key})
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdDeleteObjectMeta, Data: data})
}

func (s *RaftStore) ListObjects(ctx context.Context, bucket, prefix, delimiter string, maxKeys int, continuationToken string) (*ObjectListing, error) {
	dbPrefix := []byte(fmt.Sprintf("%s%s/", prefixObject, bucket))

	var objects []*ObjectMeta
	var commonPrefixes []string
	prefixSet := make(map[string]bool)

	err := s.badger.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = dbPrefix
		it := txn.NewIterator(opts)
		defer it.Close()

		startKey := dbPrefix
		if continuationToken != "" {
			startKey = []byte(fmt.Sprintf("%s%s/%s", prefixObject, bucket, continuationToken))
		}

		for it.Seek(startKey); it.Valid(); it.Next() {
			item := it.Item()
			key := string(item.Key())

			// Extract object key from DB key
			objKey := strings.TrimPrefix(key, string(dbPrefix))

			// Apply prefix filter
			if prefix != "" && !strings.HasPrefix(objKey, prefix) {
				continue
			}

			// Handle delimiter (for "folder" simulation)
			if delimiter != "" {
				afterPrefix := strings.TrimPrefix(objKey, prefix)
				if idx := strings.Index(afterPrefix, delimiter); idx >= 0 {
					// This is a "common prefix" (folder)
					commonPrefix := prefix + afterPrefix[:idx+1]
					if !prefixSet[commonPrefix] {
						prefixSet[commonPrefix] = true
						commonPrefixes = append(commonPrefixes, commonPrefix)
					}
					continue
				}
			}

			// Get object metadata
			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}

			var meta ObjectMeta
			if err := json.Unmarshal(val, &meta); err != nil {
				continue
			}

			// Skip delete markers unless specifically querying versions
			if meta.DeleteMarker {
				continue
			}

			objects = append(objects, &meta)

			if maxKeys > 0 && len(objects) >= maxKeys+1 {
				break
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Sort common prefixes
	sort.Strings(commonPrefixes)

	// Determine if truncated
	isTruncated := false
	nextToken := ""
	if maxKeys > 0 && len(objects) > maxKeys {
		isTruncated = true
		nextToken = objects[maxKeys].Key
		objects = objects[:maxKeys]
	}

	return &ObjectListing{
		Objects:               objects,
		CommonPrefixes:        commonPrefixes,
		IsTruncated:           isTruncated,
		NextContinuationToken: nextToken,
	}, nil
}

// Version operations

func (s *RaftStore) GetObjectVersion(ctx context.Context, bucket, key, versionID string) (*ObjectMeta, error) {
	// If no version ID specified or "null", get the current version
	if versionID == "" || versionID == "null" {
		return s.GetObjectMeta(ctx, bucket, key)
	}

	// Get specific version from version store
	dbKey := []byte(fmt.Sprintf("%s%s/%s#%s", prefixObjectVersion, bucket, key, versionID))
	data, err := s.get(dbKey)
	if err == badger.ErrKeyNotFound {
		return nil, fmt.Errorf("object version not found: %s/%s (version %s)", bucket, key, versionID)
	}
	if err != nil {
		return nil, err
	}

	var meta ObjectMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (s *RaftStore) ListObjectVersions(ctx context.Context, bucket, prefix, delimiter, keyMarker, versionIDMarker string, maxKeys int) (*VersionListing, error) {
	// Prefix for all versions in this bucket
	dbPrefix := []byte(fmt.Sprintf("%s%s/", prefixObjectVersion, bucket))

	var versions []*ObjectMeta
	var deleteMarkers []*ObjectMeta
	var commonPrefixes []string
	prefixSet := make(map[string]bool)

	err := s.badger.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = dbPrefix
		it := txn.NewIterator(opts)
		defer it.Close()

		// Determine start key
		startKey := dbPrefix
		if keyMarker != "" {
			if versionIDMarker != "" {
				startKey = []byte(fmt.Sprintf("%s%s/%s#%s", prefixObjectVersion, bucket, keyMarker, versionIDMarker))
			} else {
				startKey = []byte(fmt.Sprintf("%s%s/%s#", prefixObjectVersion, bucket, keyMarker))
			}
		}

		count := 0
		for it.Seek(startKey); it.Valid(); it.Next() {
			item := it.Item()
			key := string(item.Key())

			// Extract object key and version from DB key
			// Format: objver:{bucket}/{key}#{versionID}
			trimmed := strings.TrimPrefix(key, string(dbPrefix))
			hashIdx := strings.LastIndex(trimmed, "#")
			if hashIdx < 0 {
				continue
			}
			objKey := trimmed[:hashIdx]

			// Apply prefix filter
			if prefix != "" && !strings.HasPrefix(objKey, prefix) {
				continue
			}

			// Handle delimiter (for "folder" simulation)
			if delimiter != "" {
				afterPrefix := strings.TrimPrefix(objKey, prefix)
				if idx := strings.Index(afterPrefix, delimiter); idx >= 0 {
					// This is a "common prefix" (folder)
					commonPrefix := prefix + afterPrefix[:idx+1]
					if !prefixSet[commonPrefix] {
						prefixSet[commonPrefix] = true
						commonPrefixes = append(commonPrefixes, commonPrefix)
					}
					continue
				}
			}

			// Skip the marker entry itself
			if keyMarker != "" && objKey == keyMarker {
				// Get the version ID from this entry
				thisVersionID := trimmed[hashIdx+1:]
				if versionIDMarker != "" && thisVersionID <= versionIDMarker {
					continue
				}
			}

			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}

			var meta ObjectMeta
			if err := json.Unmarshal(val, &meta); err != nil {
				continue
			}

			if meta.DeleteMarker {
				deleteMarkers = append(deleteMarkers, &meta)
			} else {
				versions = append(versions, &meta)
			}

			count++
			if maxKeys > 0 && count >= maxKeys+1 {
				break
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Also include current versions from the main object store
	err = s.badger.View(func(txn *badger.Txn) error {
		objPrefix := []byte(fmt.Sprintf("%s%s/", prefixObject, bucket))
		opts := badger.DefaultIteratorOptions
		opts.Prefix = objPrefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := string(item.Key())
			objKey := strings.TrimPrefix(key, string(objPrefix))

			// Apply prefix filter
			if prefix != "" && !strings.HasPrefix(objKey, prefix) {
				continue
			}

			// Handle delimiter (for "folder" simulation)
			if delimiter != "" {
				afterPrefix := strings.TrimPrefix(objKey, prefix)
				if idx := strings.Index(afterPrefix, delimiter); idx >= 0 {
					// This is a "common prefix" (folder)
					commonPrefix := prefix + afterPrefix[:idx+1]
					if !prefixSet[commonPrefix] {
						prefixSet[commonPrefix] = true
						commonPrefixes = append(commonPrefixes, commonPrefix)
					}
					continue
				}
			}

			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}

			var meta ObjectMeta
			if err := json.Unmarshal(val, &meta); err != nil {
				continue
			}

			// Only add if it has no version ID (non-versioned object) or is a unique entry
			if meta.VersionID == "" || meta.VersionID == "null" {
				if meta.DeleteMarker {
					deleteMarkers = append(deleteMarkers, &meta)
				} else {
					versions = append(versions, &meta)
				}
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Sort common prefixes
	sort.Strings(commonPrefixes)

	// Sort versions by key then by version ID (descending for versions)
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].Key != versions[j].Key {
			return versions[i].Key < versions[j].Key
		}
		// Newer versions first (higher ULID = newer)
		return versions[i].VersionID > versions[j].VersionID
	})

	sort.Slice(deleteMarkers, func(i, j int) bool {
		if deleteMarkers[i].Key != deleteMarkers[j].Key {
			return deleteMarkers[i].Key < deleteMarkers[j].Key
		}
		return deleteMarkers[i].VersionID > deleteMarkers[j].VersionID
	})

	// Determine if truncated
	isTruncated := false
	nextKeyMarker := ""
	nextVersionIDMarker := ""

	totalItems := len(versions) + len(deleteMarkers)
	if maxKeys > 0 && totalItems > maxKeys {
		isTruncated = true
		// Truncate to maxKeys
		if len(versions) > maxKeys {
			nextKeyMarker = versions[maxKeys].Key
			nextVersionIDMarker = versions[maxKeys].VersionID
			versions = versions[:maxKeys]
		} else if len(deleteMarkers) > 0 {
			remaining := maxKeys - len(versions)
			if remaining < len(deleteMarkers) {
				nextKeyMarker = deleteMarkers[remaining].Key
				nextVersionIDMarker = deleteMarkers[remaining].VersionID
				deleteMarkers = deleteMarkers[:remaining]
			}
		}
	}

	return &VersionListing{
		Versions:            versions,
		DeleteMarkers:       deleteMarkers,
		CommonPrefixes:      commonPrefixes,
		IsTruncated:         isTruncated,
		NextKeyMarker:       nextKeyMarker,
		NextVersionIDMarker: nextVersionIDMarker,
	}, nil
}

func (s *RaftStore) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error {
	data, err := json.Marshal(struct {
		Bucket    string `json:"bucket"`
		Key       string `json:"key"`
		VersionID string `json:"version_id"`
	}{Bucket: bucket, Key: key, VersionID: versionID})
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdDeleteObjectVersion, Data: data})
}

func (s *RaftStore) PutObjectMetaVersioned(ctx context.Context, meta *ObjectMeta, preserveOldVersions bool) error {
	data, err := json.Marshal(struct {
		Meta                *ObjectMeta `json:"meta"`
		PreserveOldVersions bool        `json:"preserve_old_versions"`
	}{Meta: meta, PreserveOldVersions: preserveOldVersions})
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdPutObjectMetaVersioned, Data: data})
}

// Multipart upload operations

func (s *RaftStore) CreateMultipartUpload(ctx context.Context, upload *MultipartUpload) error {
	data, err := json.Marshal(upload)
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdCreateMultipartUpload, Data: data})
}

func (s *RaftStore) GetMultipartUpload(ctx context.Context, bucket, key, uploadID string) (*MultipartUpload, error) {
	dbKey := []byte(fmt.Sprintf("%s%s/%s/%s", prefixMultipart, bucket, key, uploadID))
	data, err := s.get(dbKey)
	if err == badger.ErrKeyNotFound {
		return nil, fmt.Errorf("multipart upload not found: %s", uploadID)
	}
	if err != nil {
		return nil, err
	}

	var upload MultipartUpload
	if err := json.Unmarshal(data, &upload); err != nil {
		return nil, err
	}
	return &upload, nil
}

func (s *RaftStore) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	data, err := json.Marshal(struct {
		Bucket   string `json:"bucket"`
		Key      string `json:"key"`
		UploadID string `json:"upload_id"`
	}{Bucket: bucket, Key: key, UploadID: uploadID})
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdAbortMultipartUpload, Data: data})
}

func (s *RaftStore) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	data, err := json.Marshal(struct {
		Bucket   string `json:"bucket"`
		Key      string `json:"key"`
		UploadID string `json:"upload_id"`
	}{Bucket: bucket, Key: key, UploadID: uploadID})
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdCompleteMultipartUpload, Data: data})
}

func (s *RaftStore) AddUploadPart(ctx context.Context, bucket, key, uploadID string, part *UploadPart) error {
	data, err := json.Marshal(struct {
		Bucket   string     `json:"bucket"`
		Key      string     `json:"key"`
		UploadID string     `json:"upload_id"`
		Part     UploadPart `json:"part"`
	}{Bucket: bucket, Key: key, UploadID: uploadID, Part: *part})
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdAddUploadPart, Data: data})
}

func (s *RaftStore) ListMultipartUploads(ctx context.Context, bucket string) ([]*MultipartUpload, error) {
	prefix := []byte(fmt.Sprintf("%s%s/", prefixMultipart, bucket))
	results, err := s.scan(prefix)
	if err != nil {
		return nil, err
	}

	var uploads []*MultipartUpload
	for _, data := range results {
		var upload MultipartUpload
		if err := json.Unmarshal(data, &upload); err != nil {
			continue
		}
		uploads = append(uploads, &upload)
	}
	return uploads, nil
}

// User operations

func (s *RaftStore) CreateUser(ctx context.Context, user *User) error {
	data, err := json.Marshal(user)
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdCreateUser, Data: data})
}

func (s *RaftStore) GetUser(ctx context.Context, id string) (*User, error) {
	key := []byte(prefixUser + id)
	data, err := s.get(key)
	if err == badger.ErrKeyNotFound {
		return nil, fmt.Errorf("user not found: %s", id)
	}
	if err != nil {
		return nil, err
	}

	var user User
	if err := json.Unmarshal(data, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *RaftStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	// Get user ID from username mapping
	usernameKey := []byte(prefixUsername + username)
	userID, err := s.get(usernameKey)
	if err == badger.ErrKeyNotFound {
		return nil, fmt.Errorf("user not found: %s", username)
	}
	if err != nil {
		return nil, err
	}

	return s.GetUser(ctx, string(userID))
}

func (s *RaftStore) UpdateUser(ctx context.Context, user *User) error {
	data, err := json.Marshal(user)
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdUpdateUser, Data: data})
}

func (s *RaftStore) DeleteUser(ctx context.Context, id string) error {
	data, err := json.Marshal(id)
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdDeleteUser, Data: data})
}

func (s *RaftStore) ListUsers(ctx context.Context) ([]*User, error) {
	results, err := s.scan([]byte(prefixUser))
	if err != nil {
		return nil, err
	}

	var users []*User
	for _, data := range results {
		var user User
		if err := json.Unmarshal(data, &user); err != nil {
			continue
		}
		users = append(users, &user)
	}

	sort.Slice(users, func(i, j int) bool {
		return users[i].Username < users[j].Username
	})

	return users, nil
}

// Access key operations

func (s *RaftStore) CreateAccessKey(ctx context.Context, key *AccessKey) error {
	data, err := json.Marshal(key)
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdCreateAccessKey, Data: data})
}

func (s *RaftStore) GetAccessKey(ctx context.Context, accessKeyID string) (*AccessKey, error) {
	key := []byte(prefixAccessKey + accessKeyID)
	data, err := s.get(key)
	if err == badger.ErrKeyNotFound {
		return nil, fmt.Errorf("access key not found: %s", accessKeyID)
	}
	if err != nil {
		return nil, err
	}

	var accessKey AccessKey
	if err := json.Unmarshal(data, &accessKey); err != nil {
		return nil, err
	}
	return &accessKey, nil
}

func (s *RaftStore) DeleteAccessKey(ctx context.Context, accessKeyID string) error {
	data, err := json.Marshal(accessKeyID)
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdDeleteAccessKey, Data: data})
}

func (s *RaftStore) ListAccessKeys(ctx context.Context, userID string) ([]*AccessKey, error) {
	results, err := s.scan([]byte(prefixAccessKey))
	if err != nil {
		return nil, err
	}

	var keys []*AccessKey
	for _, data := range results {
		var key AccessKey
		if err := json.Unmarshal(data, &key); err != nil {
			continue
		}
		if key.UserID == userID {
			keys = append(keys, &key)
		}
	}
	return keys, nil
}

// Policy operations

func (s *RaftStore) CreatePolicy(ctx context.Context, policy *Policy) error {
	data, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdCreatePolicy, Data: data})
}

func (s *RaftStore) GetPolicy(ctx context.Context, name string) (*Policy, error) {
	key := []byte(prefixPolicy + name)
	data, err := s.get(key)
	if err == badger.ErrKeyNotFound {
		return nil, fmt.Errorf("policy not found: %s", name)
	}
	if err != nil {
		return nil, err
	}

	var policy Policy
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, err
	}
	return &policy, nil
}

func (s *RaftStore) UpdatePolicy(ctx context.Context, policy *Policy) error {
	data, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdUpdatePolicy, Data: data})
}

func (s *RaftStore) DeletePolicy(ctx context.Context, name string) error {
	data, err := json.Marshal(name)
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdDeletePolicy, Data: data})
}

func (s *RaftStore) ListPolicies(ctx context.Context) ([]*Policy, error) {
	results, err := s.scan([]byte(prefixPolicy))
	if err != nil {
		return nil, err
	}

	var policies []*Policy
	for _, data := range results {
		var policy Policy
		if err := json.Unmarshal(data, &policy); err != nil {
			continue
		}
		policies = append(policies, &policy)
	}

	sort.Slice(policies, func(i, j int) bool {
		return policies[i].Name < policies[j].Name
	})

	return policies, nil
}

// Cluster operations

func (s *RaftStore) GetClusterInfo(ctx context.Context) (*ClusterInfo, error) {
	nodes, err := s.ListNodes(ctx)
	if err != nil {
		return nil, err
	}

	return &ClusterInfo{
		ClusterID:     s.config.NodeID, // Use node ID as cluster ID for now
		LeaderID:      string(s.raft.Leader()),
		LeaderAddress: s.LeaderAddress(),
		Nodes:         nodes,
		RaftState:     s.raft.State().String(),
	}, nil
}

func (s *RaftStore) AddNode(ctx context.Context, node *NodeInfo) error {
	data, err := json.Marshal(node)
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdAddNode, Data: data})
}

func (s *RaftStore) RemoveNode(ctx context.Context, nodeID string) error {
	data, err := json.Marshal(nodeID)
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdRemoveNode, Data: data})
}

func (s *RaftStore) ListNodes(ctx context.Context) ([]*NodeInfo, error) {
	results, err := s.scan([]byte(prefixNode))
	if err != nil {
		return nil, err
	}

	var nodes []*NodeInfo
	for _, data := range results {
		var node NodeInfo
		if err := json.Unmarshal(data, &node); err != nil {
			continue
		}
		nodes = append(nodes, &node)
	}
	return nodes, nil
}

// Audit operations

// StoreAuditEvent stores an audit event
func (s *RaftStore) StoreAuditEvent(ctx context.Context, event *audit.AuditEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return s.apply(&command{Type: cmdStoreAuditEvent, Data: data})
}

// ListAuditEvents lists audit events with filtering
func (s *RaftStore) ListAuditEvents(ctx context.Context, filter audit.AuditFilter) (*audit.AuditListResult, error) {
	prefix := []byte(prefixAudit)

	var events []audit.AuditEvent
	var nextToken string

	err := s.badger.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Reverse = true // Most recent first
		it := txn.NewIterator(opts)
		defer it.Close()

		maxResults := filter.MaxResults
		if maxResults <= 0 {
			maxResults = 100 // Default limit
		}
		if maxResults > 1000 {
			maxResults = 1000 // Hard limit
		}

		// Determine seek position
		var seekKey []byte
		if filter.NextToken != "" {
			seekKey = []byte(filter.NextToken)
		} else if !filter.EndTime.IsZero() {
			// Create a key that will be after any events at EndTime
			seekKey = []byte(fmt.Sprintf("%s%s", prefixAudit, filter.EndTime.Format(time.RFC3339Nano)+"~"))
		} else {
			// Seek to the end to iterate in reverse
			seekKey = append(prefix, 0xFF)
		}

		count := 0
		for it.Seek(seekKey); it.Valid(); it.Next() {
			item := it.Item()
			key := item.Key()

			// Check prefix
			if !strings.HasPrefix(string(key), prefixAudit) {
				break
			}

			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}

			var event audit.AuditEvent
			if err := json.Unmarshal(val, &event); err != nil {
				continue
			}

			// Apply filters
			if !filter.StartTime.IsZero() && event.Timestamp.Before(filter.StartTime) {
				break // Events are ordered by time, so we can stop here
			}
			if !filter.EndTime.IsZero() && event.Timestamp.After(filter.EndTime) {
				continue
			}
			if filter.Bucket != "" && event.Resource.Bucket != filter.Bucket {
				continue
			}
			if filter.User != "" && event.UserIdentity.Username != filter.User && event.UserIdentity.UserID != filter.User {
				continue
			}
			if filter.EventType != "" && !strings.HasPrefix(string(event.EventType), filter.EventType) {
				continue
			}
			if filter.Result != "" && string(event.Result) != filter.Result {
				continue
			}

			events = append(events, event)
			count++

			// Check if we've collected enough
			if count >= maxResults+1 {
				break
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Handle pagination
	maxResults := filter.MaxResults
	if maxResults <= 0 {
		maxResults = 100
	}
	if len(events) > maxResults {
		nextToken = fmt.Sprintf("%s%s", prefixAudit, events[maxResults].Timestamp.Format(time.RFC3339Nano))
		events = events[:maxResults]
	}

	return &audit.AuditListResult{
		Events:    events,
		NextToken: nextToken,
	}, nil
}

// DeleteOldAuditEvents deletes audit events older than the specified time
func (s *RaftStore) DeleteOldAuditEvents(ctx context.Context, before time.Time) (int, error) {
	// First, collect the keys to delete
	var keysToDelete []string

	err := s.badger.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte(prefixAudit)
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			val, err := item.ValueCopy(nil)
			if err != nil {
				continue
			}

			var event audit.AuditEvent
			if err := json.Unmarshal(val, &event); err != nil {
				continue
			}

			if event.Timestamp.Before(before) {
				keysToDelete = append(keysToDelete, event.ID)
			}
		}
		return nil
	})

	if err != nil {
		return 0, err
	}

	// Delete the collected events through Raft
	deleted := 0
	for _, id := range keysToDelete {
		data, err := json.Marshal(id)
		if err != nil {
			continue
		}
		if err := s.apply(&command{Type: cmdDeleteAuditEvent, Data: data}); err != nil {
			continue
		}
		deleted++
	}

	return deleted, nil
}
