package main

// safeStr returns an empty string if the pointer is nil, or the underlying string value if not nil.
func safeStr(str *string) string {
	if str == nil {
		return ""
	} else {
		return *str
	}
}
