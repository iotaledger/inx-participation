package participation

import (
	"github.com/iotaledger/hive.go/serializer/v2"
	iotago "github.com/iotaledger/iota.go/v3"
)

type ParticipationBlock struct {
	BlockID iotago.BlockID
	Block   *iotago.Block
	Data    []byte
}

type ParticipationOutput struct {
	BlockID  iotago.BlockID
	OutputID *iotago.OutputID
	Address  iotago.Address
	Deposit  uint64
}

func (o *ParticipationOutput) serializedAddressBytes() ([]byte, error) {
	return o.Address.Serialize(serializer.DeSeriModeNoValidation, nil)
}

func (msg *ParticipationBlock) Transaction() *iotago.Transaction {
	switch payload := msg.Block.Payload.(type) {
	case *iotago.Transaction:
		return payload
	default:
		return nil
	}
}

func (msg *ParticipationBlock) TransactionEssence() *iotago.TransactionEssence {
	if transaction := msg.Transaction(); transaction != nil {
		return transaction.Essence
	}
	return nil
}

func (msg *ParticipationBlock) TransactionEssenceTaggedData() *iotago.TaggedData {
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

func (msg *ParticipationBlock) TransactionEssenceUTXOInputs() []*iotago.OutputID {
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
