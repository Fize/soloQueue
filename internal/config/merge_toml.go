package config

import (
	"fmt"

	"github.com/pelletier/go-toml/v2"
)

// MergeTOML partially merges src TOML bytes into dst and returns the new value
//
// Merge strategy:
//   - Object fields: override one by one (partial merge, unspecified fields retain upper-level values)
//   - Array fields: replace entirely (not append)
//   - null / missing fields: skip, retain upper-level values
//
// Implementation: serialize dst to TOML, parse src into map,
// recursively merge, then deserialize back to T.
func MergeTOML[T any](dst T, src []byte) (T, error) {
	// Serialize dst into generic map
	dstBytes, err := toml.Marshal(dst)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("MergeTOML: marshal dst: %w", err)
	}

	var dstMap map[string]any
	if err := toml.Unmarshal(dstBytes, &dstMap); err != nil {
		var zero T
		return zero, fmt.Errorf("MergeTOML: unmarshal dst map: %w", err)
	}

	// Parse src
	var srcMap map[string]any
	if err := toml.Unmarshal(src, &srcMap); err != nil {
		var zero T
		return zero, fmt.Errorf("MergeTOML: unmarshal src: %w", err)
	}

	// Recursively merge
	mergeMapTOML(dstMap, srcMap)

	// Serialize merged result, deserialize back to T
	merged, err := toml.Marshal(dstMap)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("MergeTOML: marshal merged: %w", err)
	}

	var result T
	if err := toml.Unmarshal(merged, &result); err != nil {
		var zero T
		return zero, fmt.Errorf("MergeTOML: unmarshal result: %w", err)
	}

	return result, nil
}

// mergeMapTOML recursively merges src into dst (modifies dst in-place)
//
// Rules:
//   - Object fields in src recursively merge into corresponding dst fields
//   - Array fields in src completely replace corresponding dst fields
//   - null values in src are skipped (preserve dst original values)
//   - Other types (string/number/bool) are directly overwritten
func mergeMapTOML(dst, src map[string]any) {
	for k, srcVal := range src {
		// null skip
		if srcVal == nil {
			continue
		}

		srcObj, srcIsObj := srcVal.(map[string]any)
		dstVal, exists := dst[k]

		if srcIsObj && exists {
			dstObj, dstIsObj := dstVal.(map[string]any)
			if dstIsObj {
				// Both sides are objects: recursively merge
				mergeMapTOML(dstObj, srcObj)
				continue
			}
		}

		// Arrays, scalars, new keys: directly overwrite
		dst[k] = srcVal
	}
}
