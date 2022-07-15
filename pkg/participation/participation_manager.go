package participation

import (
	"bytes"
	"context"
	"fmt"

	"github.com/pkg/errors"

	"github.com/iotaledger/hive.go/kvstore"
	"github.com/iotaledger/hive.go/serializer/v2"
	"github.com/iotaledger/hive.go/syncutils"
	"github.com/iotaledger/hornet/v2/pkg/model/storage"
	iotago "github.com/iotaledger/iota.go/v3"
)

var (
	ErrParticipationCorruptedStorage               = errors.New("the participation database was not shutdown properly")
	ErrParticipationEventStartedBeforePruningIndex = errors.New("the given participation event started before the pruning index of this node")
	ErrParticipationEventBallotCanOverflow         = errors.New("the given participation duration in combination with the maximum voting weight can overflow uint64")
	ErrParticipationEventStakingCanOverflow        = errors.New("the given participation staking nominator and denominator in combination with the duration can overflow uint64")
	ErrParticipationEventAlreadyExists             = errors.New("the given participation event already exists")
)

type ProtocolParametersProvider func() *iotago.ProtocolParameters
type NodeStatusProvider func() (confirmedIndex iotago.MilestoneIndex, pruningIndex iotago.MilestoneIndex)
type BlockForBlockIDProvider func(blockID iotago.BlockID) (*ParticipationBlock, error)
type OutputForOutputIDProvider func(outputID iotago.OutputID) (*ParticipationOutput, error)
type LedgerUpdatesProvider func(ctx context.Context, startIndex iotago.MilestoneIndex, endIndex iotago.MilestoneIndex, handler func(index iotago.MilestoneIndex, created []*ParticipationOutput, consumed []*ParticipationOutput) error) error

// ParticipationManager is used to track the outcome of participation in the tangle.
type ParticipationManager struct {
	// lock used to secure the state of the ParticipationManager.
	syncutils.RWMutex

	protocolParametersFunc ProtocolParametersProvider
	nodeStatusFunc         NodeStatusProvider
	blockForBlockIDFunc    BlockForBlockIDProvider
	outputForOutputIDFunc  OutputForOutputIDProvider
	ledgerUpdatesFunc      LedgerUpdatesProvider

	// holds the ParticipationManager options.
	opts *Options

	participationStore       kvstore.KVStore
	participationStoreHealth *storage.StoreHealthTracker

	events map[EventID]*Event
}

// the default options applied to the ParticipationManager.
var defaultOptions = []Option{
	WithTagMessage("PARTICIPATE"),
}

// Options define options for the ParticipationManager.
type Options struct {
	// defines the tag payload to track
	tagMessage []byte
}

// applies the given Option.
func (so *Options) apply(opts ...Option) {
	for _, opt := range opts {
		opt(so)
	}
}

// WithTagMessage defines the ParticipationManager tag payload to track.
func WithTagMessage(tagMessage string) Option {
	return func(opts *Options) {
		opts.tagMessage = []byte(tagMessage)
	}
}

// Option is a function setting a ParticipationManager option.
type Option func(opts *Options)

// NewManager creates a new ParticipationManager instance.
func NewManager(
	participationStore kvstore.KVStore,
	protocolParametersProvider ProtocolParametersProvider,
	nodeStatusProvider NodeStatusProvider,
	blockForBlockIDProvider BlockForBlockIDProvider,
	outputForOutputIDProvider OutputForOutputIDProvider,
	ledgerUpdatesProvider LedgerUpdatesProvider,
	opts ...Option) (*ParticipationManager, error) {

	options := &Options{}
	options.apply(defaultOptions...)
	options.apply(opts...)

	healthTracker, err := storage.NewStoreHealthTracker(participationStore, DBVersionParticipation)
	if err != nil {
		return nil, err
	}

	manager := &ParticipationManager{
		protocolParametersFunc:   protocolParametersProvider,
		nodeStatusFunc:           nodeStatusProvider,
		blockForBlockIDFunc:      blockForBlockIDProvider,
		outputForOutputIDFunc:    outputForOutputIDProvider,
		ledgerUpdatesFunc:        ledgerUpdatesProvider,
		participationStore:       participationStore,
		participationStoreHealth: healthTracker,
		opts:                     options,
	}

	err = manager.init()
	if err != nil {
		return nil, err
	}

	return manager, nil
}

func (pm *ParticipationManager) init() error {

	corrupted, err := pm.participationStoreHealth.IsCorrupted()
	if err != nil {
		return err
	}
	if corrupted {
		return ErrParticipationCorruptedStorage
	}

	correctDatabasesVersion, err := pm.participationStoreHealth.CheckCorrectDatabaseVersion()
	if err != nil {
		return err
	}

	if !correctDatabasesVersion {
		databaseVersionUpdated, err := pm.participationStoreHealth.UpdateDatabaseVersion()
		if err != nil {
			return err
		}

		if !databaseVersionUpdated {
			return errors.New("HORNET participation database version mismatch. The database scheme was updated. Please delete the database folder and start with a new snapshot.")
		}
	}

	// Read events from storage
	events, err := pm.loadEvents()
	if err != nil {
		return err
	}
	pm.events = events

	// Mark the database as corrupted here and as clean when we shut it down
	return pm.participationStoreHealth.MarkCorrupted()
}

// CloseDatabase flushes the store and closes the underlying database
func (pm *ParticipationManager) CloseDatabase() error {
	var flushAndCloseError error

	if err := pm.participationStoreHealth.MarkHealthy(); err != nil {
		flushAndCloseError = err
	}

	if err := pm.participationStore.Flush(); err != nil {
		flushAndCloseError = err
	}
	if err := pm.participationStore.Close(); err != nil {
		flushAndCloseError = err
	}
	return flushAndCloseError
}

func (pm *ParticipationManager) LedgerIndex() iotago.MilestoneIndex {
	pm.RLock()
	defer pm.RUnlock()
	index, err := pm.readLedgerIndex()
	if err != nil {
		panic(err)
	}
	return index
}

func (pm *ParticipationManager) eventIDsWithoutLocking(eventPayloadType ...uint32) []EventID {
	events := pm.events
	if len(eventPayloadType) > 0 {
		events = filteredEvents(events, eventPayloadType)
	}

	var ids []EventID
	for id := range events {
		ids = append(ids, id)
	}
	return ids
}

// EventIDs return the IDs of all known events. Can be optionally filtered by event payload type.
func (pm *ParticipationManager) EventIDs(eventPayloadType ...uint32) []EventID {
	pm.RLock()
	defer pm.RUnlock()
	return pm.eventIDsWithoutLocking(eventPayloadType...)
}

func (pm *ParticipationManager) eventsWithoutLocking() map[EventID]*Event {
	events := make(map[EventID]*Event)
	for id, e := range pm.events {
		events[id] = e
	}
	return events
}

// Events returns all known events
func (pm *ParticipationManager) Events() map[EventID]*Event {
	pm.RLock()
	defer pm.RUnlock()
	return pm.eventsWithoutLocking()
}

func filteredEvents(events map[EventID]*Event, filterPayloadTypes []uint32) map[EventID]*Event {

	filtered := make(map[EventID]*Event)
eventLoop:
	for id, event := range events {
		eventPayloadType := event.payloadType()
		for _, payloadType := range filterPayloadTypes {
			if payloadType == eventPayloadType {
				filtered[id] = event
			}
			continue eventLoop
		}
	}
	return filtered
}

// EventsAcceptingParticipation returns the events that are currently accepting participation, i.e. commencing or in the holding period.
func (pm *ParticipationManager) EventsAcceptingParticipation(index iotago.MilestoneIndex) map[EventID]*Event {
	return filterEvents(pm.Events(), index, func(e *Event, index iotago.MilestoneIndex) bool {
		return e.IsAcceptingParticipation(index)
	})
}

// EventsCountingParticipation returns the events that are currently actively counting participation, i.e. in the holding period
func (pm *ParticipationManager) EventsCountingParticipation(index iotago.MilestoneIndex) map[EventID]*Event {
	return filterEvents(pm.Events(), index, func(e *Event, index iotago.MilestoneIndex) bool {
		return e.IsCountingParticipation(index)
	})
}

// StoreEvent accepts a new Event the manager should track.
// The current confirmed milestone index needs to be provided, so that the manager can check if the event can be added.
func (pm *ParticipationManager) StoreEvent(event *Event) (EventID, error) {
	pm.Lock()
	defer pm.Unlock()

	protoParas := pm.protocolParametersFunc()
	confirmedIndex, pruningIndex := pm.nodeStatusFunc()

	eventID, err := event.ID()
	if err != nil {
		return NullEventID, err
	}

	if _, exists := pm.events[eventID]; exists {
		return NullEventID, ErrParticipationEventAlreadyExists
	}

	if event.BallotCanOverflow(protoParas) {
		return NullEventID, ErrParticipationEventBallotCanOverflow
	}

	if event.StakingCanOverflow(protoParas) {
		return NullEventID, ErrParticipationEventStakingCanOverflow
	}

	if confirmedIndex >= event.CommenceMilestoneIndex() {

		if pruningIndex >= event.CommenceMilestoneIndex() {
			return NullEventID, ErrParticipationEventStartedBeforePruningIndex
		}

		if err := pm.calculatePastParticipationForEvent(event); err != nil {
			return NullEventID, err
		}
	}

	if _, err = pm.storeEvent(event); err != nil {
		return NullEventID, err
	}
	pm.events[eventID] = event

	return eventID, err
}

// Event returns the event for the given eventID if it exists
func (pm *ParticipationManager) Event(eventID EventID) *Event {
	pm.RLock()
	defer pm.RUnlock()
	return pm.events[eventID]
}

// EventWithoutLocking returns the event for the given eventID if it exists
func (pm *ParticipationManager) EventWithoutLocking(eventID EventID) *Event {
	return pm.events[eventID]
}

// DeleteEvent deletes the event for the given eventID if it exists, else returns ErrEventNotFound.
func (pm *ParticipationManager) DeleteEvent(eventID EventID) error {
	pm.Lock()
	defer pm.Unlock()

	event := pm.events[eventID]
	if event == nil {
		return ErrEventNotFound
	}

	if err := pm.clearStorageForEventID(eventID); err != nil {
		return err
	}

	if err := pm.deleteEvent(eventID); err != nil {
		return err
	}

	delete(pm.events, eventID)
	return nil
}

func (pm *ParticipationManager) calculatePastParticipationForEvent(event *Event) error {

	eventID, err := event.ID()
	if err != nil {
		return err
	}

	// Make sure we have no data from a previous import in the storage
	if err := pm.clearStorageForEventID(eventID); err != nil {
		return err
	}

	events := make(map[EventID]*Event)
	events[eventID] = event

	ledgerIndex, err := pm.readLedgerIndex()
	if err != nil {
		return err
	}

	endIndex := event.EndMilestoneIndex()
	if ledgerIndex < endIndex {
		endIndex = ledgerIndex
	}

	err = pm.ledgerUpdatesFunc(context.Background(), event.CommenceMilestoneIndex(), endIndex, func(index iotago.MilestoneIndex, created []*ParticipationOutput, consumed []*ParticipationOutput) error {
		for _, output := range created {
			if err := pm.applyNewUTXOForEvents(index, output, events); err != nil {
				return err
			}
		}

		for _, spent := range consumed {
			if err := pm.applySpentUTXOForEvents(index, spent, events); err != nil {
				return err
			}
		}

		if err := pm.applyNewConfirmedMilestoneIndexForEvents(index, events); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func (pm *ParticipationManager) ApplyNewLedgerUpdate(index iotago.MilestoneIndex, created []*ParticipationOutput, consumed []*ParticipationOutput) error {
	// Lock the state to avoid anyone reading partial results while we apply the state
	pm.Lock()
	defer pm.Unlock()

	if err := pm.storeLedgerIndex(index); err != nil {
		return err
	}

	acceptingEvents := filterEvents(pm.eventsWithoutLocking(), index, func(e *Event, index iotago.MilestoneIndex) bool {
		return e.ShouldAcceptParticipation(index)
	})

	// No events accepting participation, so no work to be done
	if len(acceptingEvents) == 0 {
		return nil
	}

	for _, newOutput := range created {
		if err := pm.applyNewUTXOForEvents(index, newOutput, acceptingEvents); err != nil {
			return err
		}
	}
	for _, spent := range consumed {
		if err := pm.applySpentUTXOForEvents(index, spent, acceptingEvents); err != nil {
			return err
		}
	}

	if err := pm.applyNewConfirmedMilestoneIndexForEvents(index, acceptingEvents); err != nil {
		return err
	}
	return nil
}

// applyNewUTXOForEvents checks if the new UTXO is part of a participation transaction.
// The following rules must be satisfied:
// 	- Must be a value transaction
// 	- Inputs must all come from the same address. Multiple inputs are allowed.
// 	- Has a singular output going to the same address as all input addresses.
// 	- Output Type 0 (SigLockedSingleOutput) and Type 1 (SigLockedDustAllowanceOutput) are both valid for this.
// 	- The Indexation must match the configured Indexation.
//  - The participation data must be parseable.
func (pm *ParticipationManager) applyNewUTXOForEvents(index iotago.MilestoneIndex, newOutput *ParticipationOutput, events map[EventID]*Event) error {
	block, err := pm.blockForBlockIDFunc(newOutput.BlockID)
	if err != nil {
		return err
	}

	depositOutput, participations, err := pm.ParticipationsFromBlock(block, index)
	if err != nil {
		return err
	}

	if depositOutput == nil {
		// No output with participations, so ignore
		return nil
	}

	validParticipations := filterValidParticipationsForEvents(index, participations, events)

	if len(validParticipations) == 0 {
		// No participations for anything we are tracking
		return nil
	}

	mutations, err := pm.participationStore.Batched()
	if err != nil {
		return err
	}

	for _, participation := range validParticipations {

		// Store the block holding the participation for this event
		if err := pm.storeBlockForEvent(participation.EventID, block, mutations); err != nil {
			mutations.Cancel()
			return err
		}

		// Store the participation started at this milestone
		if err := pm.startParticipationAtMilestone(participation.EventID, depositOutput, index, mutations); err != nil {
			mutations.Cancel()
			return err
		}

		event, ok := events[participation.EventID]
		if !ok {
			mutations.Cancel()
			return nil
		}

		switch payload := event.Payload.(type) {
		case *Ballot:
			// Count the new ballot votes by increasing the current vote balance
			if err := pm.startCountingBallotAnswers(event, participation, index, depositOutput.Deposit, mutations); err != nil {
				mutations.Cancel()
				return err
			}
		case *Staking:
			// Increase the staked amount
			if err := pm.increaseStakedAmountForStakingEvent(participation.EventID, index, depositOutput.Deposit, mutations); err != nil {
				mutations.Cancel()
				return err
			}
			// Increase the staking rewards
			if err := pm.increaseCurrentRewardsPerMilestoneForStakingEvent(participation.EventID, index, payload.rewardsPerMilestone(depositOutput.Deposit), mutations); err != nil {
				mutations.Cancel()
				return err
			}
		}
	}

	return mutations.Commit()
}

// applySpentUTXOForEvents checks if the spent UTXO was part of a participation transaction.
func (pm *ParticipationManager) applySpentUTXOForEvents(index iotago.MilestoneIndex, spent *ParticipationOutput, events map[EventID]*Event) error {

	// Fetch the block, this must have been stored for at least one of the events
	var msg *ParticipationBlock
	for eID := range events {
		// Check if we tracked the participation initially, event.g. saved the Block that created this UTXO
		blockForEvent, err := pm.BlockForEventAndBlockID(eID, spent.BlockID)
		if err != nil {
			return err
		}
		if blockForEvent != nil {
			msg = blockForEvent
			break
		}
	}

	if msg == nil {
		// This UTXO had no valid participation, so we did not store the block for it
		return nil
	}

	txEssenceTaggedData := msg.TransactionEssenceTaggedData()
	if txEssenceTaggedData == nil {
		// We tracked this participation before, and now we don't have its taggedData, so something happened
		return ErrInvalidPreviouslyTrackedParticipation
	}

	participations, err := participationFromTaggedData(txEssenceTaggedData)
	if err != nil {
		return err
	}

	validParticipations := filterValidParticipationsForEvents(index, participations, events)

	if len(validParticipations) == 0 {
		// This might happen if the participation ended, and we spend the UTXO
		return nil
	}

	mutations, err := pm.participationStore.Batched()
	if err != nil {
		return err
	}

	for _, participation := range validParticipations {

		// Store the participation ended at this milestone
		if err := pm.endParticipationAtMilestone(participation.EventID, spent, index, mutations); err != nil {
			if errors.Is(err, ErrUnknownParticipation) {
				// This was a previously invalid participation, so we did not track it
				continue
			}
			mutations.Cancel()
			return err
		}

		event, ok := events[participation.EventID]
		if !ok {
			mutations.Cancel()
			return nil
		}

		switch payload := event.Payload.(type) {
		case *Ballot:
			// Count the spent votes by decreasing the current vote balance
			if err := pm.stopCountingBallotAnswers(event, participation, index, spent.Deposit, mutations); err != nil {
				mutations.Cancel()
				return err
			}
		case *Staking:
			// Decrease the staked amount
			if err := pm.decreaseStakedAmountForStakingEvent(participation.EventID, index, spent.Deposit, mutations); err != nil {
				mutations.Cancel()
				return err
			}
			// Decrease the staking rewards
			if err := pm.decreaseCurrentRewardsPerMilestoneForStakingEvent(participation.EventID, index, payload.rewardsPerMilestone(spent.Deposit), mutations); err != nil {
				mutations.Cancel()
				return err
			}
		}
	}

	return mutations.Commit()
}

// applyNewConfirmedMilestoneIndexForEvents iterates over each counting ballot participation and applies the current vote balance for each question to the total vote balance
func (pm *ParticipationManager) applyNewConfirmedMilestoneIndexForEvents(index iotago.MilestoneIndex, events map[EventID]*Event) error {

	mutations, err := pm.participationStore.Batched()
	if err != nil {
		return err
	}

	// Iterate over all known events and increase the one that are currently counting
	for eventID, event := range events {
		shouldCountParticipation := event.ShouldCountParticipation(index)

		processAnswerValueBalances := func(questionIndex uint8, answerValue uint8) error {

			// Read the accumulated value from the previous milestone, add the current vote and store accumulated for this milestone

			currentBalance, err := pm.currentBallotVoteBalanceForQuestionAndAnswer(eventID, index, questionIndex, answerValue)
			if err != nil {
				mutations.Cancel()
				return err
			}

			if event.EndMilestoneIndex() > index {
				// Event not ended yet, so copy the current for the next milestone already
				if err := setCurrentBallotVoteBalanceForQuestionAndAnswer(eventID, index+1, questionIndex, answerValue, currentBalance, mutations); err != nil {
					mutations.Cancel()
					return err
				}
			}

			if shouldCountParticipation {
				accumulatedBalance, err := pm.accumulatedBallotVoteBalanceForQuestionAndAnswer(eventID, index-1, questionIndex, answerValue)
				if err != nil {
					mutations.Cancel()
					return err
				}

				// Add current vote balance to accumulated vote balance for each answer
				newAccumulatedBalance := accumulatedBalance + currentBalance

				if err := setAccumulatedBallotVoteBalanceForQuestionAndAnswer(eventID, index, questionIndex, answerValue, newAccumulatedBalance, mutations); err != nil {
					mutations.Cancel()
					return err
				}
			}

			return nil
		}

		// For each participation, iterate over all questions
		for idx, question := range event.BallotQuestions() {
			questionIndex := uint8(idx)

			// For each question, iterate over all answers values
			for _, answer := range question.QuestionAnswers() {
				if err := processAnswerValueBalances(questionIndex, answer.Value); err != nil {
					return err
				}
			}
			if err := processAnswerValueBalances(questionIndex, AnswerValueSkipped); err != nil {
				return err
			}
			if err := processAnswerValueBalances(questionIndex, AnswerValueInvalid); err != nil {
				return err
			}
		}

		staking := event.Staking()
		if staking != nil {

			currentRewardsPerMilestone, err := pm.currentRewardsPerMilestoneForStakingEvent(eventID, index)
			if err != nil {
				mutations.Cancel()
				return err
			}

			if event.EndMilestoneIndex() > index {
				// Event not ended yet, so copy the current rewards for the next milestone already
				if err := pm.setCurrentRewardsPerMilestoneForStakingEvent(eventID, index+1, currentRewardsPerMilestone, mutations); err != nil {
					mutations.Cancel()
					return err
				}
			}

			total, err := pm.totalStakingParticipationForEvent(eventID, index)
			if err != nil {
				mutations.Cancel()
				return err
			}

			if shouldCountParticipation {
				total.rewarded += currentRewardsPerMilestone
			}

			if err := pm.setTotalStakingParticipationForEvent(eventID, index, total, mutations); err != nil {
				mutations.Cancel()
				return err
			}

			if event.EndMilestoneIndex() > index {
				// Event not ended yet, so copy the current total for the next milestone already
				if err := pm.setTotalStakingParticipationForEvent(eventID, index+1, total, mutations); err != nil {
					mutations.Cancel()
					return err
				}
			}
		}

		// End all participation if event is ending this milestone
		if event.EndMilestoneIndex() == index {
			if err := pm.endAllParticipationsAtMilestone(eventID, index+1, mutations); err != nil {
				mutations.Cancel()
				return err
			}
		}
	}

	return mutations.Commit()
}

func filterValidParticipationsForEvents(index iotago.MilestoneIndex, votes []*Participation, events map[EventID]*Event) []*Participation {

	var validParticipations []*Participation
	for _, vote := range votes {

		// Check that we want to handle the event for the given participation
		event, found := events[vote.EventID]
		if !found {
			continue
		}

		// Check that the event is accepting participations
		if !event.ShouldAcceptParticipation(index) {
			continue
		}

		// Check that the amount of answers equals the questions in the ballot
		if len(vote.Answers) != len(event.BallotQuestions()) {
			continue
		}

		validParticipations = append(validParticipations, vote)
	}

	return validParticipations
}

func participationFromTaggedData(taggedData *iotago.TaggedData) ([]*Participation, error) {

	// try to parse the votes payload
	parsedVotes := &ParticipationPayload{}
	if _, err := parsedVotes.Deserialize(taggedData.Data, serializer.DeSeriModePerformValidation, nil); err != nil {
		// votes payload can't be parsed => ignore votes
		return nil, fmt.Errorf("no valid votes payload")
	}

	var votes []*Participation
	for _, vote := range parsedVotes.Participations {
		votes = append(votes, vote)
	}

	return votes, nil
}

func serializedAddressFromOutput(output *iotago.BasicOutput) ([]byte, error) {
	outputAddress, err := output.UnlockConditionSet().Address().Address.Serialize(serializer.DeSeriModeNoValidation, nil)
	if err != nil {
		return nil, err
	}
	return outputAddress, nil
}

func (pm *ParticipationManager) ParticipationsFromBlock(msg *ParticipationBlock, msIndex iotago.MilestoneIndex) (*ParticipationOutput, []*Participation, error) {
	transaction := msg.Transaction()
	if transaction == nil {
		// Do not handle outputs from migrations
		// This output was created by a migration in a milestone payload.
		return nil, nil, nil
	}

	txEssence := msg.TransactionEssence()
	if txEssence == nil {
		// if the block was included, there must be a transaction payload essence
		return nil, nil, errors.New("no transaction transactionEssence found")
	}

	txEssenceTaggedData := msg.TransactionEssenceTaggedData()
	if txEssenceTaggedData == nil {
		// no need to check if there is not taggedData payload
		return nil, nil, nil
	}

	// the tag of the transaction payload must match our configured tag
	if !bytes.Equal(txEssenceTaggedData.Tag, pm.opts.tagMessage) {
		return nil, nil, nil
	}

	// only a single output is allowed
	if len(txEssence.Outputs) != 1 {
		return nil, nil, nil
	}

	// only BasicOutput are allowed as output type
	var depositOutput *iotago.BasicOutput
	switch o := txEssence.Outputs[0].(type) {
	case *iotago.BasicOutput:
		depositOutput = o
	default:
		return nil, nil, nil
	}

	outputAddress, err := serializedAddressFromOutput(depositOutput)
	if err != nil {
		return nil, nil, nil
	}

	// collect inputs
	var inputOutputs []*ParticipationOutput
	for _, input := range msg.TransactionEssenceUTXOInputs() {
		output, err := pm.outputForOutputIDFunc(input)
		if err != nil {
			return nil, nil, err
		}
		inputOutputs = append(inputOutputs, output)
	}

	// check if at least 1 input comes from the same address as the output
	containsInputFromSameAddress := false
	for _, input := range inputOutputs {
		inputAddress, err := input.Address.Serialize(serializer.DeSeriModeNoValidation, nil)
		if err != nil {
			return nil, nil, nil
		}

		if bytes.Equal(outputAddress, inputAddress) {
			containsInputFromSameAddress = true
			break
		}
	}

	if !containsInputFromSameAddress {
		// no input address match the output address =>  not a valid voting transaction
		return nil, nil, nil
	}

	txID, err := transaction.ID()
	if err != nil {
		return nil, nil, err
	}

	participations, err := participationFromTaggedData(txEssenceTaggedData)
	if err != nil {
		return nil, nil, nil
	}

	outputID := iotago.OutputIDFromTransactionIDAndIndex(txID, 0)

	return &ParticipationOutput{
		BlockID:  msg.BlockID,
		OutputID: outputID,
		Address:  depositOutput.UnlockConditionSet().Address().Address,
		Deposit:  depositOutput.Deposit(),
	}, participations, nil
}

func filterEvents(events map[EventID]*Event, index iotago.MilestoneIndex, includeFunc func(e *Event, index iotago.MilestoneIndex) bool) map[EventID]*Event {
	filtered := make(map[EventID]*Event)
	for id, event := range events {
		if includeFunc(event, index) {
			filtered[id] = event
		}
	}
	return filtered
}
