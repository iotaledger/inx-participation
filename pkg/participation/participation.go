package participation

import (
	"encoding/json"
	"fmt"

	"github.com/iotaledger/hive.go/serializer/v2"
	iotago "github.com/iotaledger/iota.go/v3"
)

const (
	BallotDenominator = 1000
)

// Participation holds the participation for an event and the optional answer to a ballot.
type Participation struct {
	// EventID is the ID of the event the participation is made for.
	EventID EventID
	// Answers holds the IDs of the answers to the questions of the ballot.
	Answers []byte
}

func (p *Participation) Deserialize(data []byte, _ serializer.DeSerializationMode, _ interface{}) (int, error) {
	return serializer.NewDeserializer(data).
		ReadBytesInPlace(p.EventID[:], func(err error) error {
			return fmt.Errorf("unable to deserialize eventID in participation: %w", err)
		}).
		ReadVariableByteSlice(&p.Answers, serializer.SeriLengthPrefixTypeAsByte, func(err error) error {
			return fmt.Errorf("unable to deserialize answers in participation: %w", err)
		}, 0, BallotMaxQuestionsCount).
		Done()
}

func (p *Participation) Serialize(_ serializer.DeSerializationMode, _ interface{}) ([]byte, error) {
	return serializer.NewSerializer().
		WriteBytes(p.EventID[:], func(err error) error {
			return fmt.Errorf("unable to serialize eventID in participation: %w", err)
		}).
		WriteVariableByteSlice(p.Answers, serializer.SeriLengthPrefixTypeAsByte, func(err error) error {
			return fmt.Errorf("unable to serialize answers in participation: %w", err)
		}, 0, BallotMaxQuestionsCount).
		Serialize()
}

func (p *Participation) MarshalJSON() ([]byte, error) {
	j := &jsonParticipation{
		EventID: p.EventID.ToHex(),
		Answers: iotago.EncodeHex(p.Answers),
	}

	return json.Marshal(j)
}

func (p *Participation) UnmarshalJSON(bytes []byte) error {
	j := &jsonParticipation{}

	if err := json.Unmarshal(bytes, j); err != nil {
		return err
	}

	seri, err := j.ToSerializable()
	if err != nil {
		return err
	}

	participation, ok := seri.(*Participation)
	if !ok {
		panic(fmt.Sprintf("invalid type: expected *Participation, got %T", seri))
	}

	*p = *participation

	return nil
}

// jsonParticipation defines the JSON representation of a Participation.
type jsonParticipation struct {
	// EventID is the ID of the event the participation is made for.
	EventID string `json:"eventId"`
	// Answers holds the IDs of the answers to the questions of the ballot.
	Answers string `json:"answers"`
}

func (j *jsonParticipation) ToSerializable() (serializer.Serializable, error) {
	p := &Participation{
		EventID: EventID{},
		Answers: []byte{},
	}

	eventIDBytes, err := iotago.DecodeHex(j.EventID)
	if err != nil {
		return nil, fmt.Errorf("unable to decode event ID from JSON in participation: %w", err)
	}
	copy(p.EventID[:], eventIDBytes)

	answersBytes, err := iotago.DecodeHex(j.Answers)
	if err != nil {
		return nil, fmt.Errorf("unable to decode answers from JSON in participation: %w", err)
	}
	p.Answers = make([]byte, len(answersBytes))
	copy(p.Answers, answersBytes)

	return p, nil
}
