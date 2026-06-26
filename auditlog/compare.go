package auditlog

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
)

type compMap map[string]any

// DiffRecord is one version in a model's history together with the fields that
// changed relative to the previous version. It is returned by
// Core.QueryDiffAllVersionByModelID.
type DiffRecord struct {
	ID            int
	ModelID       string
	ModelType     string
	Method        string
	Path          string
	Version       int
	UpdatedBy     *string
	UpdatedAt     *time.Time
	ChangedFields []Field
}

// Field is a single changed attribute between two versions.
type Field struct {
	Key      string
	OldValue any
	NewValue any
}

// normalizeMap decodes JSON into a map and strips the volatile audit fields
// (updated_at/updated_by) so two snapshots that differ only in those are equal.
func normalizeMap(jsonData []byte) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(jsonData, &m); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	removeKeys(m, "updated_at")
	removeKeys(m, "updated_by")
	return m, nil
}

// normalizeJSON is normalizeMap re-encoded to JSON.
func normalizeJSON(jsonData []byte) ([]byte, error) {
	m, err := normalizeMap(jsonData)
	if err != nil {
		return nil, err
	}
	normalizedJSON, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return normalizedJSON, nil
}

// payloadsEqual reports whether two JSON payloads are equal after normalization.
func payloadsEqual(a, b []byte) (bool, error) {
	na, err := normalizeMap(a)
	if err != nil {
		return false, err
	}
	nb, err := normalizeMap(b)
	if err != nil {
		return false, err
	}
	return cmp.Equal(na, nb), nil
}

// removeKeys recursively traverses the map and removes any specified keys
func removeKeys(data any, keyToRemove string) {
	switch v := data.(type) {
	case map[string]any:
		// If it's a map, check for the key and remove it
		for key, value := range v {
			if key == keyToRemove {
				delete(v, key)
			} else {
				// If the value is a map or slice, deep dive into it
				removeKeys(value, keyToRemove)
			}
		}
	case []any:
		// If it's an array, recursively process each element
		for _, item := range v {
			removeKeys(item, keyToRemove)
		}
	}
}

func diff(curVer, prevVer []byte) (string, error) {
	var err error

	var resultCur compMap
	err = json.Unmarshal(curVer, &resultCur)
	if err != nil {
		return "", err
	}

	var resultPrev compMap
	err = json.Unmarshal(prevVer, &resultPrev)
	if err != nil {
		return "", err
	}

	if cmp.Equal(resultCur, resultPrev) {
		return "", nil
	}

	diff := cmp.Diff(resultCur, resultPrev)
	return diff, nil
}

// NormalizeDiff save line only contains the + sing and minus sign in the begiiing of the lines
// all other lines are removed
func (c *Core) NormalizeDiff(diff string) string {
	var result strings.Builder
	for line := range strings.SplitSeq(diff, "\n") {
		if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
			result.WriteString(line)
			result.WriteString("\n")
		}
	}
	return result.String()
}

// payloadData holds parsed JSON as a map for field-level comparison.
type payloadData map[string]any

// isArray reports whether v decoded to a JSON array.
func isArray(v any) bool {
	switch v.(type) {
	case []any:
		return true
	default:
		return false
	}
}

func unmarshalJSON(data string) (payloadData, error) {
	var result payloadData
	err := json.Unmarshal([]byte(data), &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func compareJSON(prev, curr payloadData) map[string]map[string]any {
	changes := make(map[string]map[string]any)

	// Iterate over the keys of the current version
	for key, currValue := range curr {
		// If the key exists in the previous version and the value is not an array
		if prevValue, exists := prev[key]; exists {
			// Compare only simple types (non-arrays)
			if !isArray(currValue) && !reflect.DeepEqual(prevValue, currValue) {
				// Store the changed key and its values
				if changes[key] == nil {
					changes[key] = make(map[string]any)
				}
				changes[key]["old_value"] = prevValue
				changes[key]["new_value"] = currValue
			}
		}
	}

	return changes
}
