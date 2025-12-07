package util

import "slices"

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

func IsProcessingActive(state ProcessingState) bool {
	processingStates := []ProcessingState{
		ProcessingChunks,
		AwaitingFinalization,
		AwaitingToolCallResult,
		Finalized,
	}
	return slices.Contains(processingStates, state)
}
