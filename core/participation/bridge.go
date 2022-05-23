package participation

import (
	"context"
	"fmt"

	"github.com/gohornet/hornet/pkg/model/milestone"
	"github.com/gohornet/inx-participation/pkg/participation"
	"github.com/iotaledger/hive.go/serializer/v2"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v3"
)

func participationOutputFromINXOutput(output *inx.LedgerOutput) *participation.ParticipationOutput {
	iotaOutput, err := output.UnwrapOutput(serializer.DeSeriModeNoValidation, nil)
	if err != nil {
		return nil
	}

	// Ignore anything other than BasicOutputs
	if iotaOutput.Type() != iotago.OutputBasic {
		return nil
	}

	unlockConditions := iotaOutput.UnlockConditionSet()
	return &participation.ParticipationOutput{
		BlockID:  output.UnwrapBlockID(),
		OutputID: output.UnwrapOutputID(),
		Address:  unlockConditions.Address().Address,
		Deposit:  iotaOutput.Deposit(),
	}
}

func NodeStatus() (confirmedIndex milestone.Index, pruningIndex milestone.Index) {
	status, err := deps.NodeBridge.NodeStatus()
	if err != nil {
		return 0, 0
	}
	return milestone.Index(status.GetConfirmedMilestone().GetMilestoneInfo().GetMilestoneIndex()), milestone.Index(status.GetTanglePruningIndex())
}

func BlockForBlockID(blockID iotago.BlockID) (*participation.ParticipationBlock, error) {
	block, err := deps.NodeBridge.Client().ReadBlock(context.Background(), inx.NewBlockId(blockID))
	if err != nil {
		return nil, err
	}

	iotagoBlock, err := block.UnwrapBlock(serializer.DeSeriModeNoValidation, nil)
	if err != nil {
		return nil, err
	}

	return &participation.ParticipationBlock{
		BlockID: blockID,
		Block:   iotagoBlock,
		Data:    block.GetData(),
	}, nil
}

func OutputForOutputID(outputID iotago.OutputID) (*participation.ParticipationOutput, error) {
	resp, err := deps.NodeBridge.Client().ReadOutput(context.Background(), inx.NewOutputId(outputID))
	if err != nil {
		return nil, err
	}
	switch resp.GetPayload().(type) {
	case *inx.OutputResponse_Output:
		return participationOutputFromINXOutput(resp.GetOutput()), nil
	case *inx.OutputResponse_Spent:
		return participationOutputFromINXOutput(resp.GetSpent().GetOutput()), nil
	default:
		return nil, fmt.Errorf("invalid inx.OutputResponse payload type")
	}
}

func LedgerUpdates(ctx context.Context, startIndex milestone.Index, endIndex milestone.Index, handler func(index milestone.Index, created []*participation.ParticipationOutput, consumed []*participation.ParticipationOutput) error) error {
	return deps.NodeBridge.ListenToLedgerUpdates(ctx, uint32(startIndex), uint32(endIndex), func(update *inx.LedgerUpdate) error {
		index := milestone.Index(update.GetMilestoneIndex())

		var created []*participation.ParticipationOutput
		for _, output := range update.GetCreated() {
			o := participationOutputFromINXOutput(output)
			if o != nil {
				created = append(created, o)
			}
		}

		var consumed []*participation.ParticipationOutput
		for _, spent := range update.GetConsumed() {
			o := participationOutputFromINXOutput(spent.GetOutput())
			if o != nil {
				consumed = append(consumed, o)
			}
		}

		return handler(index, created, consumed)
	})
}
