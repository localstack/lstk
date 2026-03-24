//go:build !darwin && !linux

package cmd

import "os"

func readEscapeSequence(in *os.File) (bool, error) {
	return true, nil
}
