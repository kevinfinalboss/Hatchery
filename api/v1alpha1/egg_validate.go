package v1alpha1

import (
	"fmt"
	"regexp"
)

func (s *EggSpec) Validate() []string {
	var msgs []string
	if d := s.StartupDetection; d != nil {
		if _, err := regexp.Compile(d.Regex); err != nil {
			msgs = append(msgs, fmt.Sprintf("startupDetection.regex: %v", err))
		}
	}
	return msgs
}
