package httpapi

import "strings"

func isResourceUploadURL(raw string) bool {
	url := strings.TrimSpace(raw)
	if url == "" {
		return false
	}
	lower := strings.ToLower(url)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		idx := strings.Index(lower, "/uploads/resources/")
		idx2 := strings.Index(lower, "/v1/uploads/resources/")
		return idx >= 0 || idx2 >= 0
	}
	return strings.HasPrefix(lower, "/uploads/resources/") || strings.HasPrefix(lower, "/v1/uploads/resources/")
}

