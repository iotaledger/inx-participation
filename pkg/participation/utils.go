package participation

import (
	"github.com/gohornet/hornet/pkg/model/hornet"
	"github.com/iotaledger/hive.go/serializer/v2"
	iotago "github.com/iotaledger/iota.go/v3"
)

type ParticipationMessage struct {
	MessageID hornet.MessageID
	Message   *iotago.Message
	Data      []byte
}

type ParticipationOutput struct {
	MessageID hornet.MessageID
	OutputID  *iotago.OutputID
	Address   iotago.Address
	Deposit   uint64
}

func (o *ParticipationOutput) serializedAddressBytes() ([]byte, error) {
	return o.Address.Serialize(serializer.DeSeriModeNoValidation, nil)
}

func (msg *ParticipationMessage) Transaction() *iotago.Transaction {
	switch payload := msg.Message.Payload.(type) {
	case *iotago.Transaction:
		return payload
	default:
		return nil
	}
}

func (msg *ParticipationMessage) TransactionEssence() *iotago.TransactionEssence {
	if transaction := msg.Transaction(); transaction != nil {
		return transaction.Essence
	}
	return nil
}

func (msg *ParticipationMessage) TransactionEssenceTaggedData() *iotago.TaggedData {
	if essence := msg.TransactionEssence(); essence != nil {
		switch payload := essence.Payload.(type) {
		case *iotago.TaggedData:
			return payload
		default:
			return nil
		}
	}
	return nil
}

func (msg *ParticipationMessage) TransactionEssenceUTXOInputs() []*iotago.OutputID {
	var inputs []*iotago.OutputID
	if essence := msg.TransactionEssence(); essence != nil {
		for _, input := range essence.Inputs {
			switch utxoInput := input.(type) {
			case *iotago.UTXOInput:
				id := utxoInput.ID()
				inputs = append(inputs, &id)
			default:
				return nil
			}
		}
	}
	return inputs
}
