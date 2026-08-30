package main

import (
	"fmt"

	"github.com/moby/moby/api/types/image"
)

type Image struct {
	ID      string
	RepoTag string
	Size    int64
}

type imagesMsg struct {
	images []Image
}

func newImageFromSummary(s image.Summary) Image {
	repoTag := "<none>"
	if len(s.RepoTags) > 0 {
		repoTag = s.RepoTags[0]
	}
	return Image{
		ID:      s.ID,
		RepoTag: repoTag,
		Size:    s.Size,
	}
}

func imageUsedBy(images []Image, containers []Container) map[string]int {
	keys := make([]string, 0, len(images))
	for _, i := range images {
		keys = append(keys, i.ID)
	}
	return usedByNames(keys, containers, func(c Container) []string { return []string{c.ImageID} })
}

func formatImageSize(size int64) string {
	return fmt.Sprintf("%.1fMB", float64(size)/1e6)
}
