package test

import (
	"github.com/stretchr/testify/require"

	"github.com/iotaledger/hornet/pkg/model/utxo"
	"github.com/iotaledger/hornet/pkg/testsuite"
	"github.com/iotaledger/hornet/pkg/testsuite/utils"
	"github.com/iotaledger/inx-participation/pkg/participation"
	"github.com/iotaledger/hive.go/serializer/v2"
	iotago "github.com/iotaledger/iota.go/v3"
)

type ParticipationHelper struct {
	env                   *ParticipationTestEnv
	wallet                *utils.HDWallet
	blockBuilder          *testsuite.BlockBuilder
	participationsBuilder *participation.ParticipationsBuilder
}

type SentParticipations struct {
	builder *ParticipationHelper
	block   *testsuite.Block
}

func (env *ParticipationTestEnv) NewParticipationHelper(wallet *utils.HDWallet) *ParticipationHelper {
	blockBuilder := env.te.NewBlockBuilder(ParticipationTag).
		LatestMilestoneAsParents()

	return &ParticipationHelper{
		env:                   env,
		wallet:                wallet,
		blockBuilder:          blockBuilder,
		participationsBuilder: participation.NewParticipationsBuilder(),
	}
}

func (b *ParticipationHelper) WholeWalletBalance() *ParticipationHelper {
	b.blockBuilder.Amount(b.wallet.Balance())
	return b
}

func (b *ParticipationHelper) Amount(amount uint64) *ParticipationHelper {
	b.blockBuilder.Amount(amount)
	return b
}

func (b *ParticipationHelper) Parents(parents iotago.BlockIDs) *ParticipationHelper {
	require.NotEmpty(b.env.t, parents)
	b.blockBuilder.Parents(parents)
	return b
}

func (b *ParticipationHelper) UsingOutput(output *utxo.Output) *ParticipationHelper {
	require.NotNil(b.env.t, output)
	b.blockBuilder.UsingOutput(output)
	return b
}

func (b *ParticipationHelper) AddParticipations(participations []*participation.Participation) *ParticipationHelper {
	require.NotEmpty(b.env.t, participations)
	for _, p := range participations {
		b.AddParticipation(p)
	}
	return b
}

func (b *ParticipationHelper) AddDefaultBallotVote(eventID participation.EventID) *ParticipationHelper {
	b.participationsBuilder.AddParticipation(&participation.Participation{
		EventID: eventID,
		Answers: []byte{defaultBallotAnswerValue},
	})
	return b
}

func (b *ParticipationHelper) AddParticipation(participation *participation.Participation) *ParticipationHelper {
	require.NotNil(b.env.t, participation)
	b.participationsBuilder.AddParticipation(participation)
	return b
}

func (b *ParticipationHelper) Build() *testsuite.Block {
	votes, err := b.participationsBuilder.Build()
	require.NoError(b.env.t, err)

	participationsData, err := votes.Serialize(serializer.DeSeriModePerformValidation, nil)
	require.NoError(b.env.t, err)

	block := b.blockBuilder.
		FromWallet(b.wallet).
		ToWallet(b.wallet).
		TagData(participationsData).
		Build()

	return block
}

func (b *ParticipationHelper) Send() *SentParticipations {
	return &SentParticipations{
		builder: b,
		block:   b.Build().Store().BookOnWallets(),
	}
}

func (c *SentParticipations) Block() *testsuite.Block {
	return c.block
}
