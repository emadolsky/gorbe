package types

import "fmt"

func NewStateNotFoundError(stateName string) *StateNotFound {
	return &StateNotFound{
		stateName: stateName,
	}
}

type StateNotFound struct {
	stateName string
}

func (s *StateNotFound) Error() string {
	return fmt.Sprintf("state not found: %s", s.stateName)
}


