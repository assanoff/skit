package dbx

import (
	"reflect"
	"testing"
)

func TestJSONSliceValue(t *testing.T) {
	tests := []struct {
		name string
		in   JSONSlice[string]
		want string
	}{
		{name: "nil is empty array", in: nil, want: "[]"},
		{name: "empty is empty array", in: JSONSlice[string]{}, want: "[]"},
		{name: "one element", in: JSONSlice[string]{"a"}, want: `["a"]`},
		{name: "many elements", in: JSONSlice[string]{"a", "b", "c"}, want: `["a","b","c"]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := tt.in.Value()
			if err != nil {
				t.Fatalf("Value() error: %v", err)
			}
			b, ok := v.([]byte)
			if !ok {
				t.Fatalf("Value() returned %T, want []byte", v)
			}
			if string(b) != tt.want {
				t.Fatalf("Value() = %q, want %q", b, tt.want)
			}
		})
	}
}

func TestJSONSliceScan(t *testing.T) {
	tests := []struct {
		name    string
		src     any
		want    JSONSlice[string]
		wantErr bool
	}{
		{name: "nil -> nil", src: nil, want: nil},
		{name: "empty bytes -> nil", src: []byte{}, want: nil},
		{name: "empty array bytes", src: []byte("[]"), want: JSONSlice[string]{}},
		{name: "bytes payload", src: []byte(`["a","b"]`), want: JSONSlice[string]{"a", "b"}},
		{name: "string payload", src: `["x"]`, want: JSONSlice[string]{"x"}},
		{name: "unsupported type", src: 42, wantErr: true},
		{name: "malformed json", src: []byte(`{`), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got JSONSlice[string]
			err := got.Scan(tt.src)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Scan(%v) expected error, got nil", tt.src)
				}
				return
			}
			if err != nil {
				t.Fatalf("Scan(%v) error: %v", tt.src, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Scan(%v) = %#v, want %#v", tt.src, got, tt.want)
			}
		})
	}
}

// TestJSONSliceRoundTrip checks Value -> Scan preserves the slice for a
// non-string element type, exercising the generic beyond []string.
func TestJSONSliceRoundTrip(t *testing.T) {
	in := JSONSlice[int64]{1, 2, 3}

	v, err := in.Value()
	if err != nil {
		t.Fatalf("Value() error: %v", err)
	}

	var out JSONSlice[int64]
	if err := out.Scan(v); err != nil {
		t.Fatalf("Scan() error: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip = %#v, want %#v", out, in)
	}
}
