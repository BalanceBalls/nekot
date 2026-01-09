package util

import (
	"slices"
	"sort"
)

func RemoveDuplicates[T comparable](slice []T) []T {
	if len(slice) == 0 {
		return []T{}
	}

	seen := make(map[T]struct{}, len(slice))
	result := make([]T, 0, len(slice))

	for _, element := range slice {
		if _, ok := seen[element]; !ok {
			seen[element] = struct{}{}
			result = append(result, element)
		}
	}

	return result
}

func SortByNumberDesc[T any, V int | int64 | float64](slice []T, keyFunc func(T) V) {
	sort.Slice(slice, func(i, j int) bool {
		return keyFunc(slice[i]) > keyFunc(slice[j])
	})
}

func IsProcessingActive(state ProcessingState) bool {
	processingStates := []ProcessingState{
		ProcessingChunks,
		AwaitingToolCallResult,
		AwaitingFinalization,
		Finalized,
	}
	return slices.Contains(processingStates, state)
}
