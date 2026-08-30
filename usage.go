package main

func usedByNames(keys []string, containers []Container, pick func(Container) []string) map[string]int {
	used := make(map[string]int, len(keys))
	for _, k := range keys {
		used[k] = 0
	}
	for _, c := range containers {
		for _, name := range pick(c) {
			if _, ok := used[name]; ok {
				used[name]++
			}
		}
	}
	return used
}
