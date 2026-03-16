package aws

import "strings"

// extractResourceName extracts the name from a resource ID.
// For ARNs (arn:partition:service:region:account:resource), it returns the resource part.
// For plain names, it returns the ID as-is.
func extractResourceName(id string) string {
	if strings.HasPrefix(id, "arn:") {
		parts := strings.SplitN(id, ":", 6)
		if len(parts) == 6 {
			resource := parts[5]
			// Handle resources like "role/my-role"
			if idx := strings.LastIndex(resource, "/"); idx != -1 {
				return resource[idx+1:]
			}
			// Handle resources like "function:my-func"
			if idx := strings.LastIndex(resource, ":"); idx != -1 {
				return resource[idx+1:]
			}
			return resource
		}
	}
	return id
}
