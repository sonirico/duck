package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatVolumeRow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		vol  Volume
		used int
		want string
	}{
		{
			name: "unused volume",
			vol:  Volume{Name: "data", Driver: "local"},
			used: 0,
			want: "data  local  used-by:0",
		},
		{
			name: "volume used by two containers",
			vol:  Volume{Name: "cache", Driver: "local"},
			used: 2,
			want: "cache  local  used-by:2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := formatVolumeRow(tc.vol, tc.used)

			require.Equal(t, tc.want, got)
		})
	}
}
