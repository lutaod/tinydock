package container

import (
	"strings"
)

// Envs implements flag.Value for collecting environment variables.
type Envs []string

func (s *Envs) String() string {
	return strings.Join(*s, ",")
}

func (s *Envs) Set(value string) error {
	*s = append(*s, value)
	return nil
}
