package volume

import (
	"fmt"
	"strings"
)

// Volume represents a bind mount from host to container.
type Volume struct {
	Source string
	Target string
}

// Volumes is a slice of Volume that implements flag.Value interface.
type Volumes []Volume

func (v *Volumes) String() string {
	return fmt.Sprintf("%v", *v)
}

func (v *Volumes) Set(value string) error {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return fmt.Errorf("expect /host:/container")
	}

	*v = append(*v, Volume{
		Source: parts[0],
		Target: parts[1],
	})
	return nil
}
