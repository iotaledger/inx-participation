package participation_test

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gohornet/hornet/pkg/model/milestone"
	"github.com/gohornet/inx-participation/pkg/participation"
	"github.com/iotaledger/hive.go/marshalutil"
	"github.com/iotaledger/hive.go/testutil"
	iotago "github.com/iotaledger/iota.go/v3"
)

func RandBlockID() iotago.BlockID {
	blockID := iotago.BlockID{}
	copy(blockID[:], testutil.RandBytes(iotago.BlockIDLength))
	return blockID
}

func RandOutputID() iotago.OutputID {
	outputID := iotago.OutputID{}
	copy(outputID[:], testutil.RandBytes(iotago.OutputIDLength))
	return outputID
}

func RandomTrackedParticipation() *participation.TrackedParticipation {
	msIndex := milestone.Index(rand.Int31())
	return &participation.TrackedParticipation{
		EventID:    RandEventID(),
		OutputID:   RandOutputID(),
		BlockID:    RandBlockID(),
		Amount:     uint64(rand.Int63()),
		StartIndex: msIndex,
		EndIndex:   msIndex + 10,
	}
}

func TestTrackedParticipation_Serialization(t *testing.T) {
	p := RandomTrackedParticipation()

	ms := marshalutil.New(p.ValueBytes())
	blockID, err := ms.ReadBytes(iotago.BlockIDLength)
	require.NoError(t, err)
	require.True(t, bytes.Equal(p.BlockID[:], blockID))

	amount, err := ms.ReadUint64()
	require.NoError(t, err)
	require.Exactly(t, p.Amount, amount)

	startIndex, err := ms.ReadUint32()
	require.NoError(t, err)
	require.Exactly(t, p.StartIndex, milestone.Index(startIndex))

	endIndex, err := ms.ReadUint32()
	require.NoError(t, err)
	require.Exactly(t, p.EndIndex, milestone.Index(endIndex))

	require.Equal(t, 48, ms.ReadOffset())
}

func TestTrackedParticipation_Deserialization(t *testing.T) {
	eventID := RandEventID()
	outputID := RandOutputID()
	blockID := RandBlockID()
	amount := uint64(rand.Int63())
	startIndex := milestone.Index(rand.Int31())
	endIndex := startIndex + 25

	ms := marshalutil.New(67)
	ms.WriteByte(255)
	ms.WriteBytes(eventID[:])
	ms.WriteBytes(outputID[:])

	key := ms.Bytes()
	require.Equal(t, 67, len(key))

	ms = marshalutil.New(48)
	ms.WriteBytes(blockID[:])
	ms.WriteUint64(amount)
	ms.WriteUint32(uint32(startIndex))
	ms.WriteUint32(uint32(endIndex))

	value := ms.Bytes()
	require.Equal(t, 48, len(value))

	p, err := participation.TrackedParticipationFromBytes(key, value)
	require.NoError(t, err)

	require.Equal(t, eventID[:], p.EventID[:])
	require.Equal(t, outputID[:], p.OutputID[:])
	require.Equal(t, blockID, p.BlockID)
	require.Exactly(t, amount, p.Amount)
	require.Exactly(t, startIndex, p.StartIndex)
	require.Exactly(t, endIndex, p.EndIndex)
}
