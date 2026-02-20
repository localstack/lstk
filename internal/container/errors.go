package container

import (
	"fmt"
	"strings"

	"github.com/containerd/errdefs"
)

func formatContainerNameConflict(name string, err error) error {
	if err == nil {
		return nil
	}

	msg := strings.ToLower(err.Error())
	if errdefs.IsConflict(err) || strings.Contains(msg, "already in use by container") {
		return fmt.Errorf(
			"container name conflict: %q already exists\n"+
				"run `lstk stop` or remove it manually with `docker rm -f %s`",
			name, name,
		)
	}

	return err
}
