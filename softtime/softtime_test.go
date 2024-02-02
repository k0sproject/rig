package softtime

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMinPrecision(t *testing.T) {
	testCases := []struct {
		name     string
		timeA    time.Time
		timeB    time.Time
		expected time.Duration
	}{
		{
			name:     "BothTimesHaveSecondsPrecision",
			timeA:    time.Date(2022, 1, 1, 12, 0, 1, 0, time.UTC),
			timeB:    time.Date(2022, 1, 1, 12, 0, 2, 0, time.UTC),
			expected: time.Second,
		},
		{
			name:     "BothTimesHaveMicrosecondPrecision",
			timeA:    time.Date(2022, 1, 1, 12, 0, 1, 0, time.UTC).Add(time.Microsecond),
			timeB:    time.Date(2022, 1, 1, 12, 0, 2, 0, time.UTC).Add(time.Microsecond),
			expected: time.Microsecond,
		},
		{
			name:     "BothTimesHaveMillisecondPrecision",
			timeA:    time.Date(2022, 1, 1, 12, 0, 1, 0, time.UTC).Add(time.Millisecond),
			timeB:    time.Date(2022, 1, 1, 12, 0, 2, 0, time.UTC).Add(time.Millisecond),
			expected: time.Millisecond,
		},
		{
			name:     "BothTimesHaveNanosecondPrecision",
			timeA:    time.Date(2022, 1, 1, 12, 0, 1, 1, time.UTC),
			timeB:    time.Date(2022, 1, 1, 12, 0, 2, 1, time.UTC),
			expected: time.Nanosecond,
		},
		{
			name:     "SecondAndMillisecondPrecision",
			timeA:    time.Date(2022, 1, 1, 12, 0, 1, 0, time.UTC).Add(time.Millisecond),
			timeB:    time.Date(2022, 1, 1, 12, 0, 2, 0, time.UTC),
			expected: time.Second,
		},
		{
			name:     "MillisecondAndMicrosecondPrecision",
			timeA:    time.Date(2022, 1, 1, 12, 0, 1, 0, time.UTC).Add(time.Microsecond),
			timeB:    time.Date(2022, 1, 1, 12, 0, 2, 0, time.UTC).Add(time.Millisecond),
			expected: time.Millisecond,
		},
		{
			name:     "MillisecondAndNanosecondPrecision",
			timeA:    time.Date(2022, 1, 1, 12, 0, 1, 0, time.UTC).Add(time.Millisecond),
			timeB:    time.Date(2022, 1, 1, 12, 0, 2, 1, time.UTC),
			expected: time.Millisecond,
		},
		{
			name:     "MicrosecondAndNanosecondPrecision",
			timeA:    time.Date(2022, 1, 1, 12, 0, 1, 0, time.UTC).Add(time.Microsecond),
			timeB:    time.Date(2022, 1, 1, 12, 0, 2, 1, time.UTC),
			expected: time.Microsecond,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, MinPrecision(tc.timeA, tc.timeB), "MinPrecision(%v, %v)", tc.timeA, tc.timeB)
		})
	}
}

func TestTimeEqual(t *testing.T) {
	testCases := []struct {
		name     string
		timeA    time.Time
		timeB    time.Time
		expected bool
	}{
		{
			name:     "ExactEqual",
			timeA:    time.Date(2022, 1, 1, 12, 0, 0, 1, time.UTC),
			timeB:    time.Date(2022, 1, 1, 12, 0, 0, 1, time.UTC),
			expected: true,
		},
		{
			name:     "EqualWithDifferentNanoseconds",
			timeA:    time.Date(2022, 1, 1, 12, 0, 0, 1, time.UTC),
			timeB:    time.Date(2022, 1, 1, 12, 0, 0, 2, time.UTC),
			expected: false,
		},
		{
			name:     "NotEqualSeconds",
			timeA:    time.Date(2022, 1, 1, 12, 0, 0, 0, time.UTC),
			timeB:    time.Date(2022, 1, 1, 12, 0, 1, 0, time.UTC),
			expected: false,
		},
		{
			name:     "EqualMilliDifferentMicroseconds",
			timeA:    time.Date(2022, 1, 1, 12, 0, 0, 0, time.UTC).Add(time.Millisecond).Add(time.Microsecond),
			timeB:    time.Date(2022, 1, 1, 12, 0, 0, 0, time.UTC).Add(time.Millisecond),
			expected: true,
		},
		{
			name:     "EqualMicroDifferentNanoseconds",
			timeA:    time.Date(2022, 1, 1, 12, 0, 0, 0, time.UTC).Add(time.Microsecond).Add(time.Nanosecond),
			timeB:    time.Date(2022, 1, 1, 12, 0, 0, 0, time.UTC).Add(time.Microsecond),
			expected: true,
		},
		{
			name:     "EqualMilli",
			timeA:    time.Date(2022, 1, 1, 12, 0, 0, 0, time.UTC).Add(150 * time.Millisecond),
			timeB:    time.Date(2022, 1, 1, 12, 0, 0, 0, time.UTC).Add(150 * time.Millisecond),
			expected: true,
		},
		{
			name:     "DifferentMilli",
			timeA:    time.Date(2022, 1, 1, 12, 0, 0, 0, time.UTC).Add(151 * time.Millisecond),
			timeB:    time.Date(2022, 1, 1, 12, 0, 0, 0, time.UTC).Add(150 * time.Millisecond),
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Equal(tc.timeA, tc.timeB), "TimeEqual(%v, %v), min precision: %v", tc.timeA, tc.timeB, MinPrecision(tc.timeA, tc.timeB))
		})
	}
}

func TestBeforeAfter(t *testing.T) {
	t.Run("EqualTime", func(t *testing.T) {
		equalTime := time.Date(2022, 1, 1, 12, 0, 1, 0, time.UTC)
		assert.False(t, Before(equalTime, equalTime))
		assert.False(t, After(equalTime, equalTime))
	})

	testCases := []struct {
		name     string
		timeA    time.Time
		timeB    time.Time
		expected bool // true if timeA is expected to be before timeB
	}{
		{
			name:  "MicrosecondPrecisionDifferent",
			timeA: time.Date(2022, 1, 1, 12, 0, 1, 500000, time.UTC),
			timeB: time.Date(2022, 1, 1, 12, 0, 1, 600000, time.UTC),
		},
		{
			name:  "MillisecondPrecisionSameSecond",
			timeA: time.Date(2022, 1, 1, 12, 0, 1, 1000000, time.UTC),
			timeB: time.Date(2022, 1, 1, 12, 0, 1, 2000000, time.UTC),
		},
		{
			name:  "NanosecondPrecisionDifferent",
			timeA: time.Date(2022, 1, 1, 12, 0, 1, 1001, time.UTC),
			timeB: time.Date(2022, 1, 1, 12, 0, 1, 1002, time.UTC),
		},
		{
			name:  "SecondPrecisionDifferentMinute",
			timeA: time.Date(2022, 1, 1, 12, 1, 0, 0, time.UTC),
			timeB: time.Date(2022, 1, 1, 12, 2, 0, 0, time.UTC),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.True(t, Before(tc.timeA, tc.timeB))
			assert.False(t, After(tc.timeA, tc.timeB))
			assert.False(t, Before(tc.timeB, tc.timeA))
			assert.True(t, After(tc.timeB, tc.timeA))
		})
	}
}
