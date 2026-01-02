package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/piwi3910/nebulaio/internal/audit"
)

// Bucket operations

func (s *DragonboatStore) CreateBucket(ctx context.Context, bucket *Bucket) error {
	data, err := json.Marshal(bucket)
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdCreateBucket, Data: data})
}

func (s *DragonboatStore) GetBucket(ctx context.Context, name string) (*Bucket, error) {
	key := []byte(prefixBucket + name)

	data, err := s.get(key)
	if err == badger.ErrKeyNotFound {
		return nil, fmt.Errorf("bucket not found: %s", name)
	}

	if err != nil {
		return nil, err
	}

	var bucket Bucket
	err = json.Unmarshal(data, &bucket)
	if err != nil {
		return nil, err
	}

	return &bucket, nil
}

func (s *DragonboatStore) DeleteBucket(ctx context.Context, name string) error {
	data, err := json.Marshal(name)
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdDeleteBucket, Data: data})
}

func (s *DragonboatStore) ListBuckets(ctx context.Context, owner string) ([]*Bucket, error) {
	results, err := s.scan([]byte(prefixBucket))
	if err != nil {
		return nil, err
	}

	var buckets []*Bucket
	for _, data := range results {
		var bucket Bucket

		err := json.Unmarshal(data, &bucket)
		if err != nil {
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

func (s *DragonboatStore) UpdateBucket(ctx context.Context, bucket *Bucket) error {
	data, err := json.Marshal(bucket)
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdUpdateBucket, Data: data})
}

// Object metadata operations

func (s *DragonboatStore) PutObjectMeta(ctx context.Context, meta *ObjectMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdPutObjectMeta, Data: data})
}

func (s *DragonboatStore) GetObjectMeta(ctx context.Context, bucket, key string) (*ObjectMeta, error) {
	dbKey := fmt.Appendf(nil, "%s%s/%s", prefixObject, bucket, key)

	data, err := s.get(dbKey)
	if err == badger.ErrKeyNotFound {
		return nil, fmt.Errorf("object not found: %s/%s", bucket, key)
	}

	if err != nil {
		return nil, err
	}

	var meta ObjectMeta
	err = json.Unmarshal(data, &meta)
	if err != nil {
		return nil, err
	}

	return &meta, nil
}

func (s *DragonboatStore) DeleteObjectMeta(ctx context.Context, bucket, key string) error {
	data, err := json.Marshal(struct {
		Bucket string `json:"bucket"`
		Key    string `json:"key"`
	}{Bucket: bucket, Key: key})
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdDeleteObjectMeta, Data: data})
}

func (s *DragonboatStore) ListObjects(ctx context.Context, bucket, prefix, delimiter string, maxKeys int, continuationToken string) (*ObjectListing, error) {
	dbPrefix := fmt.Appendf(nil, "%s%s/", prefixObject, bucket)

	var (
		objects        []*ObjectMeta
		commonPrefixes []string
	)

	prefixSet := make(map[string]bool)

	err := s.iterateObjectsForListing(bucket, dbPrefix, prefix, delimiter, continuationToken, maxKeys, &objects, &commonPrefixes, prefixSet)
	if err != nil {
		return nil, err
	}

	sort.Strings(commonPrefixes)

	return s.buildObjectListing(objects, commonPrefixes, maxKeys), nil
}

func (s *DragonboatStore) iterateObjectsForListing(bucket string, dbPrefix []byte, prefix, delimiter, continuationToken string, maxKeys int, objects *[]*ObjectMeta, commonPrefixes *[]string, prefixSet map[string]bool) error {
	return s.badger.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = dbPrefix

		it := txn.NewIterator(opts)
		defer it.Close()

		startKey := s.getStartKey(bucket, dbPrefix, continuationToken)

		for it.Seek(startKey); it.Valid(); it.Next() {
			if s.processListObjectItem(it.Item(), string(dbPrefix), prefix, delimiter, maxKeys, objects, commonPrefixes, prefixSet) {
				break
			}
		}

		return nil
	})
}

func (s *DragonboatStore) getStartKey(bucket string, dbPrefix []byte, continuationToken string) []byte {
	if continuationToken != "" {
		return fmt.Appendf(nil, "%s%s/%s", prefixObject, bucket, continuationToken)
	}
	return dbPrefix
}

func (s *DragonboatStore) processListObjectItem(item *badger.Item, dbPrefix, prefix, delimiter string, maxKeys int, objects *[]*ObjectMeta, commonPrefixes *[]string, prefixSet map[string]bool) bool {
	key := string(item.Key())
	objKey := strings.TrimPrefix(key, dbPrefix)

	// Apply prefix filter
	if prefix != "" && !strings.HasPrefix(objKey, prefix) {
		return false
	}

	// Handle delimiter (for "folder" simulation)
	if s.handleCommonPrefix(objKey, prefix, delimiter, commonPrefixes, prefixSet) {
		return false
	}

	// Get and add object metadata
	if s.addObjectToListing(item, objects) {
		// Check if we've hit max keys
		return maxKeys > 0 && len(*objects) >= maxKeys+1
	}

	return false
}

func (s *DragonboatStore) handleCommonPrefix(objKey, prefix, delimiter string, commonPrefixes *[]string, prefixSet map[string]bool) bool {
	if delimiter == "" {
		return false
	}

	afterPrefix := strings.TrimPrefix(objKey, prefix)
	idx := strings.Index(afterPrefix, delimiter)
	if idx < 0 {
		return false
	}

	// This is a "common prefix" (folder)
	commonPrefix := prefix + afterPrefix[:idx+1]
	if !prefixSet[commonPrefix] {
		prefixSet[commonPrefix] = true
		*commonPrefixes = append(*commonPrefixes, commonPrefix)
	}

	return true
}

func (s *DragonboatStore) addObjectToListing(item *badger.Item, objects *[]*ObjectMeta) bool {
	val, err := item.ValueCopy(nil)
	if err != nil {
		return false
	}

	var meta ObjectMeta
	err = json.Unmarshal(val, &meta)
	if err != nil {
		return false
	}

	// Skip delete markers unless specifically querying versions
	if meta.DeleteMarker {
		return false
	}

	*objects = append(*objects, &meta)
	return true
}

func (s *DragonboatStore) buildObjectListing(objects []*ObjectMeta, commonPrefixes []string, maxKeys int) *ObjectListing {
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
	}
}

// Version operations

func (s *DragonboatStore) GetObjectVersion(ctx context.Context, bucket, key, versionID string) (*ObjectMeta, error) {
	// If no version ID specified or "null", get the current version
	if versionID == "" || versionID == "null" {
		return s.GetObjectMeta(ctx, bucket, key)
	}

	// Get specific version from version store
	dbKey := fmt.Appendf(nil, "%s%s/%s#%s", prefixObjectVersion, bucket, key, versionID)

	data, err := s.get(dbKey)
	if err == badger.ErrKeyNotFound {
		return nil, fmt.Errorf("object version not found: %s/%s (version %s)", bucket, key, versionID)
	}

	if err != nil {
		return nil, err
	}

	var meta ObjectMeta
	err = json.Unmarshal(data, &meta)
	if err != nil {
		return nil, err
	}

	return &meta, nil
}

func (s *DragonboatStore) ListObjectVersions(ctx context.Context, bucket, prefix, delimiter, keyMarker, versionIDMarker string, maxKeys int) (*VersionListing, error) {
	var (
		versions       []*ObjectMeta
		deleteMarkers  []*ObjectMeta
		commonPrefixes []string
		prefixSet      = make(map[string]bool)
	)

	// Collect versioned objects
	err := s.collectVersionedObjects(bucket, prefix, delimiter, keyMarker, versionIDMarker, maxKeys, &versions, &deleteMarkers, &commonPrefixes, prefixSet)
	if err != nil {
		return nil, err
	}

	// Collect current/non-versioned objects
	err = s.collectCurrentObjects(bucket, prefix, delimiter, &versions, &deleteMarkers, &commonPrefixes, prefixSet)
	if err != nil {
		return nil, err
	}

	// Sort and paginate results
	return s.buildVersionListing(versions, deleteMarkers, commonPrefixes, maxKeys), nil
}

// collectVersionedObjects retrieves all versioned objects from the version store.
func (s *DragonboatStore) collectVersionedObjects(bucket, prefix, delimiter, keyMarker, versionIDMarker string, maxKeys int, versions, deleteMarkers *[]*ObjectMeta, commonPrefixes *[]string, prefixSet map[string]bool) error {
	dbPrefix := fmt.Appendf(nil, "%s%s/", prefixObjectVersion, bucket)

	return s.badger.View(func(txn *badger.Txn) error {
		return s.iterateVersionedObjects(txn, dbPrefix, bucket, prefix, delimiter, keyMarker, versionIDMarker, maxKeys, versions, deleteMarkers, commonPrefixes, prefixSet)
	})
}

func (s *DragonboatStore) iterateVersionedObjects(txn *badger.Txn, dbPrefix []byte, bucket, prefix, delimiter, keyMarker, versionIDMarker string, maxKeys int, versions, deleteMarkers *[]*ObjectMeta, commonPrefixes *[]string, prefixSet map[string]bool) error {
	opts := badger.DefaultIteratorOptions
	opts.Prefix = dbPrefix

	it := txn.NewIterator(opts)
	defer it.Close()

	startKey := s.calculateStartKey(dbPrefix, bucket, keyMarker, versionIDMarker)
	count := 0

	for it.Seek(startKey); it.Valid(); it.Next() {
		item := it.Item()

		if s.processVersionedItem(item, dbPrefix, prefix, delimiter, keyMarker, versionIDMarker, versions, deleteMarkers, commonPrefixes, prefixSet) {
			count++
		}

		if maxKeys > 0 && count >= maxKeys+1 {
			break
		}
	}

	return nil
}

func (s *DragonboatStore) processVersionedItem(item *badger.Item, dbPrefix []byte, prefix, delimiter, keyMarker, versionIDMarker string, versions, deleteMarkers *[]*ObjectMeta, commonPrefixes *[]string, prefixSet map[string]bool) bool {
	objKey, versionID, shouldContinue := s.parseVersionKey(item.Key(), dbPrefix)
	if shouldContinue {
		return false
	}

	if !s.matchesPrefixFilter(objKey, prefix) {
		return false
	}

	if s.handleDelimiter(objKey, prefix, delimiter, commonPrefixes, prefixSet) {
		return false
	}

	if s.shouldSkipMarker(objKey, versionID, keyMarker, versionIDMarker) {
		return false
	}

	meta, err := s.unmarshalObjectMeta(item)
	if err != nil {
		return false
	}

	s.categorizeObject(meta, versions, deleteMarkers)

	return true
}

// collectCurrentObjects retrieves current/non-versioned objects from the main object store.
func (s *DragonboatStore) collectCurrentObjects(bucket, prefix, delimiter string, versions, deleteMarkers *[]*ObjectMeta, commonPrefixes *[]string, prefixSet map[string]bool) error {
	objPrefix := fmt.Appendf(nil, "%s%s/", prefixObject, bucket)

	return s.badger.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = objPrefix

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			objKey := s.extractObjectKey(item.Key(), objPrefix)

			if !s.matchesPrefixFilter(objKey, prefix) {
				continue
			}

			if s.handleDelimiter(objKey, prefix, delimiter, commonPrefixes, prefixSet) {
				continue
			}

			meta, err := s.unmarshalObjectMeta(item)
			if err != nil {
				continue
			}

			if s.isNonVersionedObject(meta) {
				s.categorizeObject(meta, versions, deleteMarkers)
			}
		}

		return nil
	})
}

// Helper functions for ListObjectVersions

func (s *DragonboatStore) calculateStartKey(dbPrefix []byte, bucket, keyMarker, versionIDMarker string) []byte {
	if keyMarker == "" {
		return dbPrefix
	}

	if versionIDMarker != "" {
		return fmt.Appendf(nil, "%s%s/%s#%s", prefixObjectVersion, bucket, keyMarker, versionIDMarker)
	}

	return fmt.Appendf(nil, "%s%s/%s#", prefixObjectVersion, bucket, keyMarker)
}

func (s *DragonboatStore) parseVersionKey(key, dbPrefix []byte) (objKey, versionID string, shouldContinue bool) {
	trimmed := strings.TrimPrefix(string(key), string(dbPrefix))

	hashIdx := strings.LastIndex(trimmed, "#")
	if hashIdx < 0 {
		return "", "", true
	}

	return trimmed[:hashIdx], trimmed[hashIdx+1:], false
}

func (s *DragonboatStore) extractObjectKey(key, prefix []byte) string {
	return strings.TrimPrefix(string(key), string(prefix))
}

func (s *DragonboatStore) matchesPrefixFilter(objKey, prefix string) bool {
	return prefix == "" || strings.HasPrefix(objKey, prefix)
}

func (s *DragonboatStore) handleDelimiter(objKey, prefix, delimiter string, commonPrefixes *[]string, prefixSet map[string]bool) bool {
	if delimiter == "" {
		return false
	}

	afterPrefix := strings.TrimPrefix(objKey, prefix)
	if idx := strings.Index(afterPrefix, delimiter); idx >= 0 {
		commonPrefix := prefix + afterPrefix[:idx+1]
		if !prefixSet[commonPrefix] {
			prefixSet[commonPrefix] = true

			*commonPrefixes = append(*commonPrefixes, commonPrefix)
		}

		return true
	}

	return false
}

func (s *DragonboatStore) shouldSkipMarker(objKey, versionID, keyMarker, versionIDMarker string) bool {
	if keyMarker == "" || objKey != keyMarker {
		return false
	}

	return versionIDMarker != "" && versionID <= versionIDMarker
}

func (s *DragonboatStore) unmarshalObjectMeta(item *badger.Item) (*ObjectMeta, error) {
	val, err := item.ValueCopy(nil)
	if err != nil {
		return nil, err
	}

	var meta ObjectMeta

	err = json.Unmarshal(val, &meta)
	if err != nil {
		return nil, err
	}

	return &meta, nil
}

func (s *DragonboatStore) categorizeObject(meta *ObjectMeta, versions, deleteMarkers *[]*ObjectMeta) {
	if meta.DeleteMarker {
		*deleteMarkers = append(*deleteMarkers, meta)
	} else {
		*versions = append(*versions, meta)
	}
}

func (s *DragonboatStore) isNonVersionedObject(meta *ObjectMeta) bool {
	return meta.VersionID == "" || meta.VersionID == "null"
}

func (s *DragonboatStore) buildVersionListing(versions, deleteMarkers []*ObjectMeta, commonPrefixes []string, maxKeys int) *VersionListing {
	sort.Strings(commonPrefixes)

	s.sortVersions(versions)
	s.sortVersions(deleteMarkers)

	isTruncated, nextKeyMarker, nextVersionIDMarker := s.calculatePagination(versions, deleteMarkers, maxKeys)

	return &VersionListing{
		Versions:            versions,
		DeleteMarkers:       deleteMarkers,
		CommonPrefixes:      commonPrefixes,
		IsTruncated:         isTruncated,
		NextKeyMarker:       nextKeyMarker,
		NextVersionIDMarker: nextVersionIDMarker,
	}
}

func (s *DragonboatStore) sortVersions(metas []*ObjectMeta) {
	sort.Slice(metas, func(i, j int) bool {
		if metas[i].Key != metas[j].Key {
			return metas[i].Key < metas[j].Key
		}

		return metas[i].VersionID > metas[j].VersionID
	})
}

func (s *DragonboatStore) calculatePagination(versions, deleteMarkers []*ObjectMeta, maxKeys int) (isTruncated bool, nextKeyMarker, nextVersionIDMarker string) {
	if maxKeys <= 0 {
		return false, "", ""
	}

	totalItems := len(versions) + len(deleteMarkers)
	if totalItems <= maxKeys {
		return false, "", ""
	}

	isTruncated = true

	if len(versions) > maxKeys {
		nextKeyMarker = versions[maxKeys].Key
		nextVersionIDMarker = versions[maxKeys].VersionID
		_ = versions[:maxKeys] // Truncate to maxKeys
	} else if len(deleteMarkers) > 0 {
		remaining := maxKeys - len(versions)
		if remaining < len(deleteMarkers) {
			nextKeyMarker = deleteMarkers[remaining].Key
			nextVersionIDMarker = deleteMarkers[remaining].VersionID
			_ = deleteMarkers[:remaining] // Truncate to remaining
		}
	}

	return isTruncated, nextKeyMarker, nextVersionIDMarker
}

func (s *DragonboatStore) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error {
	data, err := json.Marshal(struct {
		Bucket    string `json:"bucket"`
		Key       string `json:"key"`
		VersionID string `json:"version_id"`
	}{Bucket: bucket, Key: key, VersionID: versionID})
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdDeleteObjectVersion, Data: data})
}

func (s *DragonboatStore) PutObjectMetaVersioned(ctx context.Context, meta *ObjectMeta, preserveOldVersions bool) error {
	data, err := json.Marshal(struct {
		Meta                *ObjectMeta `json:"meta"`
		PreserveOldVersions bool        `json:"preserve_old_versions"`
	}{Meta: meta, PreserveOldVersions: preserveOldVersions})
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdPutObjectMetaVersioned, Data: data})
}

// Multipart upload operations

func (s *DragonboatStore) CreateMultipartUpload(ctx context.Context, upload *MultipartUpload) error {
	data, err := json.Marshal(upload)
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdCreateMultipartUpload, Data: data})
}

func (s *DragonboatStore) GetMultipartUpload(ctx context.Context, bucket, key, uploadID string) (*MultipartUpload, error) {
	dbKey := fmt.Appendf(nil, "%s%s/%s/%s", prefixMultipart, bucket, key, uploadID)

	data, err := s.get(dbKey)
	if err == badger.ErrKeyNotFound {
		return nil, fmt.Errorf("multipart upload not found: %s", uploadID)
	}

	if err != nil {
		return nil, err
	}

	var upload MultipartUpload
	err = json.Unmarshal(data, &upload)
	if err != nil {
		return nil, err
	}

	return &upload, nil
}

func (s *DragonboatStore) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	data, err := json.Marshal(struct {
		Bucket   string `json:"bucket"`
		Key      string `json:"key"`
		UploadID string `json:"upload_id"`
	}{Bucket: bucket, Key: key, UploadID: uploadID})
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdAbortMultipartUpload, Data: data})
}

func (s *DragonboatStore) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	data, err := json.Marshal(struct {
		Bucket   string `json:"bucket"`
		Key      string `json:"key"`
		UploadID string `json:"upload_id"`
	}{Bucket: bucket, Key: key, UploadID: uploadID})
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdCompleteMultipartUpload, Data: data})
}

func (s *DragonboatStore) AddUploadPart(ctx context.Context, bucket, key, uploadID string, part *UploadPart) error {
	data, err := json.Marshal(struct {
		Bucket   string     `json:"bucket"`
		Key      string     `json:"key"`
		UploadID string     `json:"upload_id"`
		Part     UploadPart `json:"part"`
	}{Bucket: bucket, Key: key, UploadID: uploadID, Part: *part})
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdAddUploadPart, Data: data})
}

func (s *DragonboatStore) ListMultipartUploads(ctx context.Context, bucket string) ([]*MultipartUpload, error) {
	prefix := []byte(fmt.Sprintf("%s%s/", prefixMultipart, bucket))

	results, err := s.scan(prefix)
	if err != nil {
		return nil, err
	}

	uploads := make([]*MultipartUpload, 0, len(results))
	for _, data := range results {
		var upload MultipartUpload

		err := json.Unmarshal(data, &upload)
		if err != nil {
			continue
		}

		uploads = append(uploads, &upload)
	}

	return uploads, nil
}

// User operations

func (s *DragonboatStore) CreateUser(ctx context.Context, user *User) error {
	data, err := json.Marshal(user)
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdCreateUser, Data: data})
}

func (s *DragonboatStore) GetUser(ctx context.Context, id string) (*User, error) {
	key := []byte(prefixUser + id)

	data, err := s.get(key)
	if err == badger.ErrKeyNotFound {
		return nil, fmt.Errorf("user not found: %s", id)
	}

	if err != nil {
		return nil, err
	}

	var user User
	err = json.Unmarshal(data, &user)
	if err != nil {
		return nil, err
	}

	return &user, nil
}

func (s *DragonboatStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
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

func (s *DragonboatStore) UpdateUser(ctx context.Context, user *User) error {
	data, err := json.Marshal(user)
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdUpdateUser, Data: data})
}

func (s *DragonboatStore) DeleteUser(ctx context.Context, id string) error {
	data, err := json.Marshal(id)
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdDeleteUser, Data: data})
}

func (s *DragonboatStore) ListUsers(ctx context.Context) ([]*User, error) {
	results, err := s.scan([]byte(prefixUser))
	if err != nil {
		return nil, err
	}

	users := make([]*User, 0, len(results))
	for _, data := range results {
		var user User

		err := json.Unmarshal(data, &user)
		if err != nil {
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

func (s *DragonboatStore) CreateAccessKey(ctx context.Context, key *AccessKey) error {
	data, err := json.Marshal(key)
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdCreateAccessKey, Data: data})
}

func (s *DragonboatStore) GetAccessKey(ctx context.Context, accessKeyID string) (*AccessKey, error) {
	key := []byte(prefixAccessKey + accessKeyID)

	data, err := s.get(key)
	if err == badger.ErrKeyNotFound {
		return nil, fmt.Errorf("access key not found: %s", accessKeyID)
	}

	if err != nil {
		return nil, err
	}

	var accessKey AccessKey
	err = json.Unmarshal(data, &accessKey)
	if err != nil {
		return nil, err
	}

	return &accessKey, nil
}

func (s *DragonboatStore) DeleteAccessKey(ctx context.Context, accessKeyID string) error {
	data, err := json.Marshal(accessKeyID)
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdDeleteAccessKey, Data: data})
}

func (s *DragonboatStore) ListAccessKeys(ctx context.Context, userID string) ([]*AccessKey, error) {
	results, err := s.scan([]byte(prefixAccessKey))
	if err != nil {
		return nil, err
	}

	var keys []*AccessKey
	for _, data := range results {
		var key AccessKey

		err := json.Unmarshal(data, &key)
		if err != nil {
			continue
		}

		if key.UserID == userID {
			keys = append(keys, &key)
		}
	}

	return keys, nil
}

// Policy operations

func (s *DragonboatStore) CreatePolicy(ctx context.Context, policy *Policy) error {
	data, err := json.Marshal(policy)
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdCreatePolicy, Data: data})
}

func (s *DragonboatStore) GetPolicy(ctx context.Context, name string) (*Policy, error) {
	key := []byte(prefixPolicy + name)

	data, err := s.get(key)
	if err == badger.ErrKeyNotFound {
		return nil, fmt.Errorf("policy not found: %s", name)
	}

	if err != nil {
		return nil, err
	}

	var policy Policy
	err = json.Unmarshal(data, &policy)
	if err != nil {
		return nil, err
	}

	return &policy, nil
}

func (s *DragonboatStore) UpdatePolicy(ctx context.Context, policy *Policy) error {
	data, err := json.Marshal(policy)
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdUpdatePolicy, Data: data})
}

func (s *DragonboatStore) DeletePolicy(ctx context.Context, name string) error {
	data, err := json.Marshal(name)
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdDeletePolicy, Data: data})
}

func (s *DragonboatStore) ListPolicies(ctx context.Context) ([]*Policy, error) {
	results, err := s.scan([]byte(prefixPolicy))
	if err != nil {
		return nil, err
	}

	policies := make([]*Policy, 0, len(results))
	for _, data := range results {
		var policy Policy

		err := json.Unmarshal(data, &policy)
		if err != nil {
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

func (s *DragonboatStore) GetClusterInfo(ctx context.Context) (*ClusterInfo, error) {
	nodes, err := s.ListNodes(ctx)
	if err != nil {
		return nil, err
	}

	leaderID, valid, term, ready := s.nodeHost.GetLeaderID(s.shardID)
	// Intentionally ignore valid, term, ready - only leaderID is needed
	_ = valid
	_ = term
	_ = ready
	leaderAddr, _ := s.LeaderAddress(ctx)

	return &ClusterInfo{
		ClusterID:     strconv.FormatUint(s.shardID, 10),
		LeaderID:      strconv.FormatUint(leaderID, 10),
		LeaderAddress: leaderAddr,
		Nodes:         nodes,
		RaftState:     s.State(),
	}, nil
}

func (s *DragonboatStore) AddNode(ctx context.Context, node *NodeInfo) error {
	data, err := json.Marshal(node)
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdAddNode, Data: data})
}

func (s *DragonboatStore) RemoveNode(ctx context.Context, nodeID string) error {
	data, err := json.Marshal(nodeID)
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdRemoveNode, Data: data})
}

func (s *DragonboatStore) ListNodes(ctx context.Context) ([]*NodeInfo, error) {
	results, err := s.scan([]byte(prefixNode))
	if err != nil {
		return nil, err
	}

	nodes := make([]*NodeInfo, 0, len(results))
	for _, data := range results {
		var node NodeInfo

		err := json.Unmarshal(data, &node)
		if err != nil {
			continue
		}

		nodes = append(nodes, &node)
	}

	return nodes, nil
}

// Audit operations

// StoreAuditEvent stores an audit event.
func (s *DragonboatStore) StoreAuditEvent(ctx context.Context, event *audit.AuditEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	return s.apply(ctx, &command{Type: cmdStoreAuditEvent, Data: data})
}

// ListAuditEvents lists audit events with filtering.
func (s *DragonboatStore) ListAuditEvents(ctx context.Context, filter audit.AuditFilter) (*audit.AuditListResult, error) {
	maxResults := s.normalizeMaxResults(filter.MaxResults)
	seekKey := s.determineSeekKey(filter)

	events, err := s.scanAuditEvents(seekKey, filter, maxResults)
	if err != nil {
		return nil, err
	}

	nextToken := s.buildNextToken(events, maxResults, filter)
	if len(events) > maxResults {
		events = events[:maxResults]
	}

	return &audit.AuditListResult{
		Events:    events,
		NextToken: nextToken,
	}, nil
}

func (s *DragonboatStore) normalizeMaxResults(maxResults int) int {
	if maxResults <= 0 {
		return 100
	}
	if maxResults > 1000 {
		return 1000
	}
	return maxResults
}

func (s *DragonboatStore) determineSeekKey(filter audit.AuditFilter) []byte {
	prefix := []byte(prefixAudit)

	switch {
	case filter.NextToken != "":
		return []byte(filter.NextToken)
	case !filter.EndTime.IsZero():
		return []byte(fmt.Sprintf("%s%s", prefixAudit, filter.EndTime.Format(time.RFC3339Nano)+"~"))
	default:
		seekKey := make([]byte, len(prefix)+1)
		copy(seekKey, prefix)
		seekKey[len(prefix)] = 0xFF
		return seekKey
	}
}

func (s *DragonboatStore) scanAuditEvents(
	seekKey []byte,
	filter audit.AuditFilter,
	maxResults int,
) ([]audit.AuditEvent, error) {
	var events []audit.AuditEvent

	err := s.badger.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Reverse = true

		it := txn.NewIterator(opts)
		defer it.Close()

		count := 0
		for it.Seek(seekKey); it.Valid(); it.Next() {
			item := it.Item()
			key := item.Key()

			if !strings.HasPrefix(string(key), prefixAudit) {
				break
			}

			event, err := s.unmarshalAuditEvent(item)
			if err != nil {
				continue
			}

			if s.shouldStopScan(event, filter) {
				break
			}

			if !s.eventMatchesFilter(event, filter) {
				continue
			}

			events = append(events, *event)
			count++

			if count >= maxResults+1 {
				break
			}
		}

		return nil
	})

	return events, err
}

func (s *DragonboatStore) unmarshalAuditEvent(item *badger.Item) (*audit.AuditEvent, error) {
	val, err := item.ValueCopy(nil)
	if err != nil {
		return nil, err
	}

	var event audit.AuditEvent
	if err := json.Unmarshal(val, &event); err != nil {
		return nil, err
	}

	return &event, nil
}

func (s *DragonboatStore) shouldStopScan(event *audit.AuditEvent, filter audit.AuditFilter) bool {
	if !filter.StartTime.IsZero() && event.Timestamp.Before(filter.StartTime) {
		return true
	}
	return false
}

func (s *DragonboatStore) eventMatchesFilter(event *audit.AuditEvent, filter audit.AuditFilter) bool {
	if !filter.EndTime.IsZero() && event.Timestamp.After(filter.EndTime) {
		return false
	}

	if filter.Bucket != "" && event.Resource.Bucket != filter.Bucket {
		return false
	}

	if filter.User != "" && event.UserIdentity.Username != filter.User && event.UserIdentity.UserID != filter.User {
		return false
	}

	if filter.EventType != "" && !strings.HasPrefix(string(event.EventType), filter.EventType) {
		return false
	}

	if filter.Result != "" && string(event.Result) != filter.Result {
		return false
	}

	return true
}

func (s *DragonboatStore) buildNextToken(events []audit.AuditEvent, maxResults int, filter audit.AuditFilter) string {
	if len(events) > maxResults {
		return fmt.Sprintf("%s%s", prefixAudit, events[maxResults].Timestamp.Format(time.RFC3339Nano))
	}
	return ""
}

// DeleteOldAuditEvents deletes audit events older than the specified time.
func (s *DragonboatStore) DeleteOldAuditEvents(ctx context.Context, before time.Time) (int, error) {
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
			err = json.Unmarshal(val, &event)
			if err != nil {
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

	// Delete the collected events through Dragonboat
	deleted := 0

	for _, id := range keysToDelete {
		data, err := json.Marshal(id)
		if err != nil {
			continue
		}

		err = s.apply(ctx, &command{Type: cmdDeleteAuditEvent, Data: data})
		if err != nil {
			continue
		}

		deleted++
	}

	return deleted, nil
}
