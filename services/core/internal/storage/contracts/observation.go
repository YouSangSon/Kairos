package contracts

import "errors"

// ObservationRef identifies a live observation without carrying storage handles.
type ObservationRef struct{ identity *identity }
type identity struct{ marker byte }

// NewObservationRef creates an identity; only its issuing registry grants access.
func NewObservationRef() ObservationRef { return ObservationRef{&identity{}} }

func (ObservationRef) MarshalJSON() ([]byte, error) {
	return nil, errors.New("observation references cannot be serialized")
}

func (*ObservationRef) UnmarshalJSON([]byte) error {
	return errors.New("observation references cannot be deserialized")
}
