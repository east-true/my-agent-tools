package github

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

type cacheConfig struct{ Root, Scope string }
type cacheProvider interface{ cacheConfiguration() cacheConfig }

func (client Client) cacheConfiguration() cacheConfig {
	if client.cache.Root != "" {
		return client.cache
	}
	if provider, ok := client.API.(cacheProvider); ok {
		return provider.cacheConfiguration()
	}
	return cacheConfig{}
}

func cacheHash(value []byte) string { return fmt.Sprintf("%x", sha256.Sum256(value)) }
func (config cacheConfig) file(bucket, key string) string {
	return filepath.Join(config.Root, cacheHash([]byte(config.Scope)), bucket, "cache-"+cacheHash([]byte(key))+".json")
}

type cacheEnvelope struct {
	Version  int             `json:"version"`
	Key      string          `json:"key"`
	Scope    string          `json:"scope"`
	StoredAt time.Time       `json:"stored_at"`
	ValueSHA string          `json:"value_sha256"`
	Value    json.RawMessage `json:"value"`
}

func (config cacheConfig) load(bucket, key string, ttl time.Duration, target any) bool {
	if config.Root == "" || config.Scope == "" {
		return false
	}
	path := config.file(bucket, key)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 32<<20 {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var record cacheEnvelope
	if json.Unmarshal(data, &record) != nil || record.Version != 1 || record.Key != cacheHash([]byte(key)) || record.Scope != cacheHash([]byte(config.Scope)) || record.StoredAt.After(time.Now().Add(time.Minute)) || time.Since(record.StoredAt) > ttl || record.ValueSHA != cacheHash(record.Value) {
		return false
	}
	return json.Unmarshal(record.Value, target) == nil
}

// Cache failures never replace a successful API result. Only managed cache
// files are pruned; authentication tokens are never serialized.
func (config cacheConfig) save(bucket, key string, ttl time.Duration, value any) {
	if config.Root == "" || config.Scope == "" {
		return
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 24<<20 {
		return
	}
	record := cacheEnvelope{1, cacheHash([]byte(key)), cacheHash([]byte(config.Scope)), time.Now().UTC(), cacheHash(raw), raw}
	data, err := json.Marshal(record)
	if err != nil {
		return
	}
	path := config.file(bucket, key)
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".cache-*.json")
	if err != nil {
		return
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil || os.Rename(file.Name(), path) != nil {
		return
	}
	config.prune(filepath.Dir(path), path, ttl)
}

var cacheFileName = regexp.MustCompile(`^cache-[a-f0-9]{64}\.json$`)

func (config cacheConfig) prune(dir, current string, ttl time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type candidate struct {
		path     string
		modified time.Time
	}
	files := []candidate{}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !cacheFileName.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if path == current {
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		prefix, _ := io.ReadAll(io.LimitReader(file, 512))
		file.Close()
		boundary := bytes.Index(prefix, []byte(`,"value":`))
		if boundary < 0 {
			continue
		}
		var metadata cacheEnvelope
		if json.Unmarshal(append(prefix[:boundary:boundary], '}'), &metadata) != nil || metadata.Version != 1 || metadata.Scope != cacheHash([]byte(config.Scope)) || entry.Name() != "cache-"+metadata.Key+".json" {
			continue
		}
		info, err := entry.Info()
		if err == nil {
			files = append(files, candidate{path, info.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modified.After(files[j].modified) })
	kept := 1
	for _, file := range files {
		if time.Since(file.modified) > ttl || kept >= 200 {
			_ = os.Remove(file.path)
		} else {
			kept++
		}
	}
}
