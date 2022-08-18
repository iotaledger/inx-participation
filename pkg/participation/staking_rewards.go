package participation

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"

	iotago "github.com/iotaledger/iota.go/v3"
)

// AddressReward holds the amount and token symbol for a certain reward.
type AddressReward struct {
	// Amount is the staking reward.
	Amount uint64 `json:"amount"`
	// Symbol is the symbol of the rewarded tokens.
	Symbol string `json:"symbol"`
	// MinimumReached tells whether the minimum rewards required to be included in the staking results are reached.
	MinimumReached bool `json:"minimumReached"`
}

// AddressRewards holds all the staking rewards for a certain address.
type AddressRewards struct {
	// Rewards is a map of rewards per event.
	Rewards map[string]*AddressReward `json:"rewards"`
	// MilestoneIndex is the milestone index the rewards were calculated for.
	MilestoneIndex iotago.MilestoneIndex `json:"milestoneIndex"`
}

func (pm *Manager) AddressRewards(address iotago.Address, msIndex ...iotago.MilestoneIndex) (*AddressRewards, error) {
	pm.RLock()
	defer pm.RUnlock()

	eventIDs := pm.eventIDsWithoutLocking(StakingPayloadTypeID)

	index, err := pm.readLedgerIndex()
	if err != nil {
		return nil, err
	}
	if len(msIndex) > 0 && msIndex[0] < index {
		index = msIndex[0]
	}

	addrRewards := &AddressRewards{
		Rewards:        make(map[string]*AddressReward),
		MilestoneIndex: index,
	}

	for _, eventID := range eventIDs {
		event := pm.EventWithoutLocking(eventID)
		staking := event.Staking()
		amount, err := pm.StakingRewardForAddressWithoutLocking(eventID, address, index)
		if err != nil {
			return nil, err
		}

		addrRewards.Rewards[eventID.ToHex()] = &AddressReward{
			Amount:         amount,
			Symbol:         staking.Symbol,
			MinimumReached: amount >= staking.RequiredMinimumRewards,
		}
	}

	return addrRewards, nil
}

// EventRewards holds the total rewards per address for a given event.
type EventRewards struct {
	// Symbol is the symbol of the rewarded tokens.
	Symbol string `json:"symbol"`
	// MilestoneIndex is the milestone index the rewards were calculated for.
	MilestoneIndex iotago.MilestoneIndex `json:"milestoneIndex"`
	// TotalRewards is the total reward.
	TotalRewards uint64 `json:"totalRewards"`
	// Checksum is the SHA256 checksum of the staking amount and rewards calculated for this MilestoneIndex.
	Checksum string `json:"checksum"`
	// Rewards is a map of rewards per address.
	Rewards map[string]uint64 `json:"rewards"`
}

func (pm *Manager) EventRewards(eventID EventID, msIndex ...iotago.MilestoneIndex) (*EventRewards, error) {
	protoParas := pm.protocolParametersFunc()

	pm.RLock()
	defer pm.RUnlock()

	event := pm.EventWithoutLocking(eventID)

	if event == nil || event.Staking() == nil {
		return nil, ErrInvalidEvent
	}

	milestoneIndex, err := pm.readLedgerIndex()
	if err != nil {
		return nil, err
	}
	if len(msIndex) > 0 && msIndex[0] < milestoneIndex {
		milestoneIndex = msIndex[0]
	}

	if milestoneIndex > event.EndMilestoneIndex() {
		milestoneIndex = event.EndMilestoneIndex()
	}

	var addresses []string
	rewardsByAddress := make(map[string]uint64)
	if err := pm.ForEachAddressStakingParticipation(eventID, milestoneIndex, func(address iotago.Address, _ *TrackedParticipation, rewards uint64) bool {
		addr := address.Bech32(protoParas.Bech32HRP)
		if _, has := rewardsByAddress[addr]; !has {
			addresses = append(addresses, addr)
		}
		rewardsByAddress[addr] += rewards

		return true
	}); err != nil {
		return nil, err
	}

	responseHash := sha256.New()
	if _, err := responseHash.Write(eventID[:]); err != nil {
		return nil, fmt.Errorf("failed to write eventID to response hash: %w", err)
	}

	if err := binary.Write(responseHash, binary.LittleEndian, milestoneIndex); err != nil {
		return nil, fmt.Errorf("failed to write milestone index to response hash: %w", err)
	}

	if _, err := responseHash.Write([]byte(event.Staking().Symbol)); err != nil {
		return nil, fmt.Errorf("failed to write staking symbol to response hash: %w", err)
	}

	eventRewards := &EventRewards{
		Symbol:         event.Staking().Symbol,
		MilestoneIndex: milestoneIndex,
		TotalRewards:   0,
		Rewards:        make(map[string]uint64),
	}

	sort.Strings(addresses)
	for _, addr := range addresses {
		amount := rewardsByAddress[addr]
		if amount < event.Staking().RequiredMinimumRewards {
			continue
		}

		if _, err := responseHash.Write([]byte(addr)); err != nil {
			return nil, fmt.Errorf("failed to write address to response hash: %w", err)
		}

		if err := binary.Write(responseHash, binary.LittleEndian, amount); err != nil {
			return nil, fmt.Errorf("failed to write amount to response hash: %w", err)
		}

		eventRewards.Rewards[addr] = amount
		eventRewards.TotalRewards += amount
	}

	eventRewards.Checksum = iotago.EncodeHex(responseHash.Sum(nil))

	return eventRewards, nil
}
