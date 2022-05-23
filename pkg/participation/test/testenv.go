package test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gohornet/hornet/pkg/model/milestone"
	"github.com/gohornet/hornet/pkg/model/storage"
	"github.com/gohornet/hornet/pkg/model/utxo"
	"github.com/gohornet/hornet/pkg/testsuite"
	"github.com/gohornet/hornet/pkg/testsuite/utils"
	"github.com/gohornet/hornet/pkg/whiteflag"
	"github.com/gohornet/inx-participation/pkg/participation"
	"github.com/iotaledger/hive.go/kvstore"
	"github.com/iotaledger/hive.go/kvstore/mapdb"
	iotago "github.com/iotaledger/iota.go/v3"
)

const (
	ParticipationTag               = "TEST"
	defaultBallotAnswerValue uint8 = 10
)

var (
	genesisSeed, _ = hex.DecodeString("2f54b071657e6644629a40518ba6554de4eee89f0757713005ad26137d80968d05e1ca1bca555d8b4b85a3f4fcf11a6a48d3d628d1ace40f48009704472fc8f9")
	seed1, _       = hex.DecodeString("96d9ff7a79e4b0a5f3e5848ae7867064402da92a62eabb4ebbe463f12d1f3b1aace1775488f51cb1e3a80732a03ef60b111d6833ab605aa9f8faebeb33bbe3d9")
	seed2, _       = hex.DecodeString("b15209ddc93cbdb600137ea6a8f88cdd7c5d480d5815c9352a0fb5c4e4b86f7151dcb44c2ba635657a2df5a8fd48cb9bab674a9eceea527dbbb254ef8c9f9cd7")
	seed3, _       = hex.DecodeString("d5353ceeed380ab89a0f6abe4630c2091acc82617c0edd4ff10bd60bba89e2ed30805ef095b989c2bf208a474f8748d11d954aade374380422d4d812b6f1da90")
	seed4, _       = hex.DecodeString("bd6fe09d8a309ca309c5db7b63513240490109cd0ac6b123551e9da0d5c8916c4a5a4f817e4b4e9df89885ce1af0986da9f1e56b65153c2af1e87ab3b11dabb4")

	MinPoWScore   = 100.0
	BelowMaxDepth = uint16(15)
)

type ParticipationTestEnv struct {
	t  *testing.T
	te *testsuite.TestEnvironment

	GenesisWallet *utils.HDWallet
	Wallet1       *utils.HDWallet
	Wallet2       *utils.HDWallet
	Wallet3       *utils.HDWallet
	Wallet4       *utils.HDWallet

	participationStore kvstore.KVStore
	rm                 *participation.ParticipationManager
}

func NewParticipationTestEnv(t *testing.T, wallet1Balance uint64, wallet2Balance uint64, wallet3Balance uint64, wallet4Balance uint64, assertSteps bool) *ParticipationTestEnv {

	genesisWallet := utils.NewHDWallet("Genesis", genesisSeed, 0)
	seed1Wallet := utils.NewHDWallet("Seed1", seed1, 0)
	seed2Wallet := utils.NewHDWallet("Seed2", seed2, 0)
	seed3Wallet := utils.NewHDWallet("Seed3", seed3, 0)
	seed4Wallet := utils.NewHDWallet("Seed4", seed4, 0)

	genesisAddress := genesisWallet.Address()

	te := testsuite.SetupTestEnvironment(t, genesisAddress, 2, BelowMaxDepth, MinPoWScore, false)

	//Add token supply to our local HDWallet
	genesisWallet.BookOutput(te.GenesisOutput)
	if assertSteps {
		te.AssertWalletBalance(genesisWallet, te.ProtocolParameters().TokenSupply)
	}

	// Fund Wallet1
	messageA := te.NewBlockBuilder("A").
		Parents(te.LastMilestoneParents()).
		FromWallet(genesisWallet).
		ToWallet(seed1Wallet).
		Amount(wallet1Balance).
		Build().
		Store().
		BookOnWallets()

	// Fund Wallet2
	messageB := te.NewBlockBuilder("B").
		Parents(append(te.LastMilestoneParents(), messageA.StoredBlockID())).
		FromWallet(genesisWallet).
		ToWallet(seed2Wallet).
		Amount(wallet2Balance).
		Build().
		Store().
		BookOnWallets()

	// Fund Wallet3
	messageC := te.NewBlockBuilder("C").
		Parents(append(te.LastMilestoneParents(), messageB.StoredBlockID())).
		FromWallet(genesisWallet).
		ToWallet(seed3Wallet).
		Amount(wallet3Balance).
		Build().
		Store().
		BookOnWallets()

	// Fund Wallet4
	messageD := te.NewBlockBuilder("D").
		Parents(append(te.LastMilestoneParents(), messageC.StoredBlockID())).
		FromWallet(genesisWallet).
		ToWallet(seed4Wallet).
		Amount(wallet4Balance).
		Build().
		Store().
		BookOnWallets()

	// Confirming milestone at block D
	_, confStats := te.IssueAndConfirmMilestoneOnTips(iotago.BlockIDs{messageD.StoredBlockID()}, false)
	if assertSteps {

		require.Equal(t, 4+1, confStats.BlocksReferenced) // 4 + milestone itself
		require.Equal(t, 4, confStats.BlocksIncludedWithTransactions)
		require.Equal(t, 0, confStats.BlocksExcludedWithConflictingTransactions)
		require.Equal(t, 1, confStats.BlocksExcludedWithoutTransactions) // the milestone

		// Verify balances
		te.AssertWalletBalance(genesisWallet, te.ProtocolParameters().TokenSupply-wallet1Balance-wallet2Balance-wallet3Balance-wallet4Balance)
		te.AssertWalletBalance(seed1Wallet, wallet1Balance)
		te.AssertWalletBalance(seed2Wallet, wallet2Balance)
		te.AssertWalletBalance(seed3Wallet, wallet3Balance)
		te.AssertWalletBalance(seed4Wallet, wallet4Balance)
	}

	store := mapdb.NewMapDB()

	pm, err := participation.NewManager(
		store,
		func() *iotago.ProtocolParameters {
			return te.ProtocolParameters()
		},
		func() (confirmedIndex milestone.Index, pruningIndex milestone.Index) {
			return te.SyncManager().ConfirmedMilestoneIndex(), 0
		},
		func(blockID iotago.BlockID) (*participation.ParticipationBlock, error) {
			cachedBlock := te.Storage().CachedBlockOrNil(blockID)
			if cachedBlock == nil {
				return nil, nil
			}
			defer cachedBlock.Release(true)
			return &participation.ParticipationBlock{
				BlockID: blockID,
				Block:   cachedBlock.Block().Block(),
				Data:    cachedBlock.Block().Data(),
			}, nil
		},
		func(outputID iotago.OutputID) (*participation.ParticipationOutput, error) {
			output, err := te.UTXOManager().ReadOutputByOutputIDWithoutLocking(outputID)
			if err != nil {
				return nil, err
			}
			if output.OutputType() != iotago.OutputBasic {
				return nil, nil
			}
			return &participation.ParticipationOutput{
				BlockID:  output.BlockID(),
				OutputID: outputID,
				Address:  output.Output().UnlockConditionSet().Address().Address,
				Deposit:  output.Deposit(),
			}, nil
		},
		func(ctx context.Context, startIndex milestone.Index, endIndex milestone.Index, handler func(index milestone.Index, created []*participation.ParticipationOutput, consumed []*participation.ParticipationOutput) error) error {
			te.UTXOManager().ReadLockLedger()
			defer te.UTXOManager().ReadUnlockLedger()

			currentIndex := startIndex
			for {
				if currentIndex > te.SyncManager().ConfirmedMilestoneIndex() {
					return nil
				}

				msDiff, err := te.UTXOManager().MilestoneDiffWithoutLocking(currentIndex)
				if err != nil {
					return err
				}

				var created []*participation.ParticipationOutput
				for _, output := range msDiff.Outputs {
					if output.OutputType() != iotago.OutputBasic {
						continue
					}
					created = append(created, &participation.ParticipationOutput{
						BlockID:  output.BlockID(),
						OutputID: output.OutputID(),
						Address:  output.Output().UnlockConditionSet().Address().Address,
						Deposit:  output.Deposit(),
					})
				}

				var consumed []*participation.ParticipationOutput
				for _, spent := range msDiff.Spents {
					if spent.OutputType() != iotago.OutputBasic {
						continue
					}
					consumed = append(consumed, &participation.ParticipationOutput{
						BlockID:  spent.BlockID(),
						OutputID: spent.OutputID(),
						Address:  spent.Output().Output().UnlockConditionSet().Address().Address,
						Deposit:  spent.Deposit(),
					})
				}

				err = handler(msDiff.Index, created, consumed)
				if err != nil {
					return err
				}

				if currentIndex >= endIndex {
					break
				}

				currentIndex++
			}
			return nil
		},
		participation.WithTagMessage(ParticipationTag),
	)
	require.NoError(t, err)

	// Connect the callbacks from the testsuite to the ParticipationManager
	te.ConfigureUTXOCallbacks(
		func(index milestone.Index, newOutputs utxo.Outputs, newSpents utxo.Spents) {

			var created []*participation.ParticipationOutput
			for _, output := range newOutputs {
				if output.OutputType() != iotago.OutputBasic {
					continue
				}
				created = append(created, &participation.ParticipationOutput{
					BlockID:  output.BlockID(),
					OutputID: output.OutputID(),
					Address:  output.Output().UnlockConditionSet().Address().Address,
					Deposit:  output.Deposit(),
				})
			}

			var consumed []*participation.ParticipationOutput
			for _, spent := range newSpents {
				if spent.OutputType() != iotago.OutputBasic {
					continue
				}
				consumed = append(consumed, &participation.ParticipationOutput{
					BlockID:  spent.BlockID(),
					OutputID: spent.OutputID(),
					Address:  spent.Output().Output().UnlockConditionSet().Address().Address,
					Deposit:  spent.Deposit(),
				})
			}

			require.NoError(t, pm.ApplyNewLedgerUpdate(index, created, consumed))
		},
	)

	return &ParticipationTestEnv{
		t:                  t,
		te:                 te,
		GenesisWallet:      genesisWallet,
		Wallet1:            seed1Wallet,
		Wallet2:            seed2Wallet,
		Wallet3:            seed3Wallet,
		Wallet4:            seed4Wallet,
		participationStore: store,
		rm:                 pm,
	}
}

func (env *ParticipationTestEnv) ProtocolParameters() *iotago.ProtocolParameters {
	return env.te.ProtocolParameters()
}

func (env *ParticipationTestEnv) ParticipationManager() *participation.ParticipationManager {
	return env.rm
}

func (env *ParticipationTestEnv) ConfirmedMilestoneIndex() milestone.Index {
	return env.te.SyncManager().ConfirmedMilestoneIndex()
}

func (env *ParticipationTestEnv) LastMilestoneParents() iotago.BlockIDs {
	return env.te.LastMilestoneParents()
}

func (env *ParticipationTestEnv) Cleanup() {
	env.rm.CloseDatabase()
	env.te.CleanupTestEnvironment(true)
}

func (env *ParticipationTestEnv) DefaultEvent(commenceMilestoneIndex milestone.Index, startPhaseDuration uint32, holdingDuration uint32) *participation.Event {

	eventCommenceIndex := commenceMilestoneIndex
	eventStartIndex := eventCommenceIndex + milestone.Index(startPhaseDuration)
	eventEndIndex := eventStartIndex + milestone.Index(holdingDuration)

	eventBuilder := participation.NewEventBuilder("All 4 HORNET", eventCommenceIndex, eventStartIndex, eventEndIndex, "The biggest governance decision in the history of IOTA")

	questionBuilder := participation.NewQuestionBuilder("Give all the funds to the HORNET developers?", "This would fund the development of HORNET indefinitely")
	questionBuilder.AddAnswer(&participation.Answer{
		Value:          defaultBallotAnswerValue,
		Text:           "YES",
		AdditionalInfo: "Go team!",
	})
	questionBuilder.AddAnswer(&participation.Answer{
		Value:          20,
		Text:           "Doh! Of course!",
		AdditionalInfo: "There is no other option",
	})

	question, err := questionBuilder.Build()
	require.NoError(env.t, err)

	ballotBuilder := participation.NewBallotBuilder()
	ballotBuilder.AddQuestion(question)
	payload, err := ballotBuilder.Build()
	require.NoError(env.t, err)

	eventBuilder.Payload(payload)

	event, err := eventBuilder.Build()
	require.NoError(env.t, err)

	env.PrintJSON(event)

	return event
}

func (env *ParticipationTestEnv) StoreDefaultEvent(commenceMilestoneIndex milestone.Index, startPhaseDuration uint32, holdingDuration uint32) participation.EventID {

	event := env.DefaultEvent(commenceMilestoneIndex, startPhaseDuration, holdingDuration)

	eventID, err := env.rm.StoreEvent(event)
	require.NoError(env.t, err)

	// Check the stored event is still there
	require.NotNil(env.t, env.rm.Event(eventID))

	return eventID
}

func (env *ParticipationTestEnv) SendParticipations(wallet *utils.HDWallet, amount uint64, participations []*participation.Participation) *SentParticipations {
	return env.NewParticipationHelper(wallet).Amount(amount).AddParticipations(participations).Send()
}

func (env *ParticipationTestEnv) CancelParticipations(wallet *utils.HDWallet) *testsuite.Block {
	return env.Transfer(wallet, wallet, wallet.Balance())
}

func (env *ParticipationTestEnv) NewBlockBuilder(optionalTag ...string) *testsuite.BlockBuilder {
	return env.te.NewBlockBuilder(optionalTag...)
}

func (env *ParticipationTestEnv) Transfer(fromWallet *utils.HDWallet, toWallet *utils.HDWallet, amount uint64) *testsuite.Block {
	return env.te.NewBlockBuilder("Not a vote").
		LatestMilestoneAsParents().
		FromWallet(fromWallet).
		ToWallet(toWallet).
		Amount(amount).
		Build().
		Store().
		BookOnWallets()
}

func (env *ParticipationTestEnv) IssueDefaultBallotVoteAndMilestone(eventID participation.EventID, wallet *utils.HDWallet, balance ...uint64) *SentParticipations {

	amountToSend := wallet.Balance()
	if len(balance) > 0 {
		amountToSend = balance[0]
	}

	castVote := env.NewParticipationHelper(wallet).
		Amount(amountToSend).
		AddDefaultBallotVote(eventID).
		Send()

	_, confStats := env.IssueMilestone(castVote.Block().StoredBlockID())
	require.Equal(env.t, 1+1, confStats.BlocksReferenced) // 1 + milestone itself

	return castVote
}

func (env *ParticipationTestEnv) IssueMilestone(onTips ...iotago.BlockID) (*whiteflag.Confirmation, *whiteflag.ConfirmedMilestoneStats) {
	return env.te.IssueAndConfirmMilestoneOnTips(onTips, false)
}

func (env *ParticipationTestEnv) ActiveParticipationsForEvent(eventID participation.EventID) []*participation.TrackedParticipation {
	var votes []*participation.TrackedParticipation
	env.ParticipationManager().ForEachActiveParticipation(eventID, func(trackedVote *participation.TrackedParticipation) bool {
		votes = append(votes, trackedVote)
		return true
	})
	return votes
}

func (env *ParticipationTestEnv) PastParticipationsForEvent(eventID participation.EventID) []*participation.TrackedParticipation {
	var votes []*participation.TrackedParticipation
	env.ParticipationManager().ForEachPastParticipation(eventID, func(trackedVote *participation.TrackedParticipation) bool {
		votes = append(votes, trackedVote)
		return true
	})
	return votes
}

func (env *ParticipationTestEnv) PrintJSON(i interface{}) {
	j, err := json.MarshalIndent(i, "", "  ")
	require.NoError(env.t, err)
	fmt.Println(string(j))
}

func (env *ParticipationTestEnv) AssertEventsCount(acceptingCount int, countingCount int) {
	// Verify current event counts
	require.Equal(env.t, acceptingCount, len(env.ParticipationManager().EventsAcceptingParticipation(env.ConfirmedMilestoneIndex())))
	require.Equal(env.t, countingCount, len(env.ParticipationManager().EventsCountingParticipation(env.ConfirmedMilestoneIndex())))
}

func (env *ParticipationTestEnv) AssertEventParticipationStatus(eventID participation.EventID, activeParticipations int, pastParticipations int) {
	// Verify current participation status for an event
	require.Equal(env.t, activeParticipations, len(env.ActiveParticipationsForEvent(eventID)))
	require.Equal(env.t, pastParticipations, len(env.PastParticipationsForEvent(eventID)))
}

func (env *ParticipationTestEnv) AssertDefaultBallotAnswerStatus(eventID participation.EventID, currentVoteAmount uint64, accumulatedVoteAmount uint64) {
	env.AssertBallotAnswerStatusAtConfirmedMilestoneIndex(eventID, currentVoteAmount, accumulatedVoteAmount, 0, defaultBallotAnswerValue)
}

func (env *ParticipationTestEnv) AssertBallotAnswerStatusAtConfirmedMilestoneIndex(eventID participation.EventID, currentVoteAmount uint64, accumulatedVoteAmount uint64, questionIndex int, answerValue uint8) {
	env.AssertBallotAnswerStatus(eventID, env.ConfirmedMilestoneIndex(), currentVoteAmount, accumulatedVoteAmount, questionIndex, answerValue)
}

func (env *ParticipationTestEnv) AssertBallotAnswerStatus(eventID participation.EventID, milestone milestone.Index, currentVoteAmount uint64, accumulatedVoteAmount uint64, questionIndex int, answerValue uint8) {
	status, err := env.ParticipationManager().EventStatus(eventID, milestone)
	require.NoError(env.t, err)
	env.PrintJSON(status)
	require.Equal(env.t, milestone, status.MilestoneIndex)
	require.Exactly(env.t, currentVoteAmount, status.Questions[questionIndex].StatusForAnswerValue(answerValue).Current)
	require.Exactly(env.t, accumulatedVoteAmount, status.Questions[questionIndex].StatusForAnswerValue(answerValue).Accumulated)
}

func (env *ParticipationTestEnv) AssertStakingRewardsStatusAtConfirmedMilestoneIndex(eventID participation.EventID, stakedAmount uint64, rewardedAmount uint64) {
	env.AssertStakingRewardsStatus(eventID, env.ConfirmedMilestoneIndex(), stakedAmount, rewardedAmount)
}

func (env *ParticipationTestEnv) AssertStakingRewardsStatus(eventID participation.EventID, milestone milestone.Index, stakedAmount uint64, rewardedAmount uint64) {
	status, err := env.ParticipationManager().EventStatus(eventID, milestone)
	require.NoError(env.t, err)
	env.PrintJSON(status)
	event := env.ParticipationManager().Event(eventID)
	require.NotNil(env.t, event)
	requiredMilestone := milestone
	if requiredMilestone > event.EndMilestoneIndex() {
		requiredMilestone = event.EndMilestoneIndex()
	}
	require.Equal(env.t, requiredMilestone, status.MilestoneIndex)
	require.Exactly(env.t, stakedAmount, status.Staking.Staked)
	require.Exactly(env.t, rewardedAmount, status.Staking.Rewarded)
}

func (env *ParticipationTestEnv) AssertTrackedParticipation(eventID participation.EventID, sentParticipations *SentParticipations, startMilestoneIndex milestone.Index, endMilestoneIndex milestone.Index, amount uint64) {
	trackedParticipation, err := env.ParticipationManager().ParticipationForOutputIDWithoutLocking(eventID, sentParticipations.Block().GeneratedUTXO().OutputID())
	require.NoError(env.t, err)
	require.Equal(env.t, sentParticipations.Block().GeneratedUTXO().OutputID(), trackedParticipation.OutputID)
	require.Equal(env.t, sentParticipations.Block().StoredBlockID(), trackedParticipation.BlockID)
	require.Equal(env.t, amount, trackedParticipation.Amount)
	require.Equal(env.t, startMilestoneIndex, trackedParticipation.StartIndex)
	require.Equal(env.t, endMilestoneIndex, trackedParticipation.EndIndex)
}

func (env *ParticipationTestEnv) AssertInvalidParticipation(eventID participation.EventID, sentParticipations *SentParticipations) {
	_, err := env.ParticipationManager().ParticipationForOutputIDWithoutLocking(eventID, sentParticipations.Block().GeneratedUTXO().OutputID())
	require.Error(env.t, err)
	require.ErrorIs(env.t, err, participation.ErrUnknownParticipation)
}

func (env *ParticipationTestEnv) AssertRewardBalance(eventID participation.EventID, address iotago.Address, balance uint64, milestoneIndex ...milestone.Index) {

	msIndex := env.ConfirmedMilestoneIndex()
	if len(milestoneIndex) > 0 {
		msIndex = milestoneIndex[0]
	}
	rewards, err := env.ParticipationManager().StakingRewardForAddressWithoutLocking(eventID, address, msIndex)
	require.NoError(env.t, err)
	require.Exactly(env.t, balance, rewards)
}

func (env *ParticipationTestEnv) AssertWalletBalance(wallet *utils.HDWallet, expectedBalance uint64) {
	env.te.AssertWalletBalance(wallet, expectedBalance)
}

func ParticipationBlockFromBlock(msg *storage.Block) *participation.ParticipationBlock {
	return &participation.ParticipationBlock{
		BlockID: msg.BlockID(),
		Block:   msg.Block(),
		Data:    msg.Data(),
	}
}
