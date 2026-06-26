package auditlog

import (
	"encoding/json"
	"testing"
)

func TestNormalizeJSON(t *testing.T) {
	tests := []struct {
		name           string
		input          []byte
		expectedOutput []byte
		expectError    bool
	}{
		{
			name: "valid JSON with fields to delete",
			input: []byte(`{
				"id": 1,
				"name": "Test",
				"updated_at": "2023-10-10T12:00:00Z",
				"updated_by": "user123"
			}`),
			expectedOutput: []byte(`{"id":1,"name":"Test"}`),
			expectError:    false,
		},
		{
			name: "valid JSON without fields to delete",
			input: []byte(`{
				"id": 2,
				"name": "Another Test"
			}`),
			expectedOutput: []byte(`{"id":2,"name":"Another Test"}`),
			expectError:    false,
		},
		{
			name:        "invalid JSON",
			input:       []byte(`{ "id": 1, "name":`), // broken JSON
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := normalizeJSON(tt.input)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected an error, but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("didn't expect an error, but got %v", err)
				}
				var expectedMap, outputMap map[string]any
				if err := json.Unmarshal(tt.expectedOutput, &expectedMap); err != nil {
					t.Fatalf("failed to unmarshal expected output: %v", err)
				}
				if err := json.Unmarshal(output, &outputMap); err != nil {
					t.Fatalf("failed to unmarshal output: %v", err)
				}
				if len(expectedMap) != len(outputMap) {
					t.Errorf("expected output %v, got %v", expectedMap, outputMap)
				}
			}
		})
	}
}
