package participation

import (
	"github.com/pkg/errors"

	"github.com/iotaledger/hive.go/core/kvstore"
	"github.com/iotaledger/hive.go/core/marshalutil"
	"github.com/iotaledger/hive.go/serializer/v2"
	iotago "github.com/iotaledger/iota.go/v3"
)

const (
	DBVersionParticipation byte = 1
)

var (
	ErrUnknownParticipation                  = errors.New("no participation found")
	ErrEventNotFound                         = errors.New("referenced event does not exist")
	ErrInvalidEvent                          = errors.New("invalid event")
	ErrInvalidPreviouslyTrackedParticipation = errors.New("a previously tracked participation changed and is now invalid")
	ErrInvalidCurrentBallotVoteBalance       = errors.New("current ballot vote balance invalid")
	ErrInvalidCurrentStakedAmount            = errors.New("current staked amount invalid")
	ErrInvalidCurrentRewardsAmount           = errors.New("current rewards amount invalid")
)

// Status

func ledgerIndexKey() []byte {
	m := marshalutil.New(12)
	m.WriteByte(ParticipationStoreKeyPrefixStatus)
	m.WriteBytes([]byte("ledgerIndex"))

	return m.Bytes()
}

func (pm *ParticipationManager) storeLedgerIndex(index iotago.MilestoneIndex) error {
	m := marshalutil.New(4)
	m.WriteUint32(index)

	return pm.participationStore.Set(ledgerIndexKey(), m.Bytes())
}

func (pm *ParticipationManager) readLedgerIndex() (iotago.MilestoneIndex, error) {
	v, err := pm.participationStore.Get(ledgerIndexKey())
	if err != nil {
		if errors.Is(err, kvstore.ErrKeyNotFound) {
			return 0, nil
		}

		return 0, err
	}
	m := marshalutil.New(v)
	u, err := m.ReadUint32()

	return u, err
}

// Events

func eventKeyForEventID(eventID EventID) []byte {
	m := marshalutil.New(33)
	m.WriteByte(ParticipationStoreKeyPrefixEvents) // 1 byte
	m.WriteBytes(eventID[:])                       // 32 bytes

	return m.Bytes()
}

func (pm *ParticipationManager) loadEvents() (map[EventID]*Event, error) {

	events := make(map[EventID]*Event)

	var innerErr error
	if err := pm.participationStore.Iterate(kvstore.KeyPrefix{ParticipationStoreKeyPrefixEvents}, func(key kvstore.Key, value kvstore.Value) bool {

		eventID := EventID{}
		copy(eventID[:], key[1:]) // Skip the prefix

		event := &Event{}
		_, innerErr = event.Deserialize(value, serializer.DeSeriModeNoValidation, nil)
		if innerErr != nil {
			return false
		}

		events[eventID] = event

		return true
	}); err != nil {
		return nil, err
	}

	if innerErr != nil {
		return nil, innerErr
	}

	return events, nil
}

func (pm *ParticipationManager) storeEvent(event *Event) (EventID, error) {

	eventBytes, err := event.Serialize(serializer.DeSeriModePerformValidation, nil)
	if err != nil {
		return NullEventID, err
	}

	eventID, err := event.ID()
	if err != nil {
		return NullEventID, err
	}

	if err := pm.participationStore.Set(eventKeyForEventID(eventID), eventBytes); err != nil {
		return NullEventID, err
	}

	return eventID, nil
}

func (pm *ParticipationManager) deleteEvent(eventID EventID) error {
	return pm.participationStore.Delete(eventKeyForEventID(eventID))
}

// Blocks

func blockKeyForEventPrefix(eventID EventID) []byte {
	m := marshalutil.New(33)
	m.WriteByte(ParticipationStoreKeyPrefixBlocks) // 1 byte
	m.WriteBytes(eventID[:])                       // 32 bytes

	return m.Bytes()
}

func blockKeyForEventAndBlockID(eventID EventID, blockID iotago.BlockID) []byte {
	m := marshalutil.New(65)
	m.WriteBytes(blockKeyForEventPrefix(eventID)) // 33 bytes
	m.WriteBytes(blockID[:])                      // 32 bytes

	return m.Bytes()
}

func (pm *ParticipationManager) storeBlockForEvent(eventID EventID, block *ParticipationBlock, mutations kvstore.BatchedMutations) error {
	return mutations.Set(blockKeyForEventAndBlockID(eventID, block.BlockID), block.Data)
}

func (pm *ParticipationManager) BlockForEventAndBlockID(eventID EventID, blockID iotago.BlockID) (*ParticipationBlock, error) {
	value, err := pm.participationStore.Get(blockKeyForEventAndBlockID(eventID, blockID))
	if errors.Is(err, kvstore.ErrKeyNotFound) {
		//nolint:nilnil // nil, nil is ok in this context, even if it is not go idiomatic
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	iotaBlock := &iotago.Block{}
	if _, err := iotaBlock.Deserialize(value, serializer.DeSeriModeNoValidation, nil); err != nil {
		return nil, err
	}

	return &ParticipationBlock{
		BlockID: blockID,
		Block:   iotaBlock,
		Data:    value,
	}, nil
}

// Outputs

func participationKeyForEventOutputsPrefix(eventID EventID) []byte {
	m := marshalutil.New(33)
	m.WriteByte(ParticipationStoreKeyPrefixTrackedOutputs) // 1 byte
	m.WriteBytes(eventID[:])                               // 32 bytes

	return m.Bytes()
}

func participationKeyForEventAndOutputID(eventID EventID, outputID iotago.OutputID) []byte {
	m := marshalutil.New(67)
	m.WriteBytes(participationKeyForEventOutputsPrefix(eventID)) // 32 bytes
	m.WriteBytes(outputID[:])                                    // 34 bytes

	return m.Bytes()
}

func participationKeyForEventSpentOutputsPrefix(eventID EventID) []byte {
	m := marshalutil.New(33)
	m.WriteByte(ParticipationStoreKeyPrefixTrackedSpentOutputs) // 1 byte
	m.WriteBytes(eventID[:])                                    // 32 bytes

	return m.Bytes()
}

func participationKeyForEventAndSpentOutputID(eventID EventID, outputID iotago.OutputID) []byte {
	m := marshalutil.New(67)
	m.WriteBytes(participationKeyForEventSpentOutputsPrefix(eventID)) // 33 bytes
	m.WriteBytes(outputID[:])                                         // 34 bytes

	return m.Bytes()
}

func participationKeyForEventPrefix(eventID EventID) []byte {
	m := marshalutil.New(33)
	m.WriteByte(ParticipationStoreKeyPrefixTrackedOutputByAddress) // 1 byte
	m.WriteBytes(eventID[:])                                       // 32 bytes

	return m.Bytes()
}

func participationKeyForEventAndAddressPrefix(eventID EventID, addressBytes []byte) []byte {
	m := marshalutil.New(66)
	m.WriteBytes(participationKeyForEventPrefix(eventID)) // 33 bytes
	m.WriteBytes(addressBytes)                            // 33 bytes

	return m.Bytes()
}

func participationKeyForEventAndAddressOutputID(eventID EventID, addressBytes []byte, outputID iotago.OutputID) []byte {
	m := marshalutil.New(100)
	m.WriteBytes(participationKeyForEventAndAddressPrefix(eventID, addressBytes)) // 66 bytes
	m.WriteBytes(outputID[:])                                                     // 34 bytes

	return m.Bytes()
}

func (pm *ParticipationManager) ParticipationsForAddress(eventID EventID, address iotago.Address) ([]*TrackedParticipation, error) {
	// We need to lock the ParticipationManager here so that we don't get partial results while the new ledger update is being applied
	pm.RLock()
	defer pm.RUnlock()

	return pm.ParticipationsForAddressWithoutLocking(eventID, address)
}

func (pm *ParticipationManager) ParticipationsForAddressWithoutLocking(eventID EventID, address iotago.Address) ([]*TrackedParticipation, error) {
	addressBytes, err := address.Serialize(serializer.DeSeriModeNoValidation, nil)
	if err != nil {
		return nil, err
	}

	trackedParticipations := []*TrackedParticipation{}

	var innerErr error
	prefix := participationKeyForEventAndAddressPrefix(eventID, addressBytes)
	prefixLen := len(prefix)
	if err := pm.participationStore.IterateKeys(prefix, func(key kvstore.Key) bool {
		outputID := iotago.OutputID{}
		copy(outputID[:], key[prefixLen:])

		participation, err := pm.ParticipationForOutputIDWithoutLocking(eventID, outputID)
		if err != nil {
			if errors.Is(err, ErrUnknownParticipation) {
				return true
			}
			innerErr = err

			return false
		}
		trackedParticipations = append(trackedParticipations, participation)

		return true
	}); err != nil {
		return nil, err
	}

	if innerErr != nil {
		return nil, innerErr
	}

	return trackedParticipations, nil
}

func (pm *ParticipationManager) ParticipationsForOutputID(outputID iotago.OutputID) ([]*TrackedParticipation, error) {
	// We need to lock the ParticipationManager here so that we don't get partial results while the new ledger update is being applied
	pm.RLock()
	defer pm.RUnlock()

	eventIDs := pm.EventIDs()
	trackedParticipations := []*TrackedParticipation{}
	for _, eventID := range eventIDs {
		participation, err := pm.ParticipationForOutputIDWithoutLocking(eventID, outputID)
		if err != nil {
			if errors.Is(err, ErrUnknownParticipation) {
				continue
			}

			return nil, err
		}
		trackedParticipations = append(trackedParticipations, participation)
	}

	return trackedParticipations, nil
}

func (pm *ParticipationManager) ParticipationForOutputIDWithoutLocking(eventID EventID, outputID iotago.OutputID) (*TrackedParticipation, error) {
	readOutput := func(eventID EventID, outputID iotago.OutputID) (kvstore.Key, kvstore.Value, error) {
		key := participationKeyForEventAndOutputID(eventID, outputID)
		value, err := pm.participationStore.Get(key)
		if errors.Is(err, kvstore.ErrKeyNotFound) {
			return nil, nil, ErrUnknownParticipation
		}
		if err != nil {
			return nil, nil, err
		}

		return key, value, nil
	}

	readSpent := func(eventID EventID, outputID iotago.OutputID) (kvstore.Key, kvstore.Value, error) {
		key := participationKeyForEventAndSpentOutputID(eventID, outputID)
		value, err := pm.participationStore.Get(key)
		if errors.Is(err, kvstore.ErrKeyNotFound) {
			return nil, nil, ErrUnknownParticipation
		}
		if err != nil {
			return nil, nil, err
		}

		return key, value, nil
	}

	var key kvstore.Key
	var value kvstore.Value
	var err error

	key, value, err = readOutput(eventID, outputID)
	if errors.Is(err, ErrUnknownParticipation) {
		key, value, err = readSpent(eventID, outputID)
	}

	if err != nil {
		return nil, err
	}

	return TrackedParticipationFromBytes(key, value)
}

type TrackedParticipationConsumer func(trackedParticipation *TrackedParticipation) bool

func (pm *ParticipationManager) ForEachActiveParticipation(eventID EventID, consumer TrackedParticipationConsumer) error {
	// We need to lock the ParticipationManager here so that we don't get partial results while the new ledger update is being applied
	pm.RLock()
	defer pm.RUnlock()

	var innerErr error
	if err := pm.participationStore.Iterate(participationKeyForEventOutputsPrefix(eventID), func(key kvstore.Key, value kvstore.Value) bool {
		participation, err := TrackedParticipationFromBytes(key, value)
		if err != nil {
			innerErr = err

			return false
		}

		return consumer(participation)
	}); err != nil {
		return err
	}

	return innerErr
}

func (pm *ParticipationManager) ForEachPastParticipation(eventID EventID, consumer TrackedParticipationConsumer) error {
	// We need to lock the ParticipationManager here so that we don't get partial results while the new ledger update is being applied
	pm.RLock()
	defer pm.RUnlock()

	var innerErr error
	if err := pm.participationStore.Iterate(participationKeyForEventSpentOutputsPrefix(eventID), func(key kvstore.Key, value kvstore.Value) bool {
		participation, err := TrackedParticipationFromBytes(key, value)
		if err != nil {
			innerErr = err

			return false
		}

		return consumer(participation)
	}); err != nil {
		return err
	}

	return innerErr
}

// Ballot answers

func currentBallotVoteBalanceKeyPrefix(eventID EventID) []byte {
	m := marshalutil.New(33)
	m.WriteByte(ParticipationStoreKeyPrefixBallotCurrentVoteBalanceForQuestionAndAnswer) // 1 byte
	m.WriteBytes(eventID[:])                                                             // 32 bytes

	return m.Bytes()
}

func currentBallotVoteBalanceKeyForQuestionAndAnswer(eventID EventID, milestone iotago.MilestoneIndex, questionIndex uint8, answerIndex uint8) []byte {
	m := marshalutil.New(39)
	m.WriteBytes(currentBallotVoteBalanceKeyPrefix(eventID)) // 33 bytes
	m.WriteUint32(milestone)                                 // 4 bytes
	m.WriteUint8(questionIndex)                              // 1 byte
	m.WriteUint8(answerIndex)                                // 1 byte

	return m.Bytes()
}

func accumulatedBallotVoteBalanceKeyPrefix(eventID EventID) []byte {
	m := marshalutil.New(33)
	m.WriteByte(ParticipationStoreKeyPrefixBallotAccululatedVoteBalanceForQuestionAndAnswer) // 1 byte
	m.WriteBytes(eventID[:])                                                                 // 32 bytes

	return m.Bytes()
}

func accumulatedBallotVoteBalanceKeyForQuestionAndAnswer(eventID EventID, milestone iotago.MilestoneIndex, questionIndex uint8, answerIndex uint8) []byte {
	m := marshalutil.New(39)
	m.WriteBytes(accumulatedBallotVoteBalanceKeyPrefix(eventID)) // 33 bytes
	m.WriteUint32(milestone)                                     // 4 bytes
	m.WriteUint8(questionIndex)                                  // 1 byte
	m.WriteUint8(answerIndex)                                    // 1 byte

	return m.Bytes()
}

func (pm *ParticipationManager) startParticipationAtMilestone(eventID EventID, output *ParticipationOutput, startIndex iotago.MilestoneIndex, mutations kvstore.BatchedMutations) error {
	trackedVote := &TrackedParticipation{
		EventID:    eventID,
		OutputID:   output.OutputID,
		BlockID:    output.BlockID,
		Amount:     output.Deposit,
		StartIndex: startIndex,
		EndIndex:   0,
	}
	if err := mutations.Set(participationKeyForEventAndOutputID(eventID, output.OutputID), trackedVote.ValueBytes()); err != nil {
		return err
	}

	addressBytes, err := output.serializedAddressBytes()
	if err != nil {
		return err
	}

	return mutations.Set(participationKeyForEventAndAddressOutputID(eventID, addressBytes, output.OutputID), []byte{})
}

func (pm *ParticipationManager) endParticipationAtMilestone(eventID EventID, output *ParticipationOutput, endIndex iotago.MilestoneIndex, mutations kvstore.BatchedMutations) error {
	key := participationKeyForEventAndOutputID(eventID, output.OutputID)

	value, err := pm.participationStore.Get(key)
	if err != nil {
		if errors.Is(err, kvstore.ErrKeyNotFound) {
			return ErrUnknownParticipation
		}

		return err
	}

	participation, err := TrackedParticipationFromBytes(key, value)
	if err != nil {
		return err
	}

	participation.EndIndex = endIndex

	// Delete the entry from the Outputs list
	if err := mutations.Delete(key); err != nil {
		return err
	}

	// Add the entry to the Spent list
	return mutations.Set(participationKeyForEventAndSpentOutputID(eventID, output.OutputID), participation.ValueBytes())
}

func (pm *ParticipationManager) endAllParticipationsAtMilestone(eventID EventID, endIndex iotago.MilestoneIndex, mutations kvstore.BatchedMutations) error {
	var innerErr error
	if err := pm.participationStore.Iterate(participationKeyForEventOutputsPrefix(eventID), func(key kvstore.Key, value kvstore.Value) bool {

		participation, err := TrackedParticipationFromBytes(key, value)
		if err != nil {
			innerErr = err

			return false
		}

		participation.EndIndex = endIndex

		// Delete the entry from the Outputs list
		if err := mutations.Delete(key); err != nil {
			innerErr = err

			return false
		}

		// Add the entry to the Spent list
		if err := mutations.Set(participationKeyForEventAndSpentOutputID(eventID, participation.OutputID), participation.ValueBytes()); err != nil {
			innerErr = err

			return false
		}

		return true

	}); err != nil {
		return err
	}

	return innerErr
}

func (pm *ParticipationManager) currentBallotVoteBalanceForQuestionAndAnswer(eventID EventID, milestone iotago.MilestoneIndex, questionIdx uint8, answerIdx uint8) (uint64, error) {
	val, err := pm.participationStore.Get(currentBallotVoteBalanceKeyForQuestionAndAnswer(eventID, milestone, questionIdx, answerIdx))

	if errors.Is(err, kvstore.ErrKeyNotFound) {
		// No votes for this answer yet
		return 0, nil
	}

	if err != nil {
		return 0, err
	}

	ms := marshalutil.New(val)

	return ms.ReadUint64()
}

func (pm *ParticipationManager) accumulatedBallotVoteBalanceForQuestionAndAnswer(eventID EventID, milestone iotago.MilestoneIndex, questionIdx uint8, answerIdx uint8) (uint64, error) {
	val, err := pm.participationStore.Get(accumulatedBallotVoteBalanceKeyForQuestionAndAnswer(eventID, milestone, questionIdx, answerIdx))

	if errors.Is(err, kvstore.ErrKeyNotFound) {
		// No votes for this answer yet
		return 0, nil
	}

	if err != nil {
		return 0, err
	}

	ms := marshalutil.New(val)

	return ms.ReadUint64()
}

func setCurrentBallotVoteBalanceForQuestionAndAnswer(eventID EventID, milestone iotago.MilestoneIndex, questionIdx uint8, answerIdx uint8, current uint64, mutations kvstore.BatchedMutations) error {
	ms := marshalutil.New(8)
	ms.WriteUint64(current)

	return mutations.Set(currentBallotVoteBalanceKeyForQuestionAndAnswer(eventID, milestone, questionIdx, answerIdx), ms.Bytes())
}

func setAccumulatedBallotVoteBalanceForQuestionAndAnswer(eventID EventID, milestone iotago.MilestoneIndex, questionIdx uint8, answerIdx uint8, total uint64, mutations kvstore.BatchedMutations) error {
	ms := marshalutil.New(8)
	ms.WriteUint64(total)

	return mutations.Set(accumulatedBallotVoteBalanceKeyForQuestionAndAnswer(eventID, milestone, questionIdx, answerIdx), ms.Bytes())
}

func (pm *ParticipationManager) startCountingBallotAnswers(event *Event, vote *Participation, milestone iotago.MilestoneIndex, amount uint64, mutations kvstore.BatchedMutations) error {
	questions := event.BallotQuestions()
	for idx, answerByte := range vote.Answers {
		questionIndex := uint8(idx)
		// We already verified, that there are exactly as many answers as questions in the ballot, so no need to check here again
		answerValue := questions[idx].answerValueForByte(answerByte)

		currentVoteBalance, err := pm.currentBallotVoteBalanceForQuestionAndAnswer(vote.EventID, milestone, questionIndex, answerValue)
		if err != nil {
			return err
		}

		voteCount := amount / BallotDenominator
		currentVoteBalance += voteCount

		if err := setCurrentBallotVoteBalanceForQuestionAndAnswer(vote.EventID, milestone, questionIndex, answerValue, currentVoteBalance, mutations); err != nil {
			return err
		}
	}

	return nil
}

func (pm *ParticipationManager) stopCountingBallotAnswers(event *Event, vote *Participation, milestone iotago.MilestoneIndex, amount uint64, mutations kvstore.BatchedMutations) error {
	questions := event.BallotQuestions()
	for idx, answerByte := range vote.Answers {
		questionIndex := uint8(idx)
		// We already verified, that there are exactly as many answers as questions in the ballot, so no need to check here again
		answerValue := questions[idx].answerValueForByte(answerByte)

		currentVoteBalance, err := pm.currentBallotVoteBalanceForQuestionAndAnswer(vote.EventID, milestone, questionIndex, answerValue)
		if err != nil {
			return err
		}

		voteCount := amount / BallotDenominator
		if currentVoteBalance < voteCount {
			// currentVoteBalance can't be less than 0
			return ErrInvalidCurrentBallotVoteBalance
		}
		currentVoteBalance -= voteCount

		if err := setCurrentBallotVoteBalanceForQuestionAndAnswer(vote.EventID, milestone, questionIndex, answerValue, currentVoteBalance, mutations); err != nil {
			return err
		}
	}

	return nil
}

// Staking

func (pm *ParticipationManager) RewardsForTrackedParticipationWithoutLocking(trackedParticipation *TrackedParticipation, atIndex iotago.MilestoneIndex) (uint64, error) {

	event := pm.Event(trackedParticipation.EventID)
	if event == nil {
		return 0, ErrEventNotFound
	}

	if event.StartMilestoneIndex() > atIndex {
		// Event not yet started counting, so skip
		return 0, nil
	}

	if trackedParticipation.StartIndex > atIndex {
		// Participation not started for this index yet
		return 0, nil
	}

	if trackedParticipation.EndIndex > 0 && trackedParticipation.EndIndex <= event.StartMilestoneIndex() {
		// Participation ended before event started
		return 0, nil
	}

	staking := event.Staking()
	if staking == nil {
		return 0, ErrInvalidEvent
	}

	eventMilestoneCountingStart := event.StartMilestoneIndex() + 1

	if eventMilestoneCountingStart < trackedParticipation.StartIndex {
		eventMilestoneCountingStart = trackedParticipation.StartIndex
	}

	var milestonesToCount uint64

	if trackedParticipation.EndIndex == 0 || atIndex < trackedParticipation.EndIndex {
		// Participation has not ended yet, or we are asking for the past of an ended participation, so count including the atIndex milestone
		milestonesToCount = uint64(atIndex + 1 - eventMilestoneCountingStart)
	} else {
		// Participation ended
		milestonesToCount = uint64(trackedParticipation.EndIndex - eventMilestoneCountingStart)
	}

	rewardsPerMilestone := staking.rewardsPerMilestone(trackedParticipation.Amount)
	rewardsForParticipation := rewardsPerMilestone * milestonesToCount

	return rewardsForParticipation, nil
}

func (pm *ParticipationManager) StakingRewardForAddressWithoutLocking(eventID EventID, address iotago.Address, msIndex iotago.MilestoneIndex) (uint64, error) {
	var rewards uint64
	trackedParticipations, err := pm.ParticipationsForAddressWithoutLocking(eventID, address)
	if err != nil {
		return 0, err
	}
	for _, trackedParticipation := range trackedParticipations {
		amount, err := pm.RewardsForTrackedParticipationWithoutLocking(trackedParticipation, msIndex)
		if err != nil {
			return 0, err
		}
		rewards += amount
	}

	return rewards, nil
}

func currentRewardsKeyForEventPrefix(eventID EventID) []byte {
	m := marshalutil.New(33)
	m.WriteByte(ParticipationStoreKeyPrefixStakingCurrentRewards) // 1 byte
	m.WriteBytes(eventID[:])                                      // 32 bytes

	return m.Bytes()
}

func currentRewardsPerMilestoneKeyForEvent(eventID EventID, milestone iotago.MilestoneIndex) []byte {
	m := marshalutil.New(37)
	m.WriteBytes(currentRewardsKeyForEventPrefix(eventID)) // 33 bytes
	m.WriteUint32(milestone)                               // 4 bytes

	return m.Bytes()
}

func totalParticipationStakingKeyForEventPrefix(eventID EventID) []byte {
	m := marshalutil.New(33)
	m.WriteByte(ParticipationStoreKeyPrefixStakingTotalParticipation) // 1 byte
	m.WriteBytes(eventID[:])                                          // 32 bytes

	return m.Bytes()
}

func totalParticipationStakingKeyForEvent(eventID EventID, milestone iotago.MilestoneIndex) []byte {
	m := marshalutil.New(37)
	m.WriteBytes(totalParticipationStakingKeyForEventPrefix(eventID)) // 33 bytes
	m.WriteUint32(milestone)                                          // 4 bytes

	return m.Bytes()
}

type totalStakingParticipation struct {
	staked   uint64
	rewarded uint64
}

func totalStakingParticipationFromBytes(bytes []byte) (*totalStakingParticipation, error) {
	m := marshalutil.New(bytes)
	staked, err := m.ReadUint64()
	if err != nil {
		return nil, err
	}
	rewarded, err := m.ReadUint64()
	if err != nil {
		return nil, err
	}

	return &totalStakingParticipation{
		staked:   staked,
		rewarded: rewarded,
	}, nil
}

func (t *totalStakingParticipation) valueBytes() []byte {
	m := marshalutil.New(16)
	m.WriteUint64(t.staked)   // 8 bytes
	m.WriteUint64(t.rewarded) // 8 bytes

	return m.Bytes()
}

func (pm *ParticipationManager) currentRewardsPerMilestoneForStakingEvent(eventID EventID, milestone iotago.MilestoneIndex) (uint64, error) {
	value, err := pm.participationStore.Get(currentRewardsPerMilestoneKeyForEvent(eventID, milestone))
	if err != nil {
		if errors.Is(err, kvstore.ErrKeyNotFound) {
			return 0, nil
		}

		return 0, err
	}
	m := marshalutil.New(value)

	return m.ReadUint64()
}

func (pm *ParticipationManager) increaseCurrentRewardsPerMilestoneForStakingEvent(eventID EventID, milestone iotago.MilestoneIndex, rewards uint64, mutations kvstore.BatchedMutations) error {
	current, err := pm.currentRewardsPerMilestoneForStakingEvent(eventID, milestone)
	if err != nil {
		return err
	}
	current += rewards

	return pm.setCurrentRewardsPerMilestoneForStakingEvent(eventID, milestone, current, mutations)
}

func (pm *ParticipationManager) decreaseCurrentRewardsPerMilestoneForStakingEvent(eventID EventID, milestone iotago.MilestoneIndex, rewards uint64, mutations kvstore.BatchedMutations) error {
	current, err := pm.currentRewardsPerMilestoneForStakingEvent(eventID, milestone)
	if err != nil {
		return err
	}
	if current < rewards {
		return ErrInvalidCurrentRewardsAmount
	}
	current -= rewards

	return pm.setCurrentRewardsPerMilestoneForStakingEvent(eventID, milestone, current, mutations)
}

func (pm *ParticipationManager) setCurrentRewardsPerMilestoneForStakingEvent(eventID EventID, milestone iotago.MilestoneIndex, rewards uint64, mutations kvstore.BatchedMutations) error {
	m := marshalutil.New(8)
	m.WriteUint64(rewards)

	return mutations.Set(currentRewardsPerMilestoneKeyForEvent(eventID, milestone), m.Bytes())
}

func (pm *ParticipationManager) totalStakingParticipationForEvent(eventID EventID, milestone iotago.MilestoneIndex) (*totalStakingParticipation, error) {
	value, err := pm.participationStore.Get(totalParticipationStakingKeyForEvent(eventID, milestone))
	if err != nil {
		if errors.Is(err, kvstore.ErrKeyNotFound) {
			return &totalStakingParticipation{staked: 0, rewarded: 0}, nil
		}

		return nil, err
	}

	return totalStakingParticipationFromBytes(value)
}

func (pm *ParticipationManager) increaseStakedAmountForStakingEvent(eventID EventID, milestone iotago.MilestoneIndex, stakedAmount uint64, mutations kvstore.BatchedMutations) error {
	total, err := pm.totalStakingParticipationForEvent(eventID, milestone)
	if err != nil {
		return err
	}
	total.staked += stakedAmount

	return mutations.Set(totalParticipationStakingKeyForEvent(eventID, milestone), total.valueBytes())
}

func (pm *ParticipationManager) decreaseStakedAmountForStakingEvent(eventID EventID, milestone iotago.MilestoneIndex, stakedAmount uint64, mutations kvstore.BatchedMutations) error {
	total, err := pm.totalStakingParticipationForEvent(eventID, milestone)
	if err != nil {
		return err
	}
	if total.staked < stakedAmount {
		return ErrInvalidCurrentStakedAmount
	}
	total.staked -= stakedAmount

	return mutations.Set(totalParticipationStakingKeyForEvent(eventID, milestone), total.valueBytes())
}

func (pm *ParticipationManager) setTotalStakingParticipationForEvent(eventID EventID, milestone iotago.MilestoneIndex, total *totalStakingParticipation, mutations kvstore.BatchedMutations) error {
	return mutations.Set(totalParticipationStakingKeyForEvent(eventID, milestone), total.valueBytes())
}

type StakingRewardsConsumer func(address iotago.Address, participation *TrackedParticipation, rewards uint64) bool

func (pm *ParticipationManager) ForEachAddressStakingParticipation(eventID EventID, msIndex iotago.MilestoneIndex, consumer StakingRewardsConsumer) error {
	// We need to lock the ParticipationManager here so that we don't get partial results while the new ledger update is being applied
	pm.RLock()
	defer pm.RUnlock()

	event := pm.EventWithoutLocking(eventID)
	if event == nil {
		return nil
	}

	staking := event.Staking()
	if staking == nil {
		return nil
	}

	var innerErr error
	prefix := participationKeyForEventPrefix(eventID)
	prefixLen := len(prefix)
	if err := pm.participationStore.IterateKeys(prefix, func(key kvstore.Key) bool {

		addressBytes := key[prefixLen:]
		addr, err := iotago.AddressSelector(uint32(addressBytes[0]))
		if err != nil {
			innerErr = err

			return false
		}

		addrLen, err := addr.Deserialize(addressBytes, serializer.DeSeriModeNoValidation, nil)
		if err != nil {
			innerErr = err

			return false
		}

		outputID := iotago.OutputID{}
		copy(outputID[:], key[prefixLen+addrLen:])

		participation, err := pm.ParticipationForOutputIDWithoutLocking(eventID, outputID)
		if err != nil {
			innerErr = err

			return false
		}

		balance, err := pm.RewardsForTrackedParticipationWithoutLocking(participation, msIndex)
		if err != nil {
			innerErr = err

			return false
		}

		return consumer(addr.(iotago.Address), participation, balance)
	}); err != nil {
		return err
	}

	return innerErr
}

// Pruning

func (pm *ParticipationManager) clearStorageForEventID(eventID EventID) error {
	if err := pm.participationStore.DeletePrefix(participationKeyForEventOutputsPrefix(eventID)); err != nil {
		return err
	}
	if err := pm.participationStore.DeletePrefix(participationKeyForEventSpentOutputsPrefix(eventID)); err != nil {
		return err
	}
	if err := pm.participationStore.DeletePrefix(participationKeyForEventPrefix(eventID)); err != nil {
		return err
	}
	if err := pm.participationStore.DeletePrefix(currentBallotVoteBalanceKeyPrefix(eventID)); err != nil {
		return err
	}
	if err := pm.participationStore.DeletePrefix(accumulatedBallotVoteBalanceKeyPrefix(eventID)); err != nil {
		return err
	}
	if err := pm.participationStore.DeletePrefix(totalParticipationStakingKeyForEventPrefix(eventID)); err != nil {
		return err
	}
	if err := pm.participationStore.DeletePrefix(currentRewardsKeyForEventPrefix(eventID)); err != nil {
		return err
	}
	if err := pm.participationStore.DeletePrefix(blockKeyForEventPrefix(eventID)); err != nil {
		return err
	}

	// Clean up old database keys
	if err := pm.participationStore.DeletePrefix([]byte{ParticipationStoreKeyPrefixStakingAddress}); err != nil {
		return err
	}

	return nil
}
