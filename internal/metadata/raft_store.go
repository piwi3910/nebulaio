package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/dgraph-io/badger/v4"
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
	// For now, just get the current version
	// TODO: Implement version storage
	return s.GetObjectMeta(ctx, bucket, key)
}

func (s *RaftStore) ListObjectVersions(ctx context.Context, bucket, prefix string, maxKeys int) ([]*ObjectMeta, error) {
	// TODO: Implement version listing
	listing, err := s.ListObjects(ctx, bucket, prefix, "", maxKeys, "")
	if err != nil {
		return nil, err
	}
	return listing.Objects, nil
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
