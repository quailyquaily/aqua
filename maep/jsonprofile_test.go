package maep

import "testing"

func TestValidateStrictJSONProfile(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "valid", raw: `{"a":1,"b":[2,3],"c":{"d":"x"}}`, wantErr: false},
		{name: "float", raw: `{"a":1.25}`, wantErr: true},
		{name: "null", raw: `{"a":null}`, wantErr: true},
		{name: "duplicate key", raw: `{"a":1,"a":2}`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateStrictJSONProfile([]byte(tc.raw))
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
