package mcpconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// applyServerEntry inserts entry at root[rootPath...][serverName], creating any
// missing intermediate objects and preserving every other key. Existing JSON is
// reparsed and reformatted on write; comments (JSONC) are not preserved.
func applyServerEntry(existing []byte, rootPath []string, serverName string, entry map[string]any) ([]byte, error) {
	root := map[string]any{}
	if len(bytes.TrimSpace(existing)) > 0 {
		if err := json.Unmarshal(existing, &root); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
		if root == nil {
			root = map[string]any{}
		}
	}

	cur := root
	for _, key := range rootPath {
		existing, present := cur[key]
		if !present {
			child := map[string]any{}
			cur[key] = child
			cur = child
			continue
		}
		// Present but not an object: refuse to clobber the user's data, matching
		// the conservative handling of an unparseable top-level document.
		child, ok := existing.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%q is not an object", key)
		}
		cur = child
	}
	cur[serverName] = entry

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// hasServerEntry reports whether serverName already exists under rootPath.
func hasServerEntry(existing []byte, rootPath []string, serverName string) bool {
	if len(bytes.TrimSpace(existing)) == 0 {
		return false
	}
	var root map[string]any
	if err := json.Unmarshal(existing, &root); err != nil {
		return false
	}
	cur := root
	for _, key := range rootPath {
		child, ok := cur[key].(map[string]any)
		if !ok {
			return false
		}
		cur = child
	}
	_, ok := cur[serverName]
	return ok
}
