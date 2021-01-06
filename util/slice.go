package util

// StringSliceContains checks if  string slice contains given string
func StringSliceContains(slice []string, str string) bool {
	for _, a := range slice {
		if a == str {
			return true
		}
	}
	return false
}

// DiffMapAgainstStringSlice compares the "b" slice against keys that exist in
// the "a" map and returns the key differences.
// Any keys that differ in the "a" map are appended onto a []string which is
// returned.
func DiffMapAgainstStringSlice(a map[string]string, b []string) []string {
	mb := make(map[string]struct{}, len(b))
	for _, x := range b {
		mb[x] = struct{}{}
	}
	var diff []string
	for k := range a {
		if _, found := mb[k]; !found {
			diff = append(diff, k)
		}
	}
	return diff
}
