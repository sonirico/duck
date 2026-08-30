package main

import (
	"testing"

	"github.com/moby/moby/api/types/image"
	"github.com/stretchr/testify/require"
)

func TestNewImageFromSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   image.Summary
		want Image
	}{
		{
			name: "with repo tag",
			in: image.Summary{
				ID:       "sha256:abc",
				RepoTags: []string{"nginx:latest"},
				Size:     1024,
			},
			want: Image{
				ID:      "sha256:abc",
				RepoTag: "nginx:latest",
				Size:    1024,
			},
		},
		{
			name: "without repo tags",
			in: image.Summary{
				ID:   "sha256:def",
				Size: 2048,
			},
			want: Image{
				ID:      "sha256:def",
				RepoTag: "<none>",
				Size:    2048,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := newImageFromSummary(tc.in)

			require.Equal(t, tc.want, got)
		})
	}
}

func TestImageUsedBy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		images     []Image
		containers []Container
		want       map[string]int
	}{
		{
			name:       "image without usage",
			images:     []Image{{ID: "img1"}},
			containers: []Container{{ID: "1", Name: "app"}},
			want:       map[string]int{"img1": 0},
		},
		{
			name:   "image used by two containers",
			images: []Image{{ID: "img1"}},
			containers: []Container{
				{ID: "1", Name: "app", ImageID: "img1"},
				{ID: "2", Name: "worker", ImageID: "img1"},
			},
			want: map[string]int{"img1": 2},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := imageUsedBy(tc.images, tc.containers)

			require.Equal(t, tc.want, got)
		})
	}
}

func TestFormatImageSize(t *testing.T) {
	t.Parallel()

	got := formatImageSize(125_300_000)

	require.Equal(t, "125.3MB", got)
}
