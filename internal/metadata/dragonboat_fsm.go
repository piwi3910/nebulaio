package metadata

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/lni/dragonboat/v4/statemachine"
	"github.com/piwi3910/nebulaio/internal/audit"
	"github.com/rs/zerolog/log"
)

// ErrLookupNotSupported is returned when Lookup is called but not supported.
var ErrLookupNotSupported = errors.New("lookup not supported, reads go directly to storage")

// stateMachine implements statemachine.IStateMachine for Dragonboat.
type stateMachine struct {
	db *badger.DB
}

// newStateMachine creates a new state machine instance.
func newStateMachine(db *badger.DB) *stateMachine {
	return &stateMachine{db: db}
}

// Open opens the state machine.
func (sm *stateMachine) Open(stopc <-chan struct{}) (uint64, error) {
	// The state machine is already open via the shared BadgerDB instance
	// Return the last applied index (we track this in the DB)
	var index uint64

	err := sm.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("_applied_index"))
		if err == badger.ErrKeyNotFound {
			return nil
		}

		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			if len(val) == 8 {
				index = uint64(val[0]) | uint64(val[1])<<8 | uint64(val[2])<<16 | uint64(val[3])<<24 |
					uint64(val[4])<<32 | uint64(val[5])<<40 | uint64(val[6])<<48 | uint64(val[7])<<56
			}

			return nil
		})
	})

	return index, err
}

// Update updates the state machine with a log entry.
func (sm *stateMachine) Update(entry statemachine.Entry) (statemachine.Result, error) {
	var cmd command

	err := json.Unmarshal(entry.Cmd, &cmd)
	if err != nil {
		//nolint:nilerr // Dragonboat pattern: Result.Value indicates error status, not Go error return
		return statemachine.Result{Value: 1}, nil // Error code
	}

	err = sm.processCommand(&cmd)
	if err != nil {
		//nolint:nilerr // Dragonboat pattern: Result.Value indicates error status, not Go error return
		return statemachine.Result{Value: 1}, nil // Error code
	}

	// Update applied index - log error if update fails to detect potential state consistency issues
	updateErr := sm.updateAppliedIndex(entry.Index)
	if updateErr != nil {
		log.Error().
			Err(updateErr).
			Uint64("index", entry.Index).
			Msg("failed to update applied index in state machine - this may cause state consistency issues on restart")
	}

	return statemachine.Result{Value: 0}, nil // Success
}

// updateAppliedIndex stores the last applied index.
func (sm *stateMachine) updateAppliedIndex(index uint64) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		val := make([]byte, 8)
		val[0] = byte(index)
		val[1] = byte(index >> 8)
		val[2] = byte(index >> 16)
		val[3] = byte(index >> 24)
		val[4] = byte(index >> 32)
		val[5] = byte(index >> 40)
		val[6] = byte(index >> 48)
		val[7] = byte(index >> 56)

		return txn.Set([]byte("_applied_index"), val)
	})
}

// processCommand processes a single command.
func (sm *stateMachine) processCommand(cmd *command) error {
	switch cmd.Type {
	case cmdCreateBucket:
		var bucket Bucket

		err := json.Unmarshal(cmd.Data, &bucket)
		if err != nil {
			return err
		}

		return sm.applyCreateBucket(&bucket)

	case cmdDeleteBucket:
		var name string

		err := json.Unmarshal(cmd.Data, &name)
		if err != nil {
			return err
		}

		return sm.applyDeleteBucket(name)

	case cmdUpdateBucket:
		var bucket Bucket

		err := json.Unmarshal(cmd.Data, &bucket)
		if err != nil {
			return err
		}

		return sm.applyUpdateBucket(&bucket)

	case cmdPutObjectMeta:
		var meta ObjectMeta

		err := json.Unmarshal(cmd.Data, &meta)
		if err != nil {
			return err
		}

		return sm.applyPutObjectMeta(&meta)

	case cmdDeleteObjectMeta:
		var data struct {
			Bucket string `json:"bucket"`
			Key    string `json:"key"`
		}

		err := json.Unmarshal(cmd.Data, &data)
		if err != nil {
			return err
		}

		return sm.applyDeleteObjectMeta(data.Bucket, data.Key)

	case cmdPutObjectMetaVersioned:
		var data struct {
			Meta                *ObjectMeta `json:"meta"`
			PreserveOldVersions bool        `json:"preserve_old_versions"`
		}

		err := json.Unmarshal(cmd.Data, &data)
		if err != nil {
			return err
		}

		return sm.applyPutObjectMetaVersioned(data.Meta, data.PreserveOldVersions)

	case cmdDeleteObjectVersion:
		var data struct {
			Bucket    string `json:"bucket"`
			Key       string `json:"key"`
			VersionID string `json:"version_id"`
		}

		err := json.Unmarshal(cmd.Data, &data)
		if err != nil {
			return err
		}

		return sm.applyDeleteObjectVersion(data.Bucket, data.Key, data.VersionID)

	case cmdCreateUser:
		var user User

		err := json.Unmarshal(cmd.Data, &user)
		if err != nil {
			return err
		}

		return sm.applyCreateUser(&user)

	case cmdUpdateUser:
		var user User

		err := json.Unmarshal(cmd.Data, &user)
		if err != nil {
			return err
		}

		return sm.applyUpdateUser(&user)

	case cmdDeleteUser:
		var id string

		err := json.Unmarshal(cmd.Data, &id)
		if err != nil {
			return err
		}

		return sm.applyDeleteUser(id)

	case cmdCreateAccessKey:
		var key AccessKey

		err := json.Unmarshal(cmd.Data, &key)
		if err != nil {
			return err
		}

		return sm.applyCreateAccessKey(&key)

	case cmdDeleteAccessKey:
		var id string

		err := json.Unmarshal(cmd.Data, &id)
		if err != nil {
			return err
		}

		return sm.applyDeleteAccessKey(id)

	case cmdCreatePolicy:
		var policy Policy

		err := json.Unmarshal(cmd.Data, &policy)
		if err != nil {
			return err
		}

		return sm.applyCreatePolicy(&policy)

	case cmdUpdatePolicy:
		var policy Policy

		err := json.Unmarshal(cmd.Data, &policy)
		if err != nil {
			return err
		}

		return sm.applyUpdatePolicy(&policy)

	case cmdDeletePolicy:
		var name string

		err := json.Unmarshal(cmd.Data, &name)
		if err != nil {
			return err
		}

		return sm.applyDeletePolicy(name)

	case cmdCreateMultipartUpload:
		var upload MultipartUpload

		err := json.Unmarshal(cmd.Data, &upload)
		if err != nil {
			return err
		}

		return sm.applyCreateMultipartUpload(&upload)

	case cmdAbortMultipartUpload:
		var data struct {
			Bucket   string `json:"bucket"`
			Key      string `json:"key"`
			UploadID string `json:"upload_id"`
		}

		err := json.Unmarshal(cmd.Data, &data)
		if err != nil {
			return err
		}

		return sm.applyAbortMultipartUpload(data.Bucket, data.Key, data.UploadID)

	case cmdCompleteMultipartUpload:
		var data struct {
			Bucket   string `json:"bucket"`
			Key      string `json:"key"`
			UploadID string `json:"upload_id"`
		}

		err := json.Unmarshal(cmd.Data, &data)
		if err != nil {
			return err
		}

		return sm.applyCompleteMultipartUpload(data.Bucket, data.Key, data.UploadID)

	case cmdAddUploadPart:
		var data struct {
			Bucket   string     `json:"bucket"`
			Key      string     `json:"key"`
			UploadID string     `json:"upload_id"`
			Part     UploadPart `json:"part"`
		}

		err := json.Unmarshal(cmd.Data, &data)
		if err != nil {
			return err
		}

		return sm.applyAddUploadPart(data.Bucket, data.Key, data.UploadID, &data.Part)

	case cmdAddNode:
		var node NodeInfo

		err := json.Unmarshal(cmd.Data, &node)
		if err != nil {
			return err
		}

		return sm.applyAddNode(&node)

	case cmdRemoveNode:
		var nodeID string

		err := json.Unmarshal(cmd.Data, &nodeID)
		if err != nil {
			return err
		}

		return sm.applyRemoveNode(nodeID)

	case cmdStoreAuditEvent:
		var event audit.AuditEvent

		err := json.Unmarshal(cmd.Data, &event)
		if err != nil {
			return err
		}

		return sm.applyStoreAuditEvent(&event)

	case cmdDeleteAuditEvent:
		var eventID string

		err := json.Unmarshal(cmd.Data, &eventID)
		if err != nil {
			return err
		}

		return sm.applyDeleteAuditEvent(eventID)
	}

	return fmt.Errorf("unknown command type: %s", cmd.Type)
}

// Lookup performs a read-only query on the state machine.
func (sm *stateMachine) Lookup(query interface{}) (interface{}, error) {
	// Dragonboat read queries are not used in our implementation
	// All reads go directly to BadgerDB
	return nil, ErrLookupNotSupported
}

// SaveSnapshot saves a snapshot of the state machine.
func (sm *stateMachine) SaveSnapshot(w io.Writer, fc statemachine.ISnapshotFileCollection, done <-chan struct{}) error {
	encoder := json.NewEncoder(w)

	err := sm.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			select {
			case <-done:
				return statemachine.ErrSnapshotStopped
			default:
			}

			item := it.Item()
			key := item.KeyCopy(nil)

			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}

			kv := struct {
				Key   []byte `json:"key"`
				Value []byte `json:"value"`
			}{Key: key, Value: val}

			err = encoder.Encode(&kv)
			if err != nil {
				return err
			}
		}

		return nil
	})

	return err
}

// RecoverFromSnapshot restores the state machine from a snapshot.
func (sm *stateMachine) RecoverFromSnapshot(r io.Reader, files []statemachine.SnapshotFile, done <-chan struct{}) error {
	// Clear existing data
	err := sm.db.DropAll()
	if err != nil {
		return err
	}

	// Read snapshot data
	decoder := json.NewDecoder(r)

	return sm.db.Update(func(txn *badger.Txn) error {
		for {
			select {
			case <-done:
				return statemachine.ErrSnapshotStopped
			default:
			}

			var kv struct {
				Key   []byte `json:"key"`
				Value []byte `json:"value"`
			}

			err := decoder.Decode(&kv)
			if err == io.EOF {
				break
			} else if err != nil {
				return err
			}

			err = txn.Set(kv.Key, kv.Value)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// Close closes the state machine.
func (sm *stateMachine) Close() error {
	// Don't close BadgerDB here - it's managed by DragonboatStore
	return nil
}

// The following methods are identical to the FSM apply methods from raft.go

func (sm *stateMachine) applyCreateBucket(bucket *Bucket) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		key := []byte(prefixBucket + bucket.Name)

		// Check if bucket already exists
		_, err := txn.Get(key)
		if err == nil {
			return fmt.Errorf("bucket already exists: %s", bucket.Name)
		}

		if err != badger.ErrKeyNotFound {
			return err
		}

		data, err := json.Marshal(bucket)
		if err != nil {
			return err
		}

		return txn.Set(key, data)
	})
}

func (sm *stateMachine) applyDeleteBucket(name string) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		key := []byte(prefixBucket + name)
		return txn.Delete(key)
	})
}

func (sm *stateMachine) applyUpdateBucket(bucket *Bucket) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		key := []byte(prefixBucket + bucket.Name)

		data, err := json.Marshal(bucket)
		if err != nil {
			return err
		}

		return txn.Set(key, data)
	})
}

func (sm *stateMachine) applyPutObjectMeta(meta *ObjectMeta) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		key := []byte(fmt.Sprintf("%s%s/%s", prefixObject, meta.Bucket, meta.Key))

		data, err := json.Marshal(meta)
		if err != nil {
			return err
		}

		return txn.Set(key, data)
	})
}

func (sm *stateMachine) applyDeleteObjectMeta(bucket, objKey string) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		key := []byte(fmt.Sprintf("%s%s/%s", prefixObject, bucket, objKey))
		return txn.Delete(key)
	})
}

func (sm *stateMachine) applyPutObjectMetaVersioned(meta *ObjectMeta, preserveOldVersions bool) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		// Current object key (for latest version pointer)
		currentKey := []byte(fmt.Sprintf("%s%s/%s", prefixObject, meta.Bucket, meta.Key))

		if preserveOldVersions {
			// Get current version and mark it as not latest
			item, err := txn.Get(currentKey)
			if err == nil {
				var oldMeta ObjectMeta

				err = item.Value(func(val []byte) error {
					return json.Unmarshal(val, &oldMeta)
				})
				if err == nil && oldMeta.VersionID != "" {
					// Mark old version as not latest
					oldMeta.IsLatest = false

					oldData, err := json.Marshal(&oldMeta)
					if err != nil {
						return err
					}

					// Store old version with compound key: objver:{bucket}/{key}#{versionID}
					oldVersionKey := []byte(fmt.Sprintf("%s%s/%s#%s", prefixObjectVersion, oldMeta.Bucket, oldMeta.Key, oldMeta.VersionID))

					err = txn.Set(oldVersionKey, oldData)
					if err != nil {
						return err
					}
				}
			}
		}

		// Mark new version as latest
		meta.IsLatest = true

		// Store new version in version history if it has a version ID
		if meta.VersionID != "" {
			versionKey := []byte(fmt.Sprintf("%s%s/%s#%s", prefixObjectVersion, meta.Bucket, meta.Key, meta.VersionID))

			versionData, err := json.Marshal(meta)
			if err != nil {
				return err
			}

			err = txn.Set(versionKey, versionData)
			if err != nil {
				return err
			}
		}

		// Store/update current version pointer
		data, err := json.Marshal(meta)
		if err != nil {
			return err
		}

		return txn.Set(currentKey, data)
	})
}

func (sm *stateMachine) applyDeleteObjectVersion(bucket, objKey, versionID string) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		// Delete the specific version
		versionKey := []byte(fmt.Sprintf("%s%s/%s#%s", prefixObjectVersion, bucket, objKey, versionID))

		err := txn.Delete(versionKey)
		if err != nil && err != badger.ErrKeyNotFound {
			return err
		}

		// Check if this was the current/latest version
		currentKey := []byte(fmt.Sprintf("%s%s/%s", prefixObject, bucket, objKey))

		item, err := txn.Get(currentKey)
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return nil
			}

			return err
		}

		var currentMeta ObjectMeta

		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &currentMeta)
		})
		if err != nil {
			return err
		}

		// If we deleted the current version, we need to find the next latest version
		if currentMeta.VersionID == versionID {
			// Find the most recent remaining version
			prefix := []byte(fmt.Sprintf("%s%s/%s#", prefixObjectVersion, bucket, objKey))
			opts := badger.DefaultIteratorOptions
			opts.Prefix = prefix
			opts.Reverse = true // Get newest first (ULIDs are sortable)

			it := txn.NewIterator(opts)
			defer it.Close()

			it.Seek(append(prefix, 0xFF)) // Seek to end of prefix range

			if it.ValidForPrefix(prefix) {
				// Found another version, make it the current
				item := it.Item()

				val, err := item.ValueCopy(nil)
				if err != nil {
					return err
				}

				var newLatest ObjectMeta

				err = json.Unmarshal(val, &newLatest)
				if err != nil {
					return err
				}

				newLatest.IsLatest = true

				data, err := json.Marshal(&newLatest)
				if err != nil {
					return err
				}

				// Update version store
				err = txn.Set(item.KeyCopy(nil), data)
				if err != nil {
					return err
				}

				// Update current pointer
				return txn.Set(currentKey, data)
			}

			// No more versions, delete the current pointer
			return txn.Delete(currentKey)
		}

		return nil
	})
}

func (sm *stateMachine) applyCreateUser(user *User) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		// Store by ID
		key := []byte(prefixUser + user.ID)

		data, err := json.Marshal(user)
		if err != nil {
			return err
		}

		err = txn.Set(key, data)
		if err != nil {
			return err
		}

		// Store username -> ID mapping
		usernameKey := []byte(prefixUsername + user.Username)

		return txn.Set(usernameKey, []byte(user.ID))
	})
}

func (sm *stateMachine) applyUpdateUser(user *User) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		key := []byte(prefixUser + user.ID)

		data, err := json.Marshal(user)
		if err != nil {
			return err
		}

		return txn.Set(key, data)
	})
}

func (sm *stateMachine) applyDeleteUser(id string) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		// Get user first to delete username mapping
		key := []byte(prefixUser + id)

		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		var user User

		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &user)
		})
		if err != nil {
			return err
		}

		// Delete username mapping
		usernameKey := []byte(prefixUsername + user.Username)

		err = txn.Delete(usernameKey)
		if err != nil {
			return err
		}

		// Delete user
		return txn.Delete(key)
	})
}

func (sm *stateMachine) applyCreateAccessKey(accessKey *AccessKey) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		key := []byte(prefixAccessKey + accessKey.AccessKeyID)

		data, err := json.Marshal(accessKey)
		if err != nil {
			return err
		}

		return txn.Set(key, data)
	})
}

func (sm *stateMachine) applyDeleteAccessKey(accessKeyID string) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		key := []byte(prefixAccessKey + accessKeyID)
		return txn.Delete(key)
	})
}

func (sm *stateMachine) applyCreatePolicy(policy *Policy) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		key := []byte(prefixPolicy + policy.Name)

		data, err := json.Marshal(policy)
		if err != nil {
			return err
		}

		return txn.Set(key, data)
	})
}

func (sm *stateMachine) applyUpdatePolicy(policy *Policy) error {
	return sm.applyCreatePolicy(policy)
}

func (sm *stateMachine) applyDeletePolicy(name string) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		key := []byte(prefixPolicy + name)
		return txn.Delete(key)
	})
}

func (sm *stateMachine) applyCreateMultipartUpload(upload *MultipartUpload) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		key := []byte(fmt.Sprintf("%s%s/%s/%s", prefixMultipart, upload.Bucket, upload.Key, upload.UploadID))

		data, err := json.Marshal(upload)
		if err != nil {
			return err
		}

		return txn.Set(key, data)
	})
}

func (sm *stateMachine) applyAbortMultipartUpload(bucket, objKey, uploadID string) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		key := []byte(fmt.Sprintf("%s%s/%s/%s", prefixMultipart, bucket, objKey, uploadID))
		return txn.Delete(key)
	})
}

func (sm *stateMachine) applyCompleteMultipartUpload(bucket, objKey, uploadID string) error {
	return sm.applyAbortMultipartUpload(bucket, objKey, uploadID)
}

func (sm *stateMachine) applyAddUploadPart(bucket, objKey, uploadID string, part *UploadPart) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		key := []byte(fmt.Sprintf("%s%s/%s/%s", prefixMultipart, bucket, objKey, uploadID))

		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		var upload MultipartUpload

		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &upload)
		})
		if err != nil {
			return err
		}

		// Add or update part
		found := false

		for i, p := range upload.Parts {
			if p.PartNumber == part.PartNumber {
				upload.Parts[i] = *part
				found = true

				break
			}
		}

		if !found {
			upload.Parts = append(upload.Parts, *part)
		}

		data, err := json.Marshal(&upload)
		if err != nil {
			return err
		}

		return txn.Set(key, data)
	})
}

func (sm *stateMachine) applyAddNode(node *NodeInfo) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		key := []byte(prefixNode + node.ID)

		data, err := json.Marshal(node)
		if err != nil {
			return err
		}

		return txn.Set(key, data)
	})
}

func (sm *stateMachine) applyRemoveNode(nodeID string) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		key := []byte(prefixNode + nodeID)
		return txn.Delete(key)
	})
}

func (sm *stateMachine) applyStoreAuditEvent(event *audit.AuditEvent) error {
	return sm.db.Update(func(txn *badger.Txn) error {
		// Use timestamp + ID as key for time-based ordering
		key := []byte(fmt.Sprintf("%s%s:%s", prefixAudit, event.Timestamp.Format(time.RFC3339Nano), event.ID))

		data, err := json.Marshal(event)
		if err != nil {
			return err
		}

		return txn.Set(key, data)
	})
}

func (sm *stateMachine) applyDeleteAuditEvent(eventID string) error {
	// We need to find and delete by event ID since we don't have the timestamp
	return sm.db.Update(func(txn *badger.Txn) error {
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

			if event.ID == eventID {
				return txn.Delete(item.KeyCopy(nil))
			}
		}

		return nil
	})
}
