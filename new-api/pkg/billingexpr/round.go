package billingexpr

import (
	"fmt"
	"math"
)

// QuotaRound converts a float64 quota value to int using half-away-from-zero
// rounding. Every tiered billing path (pre-consume, settlement, breakdown
// validation, log fields) MUST use this function to avoid +-1 discrepancies.
func QuotaRound(f float64) int {
	return int(math.Round(f))
}

// QuotaRoundStrict rejects an unrepresentable pre-consume estimate.
// Returns an error when the value is NaN or exceeds int32 range.
func QuotaRoundStrict(f float64) (int, error) {
	if math.IsNaN(f) {
		return 0, fmt.Errorf("quota conversion: NaN")
	}
	if f >= math.MaxInt32 || f <= math.MinInt32 {
		return 0, fmt.Errorf("quota conversion overflow: %g", f)
	}
	return int(math.Round(f)), nil
}
