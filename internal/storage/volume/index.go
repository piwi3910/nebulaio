package volume

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"sort"
	"sync"
)

// Index configuration constants.
const (
	initialIndexCapacity = 1024 // Initial capacity for index entries
	minIndexHeaderSize   = 4    // Minimum size for index header (entry count)
)

// Index is an in-memory sorted index of objects in a volume
// Uses a sorted slice for O(log n) lookups via binary search.
type Index struct {
	entries []IndexEntryFull
	mu      sync.RWMutex
}

// IndexEntryFull is the full index entry including the variable-length key.
type IndexEntryFull struct {
	Key string
	IndexEntry
}

// NewIndex creates a new empty index.
func NewIndex() *Index {
	return &Index{
		entries: make([]IndexEntryFull, 0, initialIndexCapacity),
	}
}

// ReadIndex reads an index from disk.
func ReadIndex(file *os.File, offset int64, size int64) (*Index, error) {
	if size == 0 {
		return NewIndex(), nil
	}

	buf := make([]byte, size)

	_, readErr := file.ReadAt(buf, offset)
	if readErr != nil {
		return nil, readErr
	}

	return unmarshalIndex(buf)
}

// WriteTo writes the index to disk at the given offset.
func (idx *Index) WriteTo(file *os.File, offset int64) (int64, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	data := idx.marshal()
	if _, err := file.WriteAt(data, offset); err != nil {
		return 0, err
	}

	return int64(len(data)), nil
}

// marshal serializes the index to bytes.
func (idx *Index) marshal() []byte {
	// Format:
	// 4 bytes: entry count
	// For each entry:
	//   64 bytes: fixed IndexEntry fields
	//   N bytes: key string
	var buf bytes.Buffer

	// Write count
	//nolint:gosec // G115: entry count bounded by volume capacity
	binary.Write(&buf, binary.LittleEndian, uint32(len(idx.entries)))

	// Write entries
	for _, entry := range idx.entries {
		// Fixed fields
		buf.Write(entry.KeyHash[:])
		binary.Write(&buf, binary.LittleEndian, entry.BlockNum)
		binary.Write(&buf, binary.LittleEndian, entry.BlockCount)
		binary.Write(&buf, binary.LittleEndian, entry.OffsetInBlock)
		binary.Write(&buf, binary.LittleEndian, entry.Size)
		binary.Write(&buf, binary.LittleEndian, entry.Created)
		binary.Write(&buf, binary.LittleEndian, entry.Flags)
		//nolint:gosec // G115: key length bounded by S3 key limits
		binary.Write(&buf, binary.LittleEndian, uint16(len(entry.Key)))
		// Variable key
		buf.WriteString(entry.Key)
	}

	return buf.Bytes()
}

// unmarshalIndex deserializes an index from bytes.
func unmarshalIndex(data []byte) (*Index, error) {
	if len(data) < minIndexHeaderSize {
		return nil, ErrIndexCorrupted
	}

	count := binary.LittleEndian.Uint32(data[0:4])
	idx := &Index{
		entries: make([]IndexEntryFull, 0, count),
	}

	r := bytes.NewReader(data[4:])

	for range count {
		var entry IndexEntryFull

		// Read fixed fields
		if _, err := io.ReadFull(r, entry.KeyHash[:]); err != nil {
			return nil, ErrIndexCorrupted
		}

		err := binary.Read(r, binary.LittleEndian, &entry.BlockNum)
		if err != nil {
			return nil, ErrIndexCorrupted
		}

		err = binary.Read(r, binary.LittleEndian, &entry.BlockCount)
		if err != nil {
			return nil, ErrIndexCorrupted
		}

		err = binary.Read(r, binary.LittleEndian, &entry.OffsetInBlock)
		if err != nil {
			return nil, ErrIndexCorrupted
		}

		err = binary.Read(r, binary.LittleEndian, &entry.Size)
		if err != nil {
			return nil, ErrIndexCorrupted
		}

		err = binary.Read(r, binary.LittleEndian, &entry.Created)
		if err != nil {
			return nil, ErrIndexCorrupted
		}

		err = binary.Read(r, binary.LittleEndian, &entry.Flags)
		if err != nil {
			return nil, ErrIndexCorrupted
		}

		err = binary.Read(r, binary.LittleEndian, &entry.KeyLen)
		if err != nil {
			return nil, ErrIndexCorrupted
		}

		// Read variable key
		keyBytes := make([]byte, entry.KeyLen)
		if _, err := io.ReadFull(r, keyBytes); err != nil {
			return nil, ErrIndexCorrupted
		}

		entry.Key = string(keyBytes)

		idx.entries = append(idx.entries, entry)
	}

	return idx, nil
}

// Get looks up an entry by key.
func (idx *Index) Get(bucket, key string) (*IndexEntryFull, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	fullKey := FullKey(bucket, key)
	keyHash := HashKey(bucket, key)

	// Binary search by key hash
	i := sort.Search(len(idx.entries), func(i int) bool {
		return bytes.Compare(idx.entries[i].KeyHash[:], keyHash[:]) >= 0
	})

	// Check for exact match (hash collision possible)
	for ; i < len(idx.entries); i++ {
		if idx.entries[i].KeyHash != keyHash {
			break
		}

		if idx.entries[i].Key == fullKey {
			if idx.entries[i].Flags&FlagDeleted != 0 {
				return nil, false // Deleted
			}

			entry := idx.entries[i]

			return &entry, true
		}
	}

	return nil, false
}

// Put adds or updates an entry in the index.
func (idx *Index) Put(entry IndexEntryFull) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	keyHash := entry.KeyHash

	// Binary search for insertion point
	i := sort.Search(len(idx.entries), func(i int) bool {
		return bytes.Compare(idx.entries[i].KeyHash[:], keyHash[:]) >= 0
	})

	// Check if entry already exists
	for j := i; j < len(idx.entries); j++ {
		if idx.entries[j].KeyHash != keyHash {
			break
		}

		if idx.entries[j].Key == entry.Key {
			// Update existing entry
			idx.entries[j] = entry
			return
		}
	}

	// Insert new entry
	idx.entries = append(idx.entries, IndexEntryFull{})
	copy(idx.entries[i+1:], idx.entries[i:])
	idx.entries[i] = entry
}

// Delete marks an entry as deleted.
func (idx *Index) Delete(bucket, key string) bool {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	fullKey := FullKey(bucket, key)
	keyHash := HashKey(bucket, key)

	i := sort.Search(len(idx.entries), func(i int) bool {
		return bytes.Compare(idx.entries[i].KeyHash[:], keyHash[:]) >= 0
	})

	for ; i < len(idx.entries); i++ {
		if idx.entries[i].KeyHash != keyHash {
			break
		}

		if idx.entries[i].Key == fullKey {
			idx.entries[i].Flags |= FlagDeleted
			return true
		}
	}

	return false
}

// Remove physically removes an entry from the index.
func (idx *Index) Remove(bucket, key string) bool {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	fullKey := FullKey(bucket, key)
	keyHash := HashKey(bucket, key)

	i := sort.Search(len(idx.entries), func(i int) bool {
		return bytes.Compare(idx.entries[i].KeyHash[:], keyHash[:]) >= 0
	})

	for ; i < len(idx.entries); i++ {
		if idx.entries[i].KeyHash != keyHash {
			break
		}

		if idx.entries[i].Key == fullKey {
			// Remove entry by shifting
			idx.entries = append(idx.entries[:i], idx.entries[i+1:]...)
			return true
		}
	}

	return false
}

// List returns entries matching a prefix.
func (idx *Index) List(bucket, prefix string, maxKeys int) []IndexEntryFull {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	fullPrefix := bucket + "/"
	if prefix != "" {
		fullPrefix += prefix
	}

	result := make([]IndexEntryFull, 0, maxKeys)

	for _, entry := range idx.entries {
		if entry.Flags&FlagDeleted != 0 {
			continue
		}

		if len(entry.Key) >= len(fullPrefix) && entry.Key[:len(fullPrefix)] == fullPrefix {
			result = append(result, entry)
			if len(result) >= maxKeys {
				break
			}
		}
	}

	return result
}

// Count returns the number of non-deleted entries.
func (idx *Index) Count() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	count := 0

	for _, entry := range idx.entries {
		if entry.Flags&FlagDeleted == 0 {
			count++
		}
	}

	return count
}

// TotalCount returns the total number of entries including deleted.
func (idx *Index) TotalCount() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	return len(idx.entries)
}

// Compact removes deleted entries from the index.
func (idx *Index) Compact() int {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	removed := 0
	j := 0

	for i := 0; i < len(idx.entries); i++ {
		if idx.entries[i].Flags&FlagDeleted == 0 {
			idx.entries[j] = idx.entries[i]
			j++
		} else {
			removed++
		}
	}

	idx.entries = idx.entries[:j]

	return removed
}

// All returns all non-deleted entries (for iteration).
func (idx *Index) All() []IndexEntryFull {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	result := make([]IndexEntryFull, 0, len(idx.entries))
	for _, entry := range idx.entries {
		if entry.Flags&FlagDeleted == 0 {
			result = append(result, entry)
		}
	}

	return result
}

// EntriesForBlock returns all entries stored in a specific block.
func (idx *Index) EntriesForBlock(blockNum uint32) []IndexEntryFull {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	result := make([]IndexEntryFull, 0)

	for _, entry := range idx.entries {
		if entry.BlockNum == blockNum && entry.Flags&FlagDeleted == 0 {
			result = append(result, entry)
		}
	}

	return result
}

// DeletedEntriesForBlock returns all deleted entries in a block.
func (idx *Index) DeletedEntriesForBlock(blockNum uint32) []IndexEntryFull {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	result := make([]IndexEntryFull, 0)

	for _, entry := range idx.entries {
		if entry.BlockNum == blockNum && entry.Flags&FlagDeleted != 0 {
			result = append(result, entry)
		}
	}

	return result
}
