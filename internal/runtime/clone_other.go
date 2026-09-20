//go:build !darwin && !linux

package runtime

import "errors"

func platformCloneFile(src, dst string) error {
	return errors.New("no platform clone fast path")
}
