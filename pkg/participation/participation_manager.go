package participation

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/iotaledger/hive.go/kvstore"
	"github.com/iotaledger/hive.go/runtime/syncutils"
	"github.com/iotaledger/hive.go/serializer/v2"
	iotago "github.com/iotaledger/iota.go/v3"
)

const (
	StorePrefixHealth byte = 255
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
type BlockForBlockIDProvider func(ctx context.Context, blockID iotago.BlockID) (*ParticipationBlock, error)
type OutputForOutputIDProvider func(ctx context.Context, outputID iotago.OutputID) (*ParticipationOutput, error)
type LedgerUpdatesProvider func(ctx context.Context, startIndex iotago.MilestoneIndex, endIndex iotago.MilestoneIndex, handler func(index iotago.MilestoneIndex, created []*ParticipationOutput, consumed []*ParticipationOutput) error) error

// Manager is used to track the outcome of participation in the tangle.
type Manager struct {
	// lock used to secure the state of the Manager.
	syncutils.RWMutex

	//nolint:containedctx // false positive
	ctx context.Context

	protocolParametersFunc ProtocolParametersProvider
	nodeStatusFunc         NodeStatusProvider
	blockForBlockIDFunc    BlockForBlockIDProvider
	outputForOutputIDFunc  OutputForOutputIDProvider
	ledgerUpdatesFunc      LedgerUpdatesProvider

	// holds the Manager options.
	opts *Options

	participationStore       kvstore.KVStore
	participationStoreHealth *kvstore.StoreHealthTracker

	events map[EventID]*Event
}

// the default options applied to the Manager.
var defaultOptions = []Option{
	WithTagMessage("PARTICIPATE"),
}

// Options define options for the Manager.
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

// WithTagMessage defines the Manager tag payload to track.
func WithTagMessage(tagMessage string) Option {
	return func(opts *Options) {
		opts.tagMessage = []byte(tagMessage)
	}
}

// Option is a function setting a Manager option.
type Option func(opts *Options)

// NewManager creates a new Manager instance.
func NewManager(
	ctx context.Context,
	participationStore kvstore.KVStore,
	protocolParametersProvider ProtocolParametersProvider,
	nodeStatusProvider NodeStatusProvider,
	blockForBlockIDProvider BlockForBlockIDProvider,
	outputForOutputIDProvider OutputForOutputIDProvider,
	ledgerUpdatesProvider LedgerUpdatesProvider,
	opts ...Option) (*Manager, error) {

	options := &Options{}
	options.apply(defaultOptions...)
	options.apply(opts...)

	healthTracker, err := kvstore.NewStoreHealthTracker(participationStore, []byte{StorePrefixHealth}, DBVersionParticipation, nil)
	if err != nil {
		return nil, err
	}

	manager := &Manager{
		ctx:                      ctx,
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

func (pm *Manager) init() error {

	corrupted, err := pm.participationStoreHealth.IsCorrupted()
	if err != nil {
		return err
	}
	if corrupted {
		return ErrParticipationCorruptedStorage
	}

	correctDatabasesVersion, err := pm.participationStoreHealth.CheckCorrectStoreVersion()
	if err != nil {
		return err
	}

	if !correctDatabasesVersion {
		databaseVersionUpdated, err := pm.participationStoreHealth.UpdateStoreVersion()
		if err != nil {
			return err
		}

		if !databaseVersionUpdated {
			//nolint:revive // this error message is shown to the user
			return errors.New("participation database version mismatch. The database scheme was updated. Please delete the database folder and start again.")
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

// CloseDatabase flushes the store and closes the underlying database.
func (pm *Manager) CloseDatabase() error {
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

func (pm *Manager) LedgerIndex() iotago.MilestoneIndex {
	pm.RLock()
	defer pm.RUnlock()
	index, err := pm.readLedgerIndex()
	if err != nil {
		panic(fmt.Errorf("failed to read ledger index: %w", err))
	}

	return index
}

func (pm *Manager) eventIDsWithoutLocking(eventPayloadType ...uint32) []EventID {
	events := pm.events
	if len(eventPayloadType) > 0 {
		events = filteredEvents(events, eventPayloadType)
	}

	ids := make([]EventID, len(events))
	i := 0
	for id := range events {
		ids[i] = id
		i++
	}

	return ids
}

// EventIDs return the IDs of all known events. Can be optionally filtered by event payload type.
func (pm *Manager) EventIDs(eventPayloadType ...uint32) []EventID {
	pm.RLock()
	defer pm.RUnlock()

	return pm.eventIDsWithoutLocking(eventPayloadType...)
}

func (pm *Manager) eventsWithoutLocking() map[EventID]*Event {
	events := make(map[EventID]*Event)
	for id, e := range pm.events {
		events[id] = e
	}

	return events
}

// Events returns all known events.
func (pm *Manager) Events() map[EventID]*Event {
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
func (pm *Manager) EventsAcceptingParticipation(index iotago.MilestoneIndex) map[EventID]*Event {
	return filterEvents(pm.Events(), index, func(e *Event, index iotago.MilestoneIndex) bool {
		return e.IsAcceptingParticipation(index)
	})
}

// EventsCountingParticipation returns the events that are currently actively counting participation, i.e. in the holding period.
func (pm *Manager) EventsCountingParticipation(index iotago.MilestoneIndex) map[EventID]*Event {
	return filterEvents(pm.Events(), index, func(e *Event, index iotago.MilestoneIndex) bool {
		return e.IsCountingParticipation(index)
	})
}

// StoreEvent accepts a new Event the manager should track.
// The current confirmed milestone index needs to be provided, so that the manager can check if the event can be added.
func (pm *Manager) StoreEvent(event *Event) (EventID, error) {
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

// Event returns the event for the given eventID if it exists.
func (pm *Manager) Event(eventID EventID) *Event {
	pm.RLock()
	defer pm.RUnlock()

	return pm.events[eventID]
}

// EventWithoutLocking returns the event for the given eventID if it exists.
func (pm *Manager) EventWithoutLocking(eventID EventID) *Event {
	return pm.events[eventID]
}

// DeleteEvent deletes the event for the given eventID if it exists, else returns ErrEventNotFound.
func (pm *Manager) DeleteEvent(eventID EventID) error {
	pm.Lock()
	defer pm.Unlock()

	//nolint:ifshort // false positive
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

func (pm *Manager) calculatePastParticipationForEvent(event *Event) error {

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

	err = pm.ledgerUpdatesFunc(pm.ctx, event.CommenceMilestoneIndex(), endIndex, func(index iotago.MilestoneIndex, created []*ParticipationOutput, consumed []*ParticipationOutput) error {
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

		return pm.applyNewConfirmedMilestoneIndexForEvents(index, events)
	})
	if err != nil {
		return err
	}

	return nil
}

func (pm *Manager) ApplyNewLedgerUpdate(index iotago.MilestoneIndex, created []*ParticipationOutput, consumed []*ParticipationOutput) error {
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

	return pm.applyNewConfirmedMilestoneIndexForEvents(index, acceptingEvents)
}

// applyNewUTXOForEvents checks if the new UTXO is part of a participation transaction.
// The following rules must be satisfied:
//   - Must be a value transaction
//   - Inputs must all come from the same address. Multiple inputs are allowed.
//   - Has a singular output going to the same address as all input addresses.
//   - Output Type 0 (SigLockedSingleOutput) and Type 1 (SigLockedDustAllowanceOutput) are both valid for this.
//   - The Indexation must match the configured Indexation.
//   - The participation data must be parseable.
func (pm *Manager) applyNewUTXOForEvents(index iotago.MilestoneIndex, newOutput *ParticipationOutput, events map[EventID]*Event) error {
	ctx, cancel := context.WithTimeout(pm.ctx, 5*time.Second)
	defer cancel()

	block, err := pm.blockForBlockIDFunc(ctx, newOutput.BlockID)
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
func (pm *Manager) applySpentUTXOForEvents(index iotago.MilestoneIndex, spent *ParticipationOutput, events map[EventID]*Event) error {

	// Fetch the block, this must have been stored for at least one of the events
	var block *ParticipationBlock
	for eID := range events {
		// Check if we tracked the participation initially, event.g. saved the Block that created this UTXO
		blockForEvent, err := pm.BlockForEventAndBlockID(eID, spent.BlockID)
		if err != nil {
			return err
		}
		if blockForEvent != nil {
			block = blockForEvent

			break
		}
	}

	if block == nil {
		// This UTXO had no valid participation, so we did not store the block for it
		return nil
	}

	txEssenceTaggedData := block.TransactionEssenceTaggedData()
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

// applyNewConfirmedMilestoneIndexForEvents iterates over each counting ballot participation and applies the current vote balance for each question to the total vote balance.
func (pm *Manager) applyNewConfirmedMilestoneIndexForEvents(index iotago.MilestoneIndex, events map[EventID]*Event) error {

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

	//nolint:prealloc // false positive
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

	votes := make([]*Participation, len(parsedVotes.Participations))
	copy(votes, parsedVotes.Participations)

	return votes, nil
}

func (pm *Manager) ParticipationsFromBlock(block *ParticipationBlock, _ iotago.MilestoneIndex) (*ParticipationOutput, []*Participation, error) {
	transaction := block.Transaction()
	if transaction == nil {
		// Do not handle outputs from migrations
		// This output was created by a migration in a milestone payload.
		return nil, nil, nil
	}

	txEssence := block.TransactionEssence()
	if txEssence == nil {
		// if the block was included, there must be a transaction payload essence
		return nil, nil, errors.New("no transaction transactionEssence found")
	}

	txEssenceTaggedData := block.TransactionEssenceTaggedData()
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

	// don't allow any timelocks, expirations or return conditions
	depositOutputUnlockConditions := depositOutput.UnlockConditionSet()
	if depositOutputUnlockConditions.HasExpirationCondition() || depositOutputUnlockConditions.HasTimelockCondition() || depositOutputUnlockConditions.HasStorageDepositReturnCondition() {
		return nil, nil, nil
	}

	containsSignatureFromOutputAddress := false
	for _, unlock := range transaction.Unlocks {
		signatureUnlock, matches := unlock.(*iotago.SignatureUnlock)
		if !matches {
			continue
		}

		ed25519Signature, matches := signatureUnlock.Signature.(*iotago.Ed25519Signature)
		if !matches {
			continue
		}

		unlockAddress := iotago.Ed25519AddressFromPubKey(ed25519Signature.PublicKey[:])

		if depositOutputUnlockConditions.Address().Address.Equal(&unlockAddress) {
			containsSignatureFromOutputAddress = true
			break
		}
	}

	if !containsSignatureFromOutputAddress {
		// no signature in the transaction matches the output address =>  not a valid voting transaction
		//nolint:nilnil // nil, nil, nil is ok in this context, even if it is not go idiomatic
		return nil, nil, nil
	}

	txID, err := transaction.ID()
	if err != nil {
		return nil, nil, err
	}

	participations, err := participationFromTaggedData(txEssenceTaggedData)
	if err != nil {
		//nolint:nilnil,nilerr // nil, nil, nil is ok in this context, even if it is not go idiomatic
		return nil, nil, nil
	}

	outputID := iotago.OutputIDFromTransactionIDAndIndex(txID, 0)

	return &ParticipationOutput{
		BlockID:  block.BlockID,
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

func (pm *Manager) AnswersForTrackedParticipation(trackedParticipation *TrackedParticipation) ([]byte, error) {
	blockForEvent, err := pm.BlockForEventAndBlockID(trackedParticipation.EventID, trackedParticipation.BlockID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch answers for tracked participation, eventID: %s, blockID: %s, error: failed to fetch block: %w", trackedParticipation.EventID.ToHex(), trackedParticipation.BlockID.ToHex(), err)
	}

	if blockForEvent == nil {
		return nil, fmt.Errorf("failed to fetch answers for tracked participation, eventID: %s, blockID: %s, error: block not found", trackedParticipation.EventID.ToHex(), trackedParticipation.BlockID.ToHex())
	}

	txEssenceTaggedData := blockForEvent.TransactionEssenceTaggedData()
	if txEssenceTaggedData == nil {
		// we only store blocks that contain a valid transaction essence payload with tagged data anyway,
		// but its ok to check again here
		return nil, fmt.Errorf("failed to fetch answers for tracked participation, eventID: %s, blockID: %s, error: no transaction essence tagged data payload found", trackedParticipation.EventID.ToHex(), trackedParticipation.BlockID.ToHex())
	}

	participations, err := participationFromTaggedData(txEssenceTaggedData)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch answers for tracked participation, eventID: %s, blockID: %s, error: %w", trackedParticipation.EventID.ToHex(), trackedParticipation.BlockID.ToHex(), err)
	}

	for _, participation := range participations {
		if participation.EventID != trackedParticipation.EventID {
			continue
		}

		// found the correct event ID, return the answers
		return participation.Answers, nil
	}

	return nil, fmt.Errorf("failed to fetch answers for tracked participation, eventID: %s, blockID: %s, error: tracked participation not found in transaction essence tagged data payload", trackedParticipation.EventID.ToHex(), trackedParticipation.BlockID.ToHex())
}
