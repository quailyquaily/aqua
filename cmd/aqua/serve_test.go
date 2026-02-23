package main

import "testing"

func TestResolveRelayProbeEnabledFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    bool
		wantErr bool
	}{
		{name: "empty disables", value: "", want: false},
		{name: "true", value: "true", want: true},
		{name: "1", value: "1", want: true},
		{name: "yes", value: "yes", want: true},
		{name: "on", value: "on", want: true},
		{name: "false", value: "false", want: false},
		{name: "0", value: "0", want: false},
		{name: "no", value: "no", want: false},
		{name: "off", value: "off", want: false},
		{name: "invalid", value: "maybe", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envRelayProbe, tt.value)
			got, err := resolveRelayProbeEnabledFromEnv()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveRelayProbeEnabledFromEnv() expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveRelayProbeEnabledFromEnv() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("resolveRelayProbeEnabledFromEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}
