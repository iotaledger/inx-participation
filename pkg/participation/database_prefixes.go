package participation

const (
	// Holds the database status.
	ParticipationStoreKeyPrefixStatus byte = 0

	// Holds the events.
	ParticipationStoreKeyPrefixEvents byte = 1

	// Holds the blocks containing participations.
	ParticipationStoreKeyPrefixBlocks byte = 2

	// Tracks all active and past participations.
	ParticipationStoreKeyPrefixTrackedOutputs         byte = 3
	ParticipationStoreKeyPrefixTrackedSpentOutputs    byte = 4
	ParticipationStoreKeyPrefixTrackedOutputByAddress byte = 5

	// Voting.
	ParticipationStoreKeyPrefixBallotCurrentVoteBalanceForQuestionAndAnswer     byte = 6
	ParticipationStoreKeyPrefixBallotAccululatedVoteBalanceForQuestionAndAnswer byte = 7

	// Staking.
	ParticipationStoreKeyPrefixStakingAddress            byte = 8
	ParticipationStoreKeyPrefixStakingTotalParticipation byte = 9
	ParticipationStoreKeyPrefixStakingCurrentRewards     byte = 10
)
