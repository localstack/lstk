package cli

import "strings"

// DeactivateAccessKey neutralizes a real-looking AWS access key id so a live
// credential is never baked into the generated override file nor sent to
// LocalStack (where it could be captured by analytics by accident).
//
// AWS access key ids begin with an "A" (e.g. "AKIA…" for long-term keys,
// "ASIA…" for temporary session keys). Replacing that leading "A" with "L"
// renders the key inert while leaving obviously-mock values (e.g. "test", or a
// 12-digit account id) untouched. This mirrors tflocal's deactivate_access_key
// safeguard — see https://docs.localstack.cloud/references/credentials/.
func DeactivateAccessKey(accessKey string) string {
	if strings.HasPrefix(accessKey, "A") {
		return "L" + accessKey[1:]
	}
	return accessKey
}
