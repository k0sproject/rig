// Package softtime provides time comparison functions that work with times up to the finest common precision.
// such as a files modification time in a file system and a time.Time value from time.Now(). Slight warning:
// there's a chance of false positives when comparing times from different sources especially when the
// one of the times happen to be at the exact boundary of the precision.
package softtime

import (
	"time"
)

// Precision returns the precision of the given time as a time.Duration.
func Precision(t time.Time) time.Duration {
	if t != t.Truncate(time.Second) {
		if t != t.Truncate(time.Millisecond) {
			if t != t.Truncate(time.Microsecond) {
				return time.Nanosecond
			}
			return time.Microsecond
		}
		return time.Millisecond
	}
	return time.Second
}

// MinPrecision returns the maximum precision of the given two times.
func MinPrecision(a, b time.Time) time.Duration {
	precA, precB := Precision(a), Precision(b)
	if precA > precB {
		return precA
	}
	return precB
}

// Equal returns true if the times are equal up to common time resolution
func Equal(timeA, timeB time.Time) bool {
	timeA, timeB = Truncate(timeA, timeB)
	return timeA.Equal(timeB)
}

// Before returns true if a is before b up to common time resolution
func Before(a, b time.Time) bool {
	a, b = Truncate(a, b)
	return a.Before(b)
}

// After returns true if a is after b up to common time resolution
func After(a, b time.Time) bool {
	a, b = Truncate(a, b)
	return a.After(b)
}

// Truncate both of the given times to the common max precision.
func Truncate(a, b time.Time) (newA time.Time, newB time.Time) { //nolint:nonamedreturns // for doc clarity
	precision := MinPrecision(a, b)
	return a.Truncate(precision), b.Truncate(precision)
}
