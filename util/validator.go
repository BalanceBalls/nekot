package util

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
)

const multiplier = 100000
const negativeSignCode = -9999999999

const FrequencyRange = "[-2.0, 2.0)"
const TemperatureRange = "[0.0, 2.0]"
const TopPRange = "[0.0, 1.0]"

var EmptyValidator = func(input string) error {
	return nil
}
var DeleteSessionValidator = func(input string) error {
	allowed := []string{"y", "n"}
	if len(input) > 1 || !slices.Contains(allowed, input) {
		return errors.New("Invalid input")
	}
	return nil
}
var FrequencyValidator = func(input string) error {
	return validateRangedFloat(input, -2.0, 2.0, false, true)
}
var TemperatureValidator = func(input string) error {
	return validateRangedFloat(input, 0.0, 2.0, false, false)
}
var TopPValidator = func(input string) error {
	return validateRangedFloat(input, 0.0, 1.0, false, false)
}
var MaxTokensValidator = func(input string) error {
	if input == "" {
		return nil
	}

	min := 0
	max := 1_000_000
	val, err := strconv.Atoi(input)
	if err != nil {
		return err
	}

	if val <= min || val > max {
		logOutOfRange(val, min, max)
		return fmt.Errorf("value %d out of range (%d, %d)", val, min, max)
	}

	return nil
}

func validateFloat(input string, allowNegative bool) (float64, error) {
	if allowNegative && len(input) == 1 && input[0] == '-' {
		return negativeSignCode, nil
	}

	value, err := strconv.ParseFloat(input, 64)
	if err != nil {
		Slog.Error("input is not a floating-point number", "input", input)
		return -1, err
	}

	return value, nil
}

func isLess(value float64, min float64, isStrict bool) bool {
	intValue := int(value * multiplier)
	intMin := int(min * multiplier)
	if isStrict {
		return intValue <= intMin
	}
	return intValue < intMin
}

func isGreater(value float64, max float64, isStrict bool) bool {
	intValue := int(value * multiplier)
	intMax := int(max * multiplier)
	if isStrict {
		return intValue >= intMax
	}
	return intValue > intMax
}

// If strict is true, the value should be strictly less or more than threshold
func validateRangedFloat(input string, min, max float64, minStrict, maxStrict bool) error {
	if input == "" {
		return nil
	}

	allowNegative := isGreater(0, min, false)

	value, err := validateFloat(input, allowNegative)
	if math.Abs(value-negativeSignCode) <= 1e-9 {
		return nil
	}

	if isLess(value, min, minStrict) {
		logOutOfRange(value, min, max)
		return fmt.Errorf("value %f out of range (%f, %f)", value, min, max)
	}

	if isGreater(value, max, maxStrict) {
		logOutOfRange(value, min, max)
		return fmt.Errorf("value %f out of range (%f, %f)", value, min, max)
	}

	return err
}

func logOutOfRange(value any, min any, max any) {
	Slog.Error(
		"value out of range",
		"value",
		value,
		"range min",
		min,
		"range max",
		max,
	)
}
