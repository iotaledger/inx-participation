package nodebridge

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	grpc_retry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/gohornet/hornet/pkg/model/milestone"
	"github.com/gohornet/inx-participation/pkg/participation"
	"github.com/iotaledger/hive.go/logger"
	"github.com/iotaledger/hive.go/serializer/v2"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v3"
)

type NodeBridge struct {
	logger     *logger.Logger
	conn       *grpc.ClientConn
	client     inx.INXClient
	protoParas *iotago.ProtocolParameters
}

func NewNodeBridge(ctx context.Context, address string, logger *logger.Logger) (*NodeBridge, error) {

	conn, err := grpc.Dial(address,
		grpc.WithChainUnaryInterceptor(grpc_retry.UnaryClientInterceptor(), grpc_prometheus.UnaryClientInterceptor),
		grpc.WithStreamInterceptor(grpc_prometheus.StreamClientInterceptor),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	client := inx.NewINXClient(conn)
	retryBackoff := func(_ uint) time.Duration {
		return 1 * time.Second
	}

	logger.Info("Connecting to node and reading protocol parameters...")
	nodeConfig, err := client.ReadNodeConfiguration(ctx, &inx.NoParams{}, grpc_retry.WithMax(5), grpc_retry.WithBackoff(retryBackoff))
	if err != nil {
		return nil, err
	}

	return &NodeBridge{
		logger:     logger,
		conn:       conn,
		client:     client,
		protoParas: nodeConfig.UnwrapProtocolParameters(),
	}, nil
}

func (n *NodeBridge) Run(ctx context.Context) {
	c, cancel := context.WithCancel(ctx)
	defer cancel()
	<-c.Done()
	n.conn.Close()
}

func (n *NodeBridge) ProtocolParameters() *iotago.ProtocolParameters {
	return n.protoParas
}

func (n *NodeBridge) NodeStatus() (confirmedIndex milestone.Index, pruningIndex milestone.Index) {
	status, err := n.client.ReadNodeStatus(context.Background(), &inx.NoParams{})
	if err != nil {
		return 0, 0
	}
	return milestone.Index(status.GetConfirmedMilestone().GetMilestoneIndex()), milestone.Index(status.GetPruningIndex())
}

func (n *NodeBridge) BlockForBlockID(blockID iotago.BlockID) (*participation.ParticipationBlock, error) {
	inxMsg, err := n.client.ReadBlock(context.Background(), inx.NewBlockId(blockID))
	if err != nil {
		return nil, err
	}

	iotaMsg, err := inxMsg.UnwrapBlock(serializer.DeSeriModeNoValidation, nil)
	if err != nil {
		// if the block was included, there must be a block
		return nil, fmt.Errorf("error deserializing block: %w", err)
	}

	return &participation.ParticipationBlock{
		BlockID: blockID,
		Block:   iotaMsg,
		Data:    inxMsg.GetData(),
	}, nil
}

func (n *NodeBridge) participationOutputFromINXOutput(output *inx.LedgerOutput) *participation.ParticipationOutput {
	iotaOutput, err := output.UnwrapOutput(serializer.DeSeriModeNoValidation, n.protoParas)
	if err != nil {
		return nil
	}

	// Ignore anything other than BasicOutputs
	if iotaOutput.Type() != iotago.OutputBasic {
		return nil
	}

	unlockConditions := iotaOutput.UnlockConditionsSet()
	return &participation.ParticipationOutput{
		BlockID:  output.UnwrapBlockID(),
		OutputID: output.UnwrapOutputID(),
		Address:  unlockConditions.Address().Address,
		Deposit:  iotaOutput.Deposit(),
	}
}

func (n *NodeBridge) OutputForOutputID(outputID iotago.OutputID) (*participation.ParticipationOutput, error) {
	resp, err := n.client.ReadOutput(context.Background(), inx.NewOutputId(outputID))
	if err != nil {
		return nil, err
	}
	switch resp.GetPayload().(type) {
	case *inx.OutputResponse_Output:
		return n.participationOutputFromINXOutput(resp.GetOutput()), nil
	case *inx.OutputResponse_Spent:
		return n.participationOutputFromINXOutput(resp.GetSpent().GetOutput()), nil
	default:
		return nil, fmt.Errorf("invalid inx.OutputResponse payload type")
	}
}

func (n *NodeBridge) LedgerUpdates(ctx context.Context, startIndex milestone.Index, handler func(index milestone.Index, created []*participation.ParticipationOutput, consumed []*participation.ParticipationOutput) bool) error {
	req := &inx.LedgerRequest{
		StartMilestoneIndex: uint32(startIndex),
	}
	stream, err := n.client.ListenToLedgerUpdates(ctx, req)
	if err != nil {
		return err
	}
	for {
		update, err := stream.Recv()
		if err != nil {
			if err == io.EOF || status.Code(err) == codes.Canceled {
				break
			}
			n.logger.Errorf("ListenToLedgerUpdates: %s", err.Error())
			return err
		}

		if ctx.Err() != nil {
			// context got cancelled, so stop the updates
			return nil
		}

		index := milestone.Index(update.GetMilestoneIndex())

		var created []*participation.ParticipationOutput
		for _, output := range update.GetCreated() {
			o := n.participationOutputFromINXOutput(output)
			if o != nil {
				created = append(created, o)
			}
		}

		var consumed []*participation.ParticipationOutput
		for _, spent := range update.GetConsumed() {
			o := n.participationOutputFromINXOutput(spent.GetOutput())
			if o != nil {
				consumed = append(consumed, o)
			}
		}

		if !handler(index, created, consumed) {
			// Stop receiving
			return nil
		}
	}
	return nil
}

func (n *NodeBridge) RegisterAPIRoute(route string, bindAddress string) error {
	bindAddressParts := strings.Split(bindAddress, ":")
	if len(bindAddressParts) != 2 {
		return fmt.Errorf("Invalid address %s", bindAddress)
	}
	port, err := strconv.ParseInt(bindAddressParts[1], 10, 32)
	if err != nil {
		return err
	}

	apiReq := &inx.APIRouteRequest{
		Route: route,
		Host:  bindAddressParts[0],
		Port:  uint32(port),
	}

	if err != nil {
		return err
	}
	_, err = n.client.RegisterAPIRoute(context.Background(), apiReq)
	return err
}

func (n *NodeBridge) UnregisterAPIRoute(route string) error {
	apiReq := &inx.APIRouteRequest{
		Route: route,
	}
	_, err := n.client.UnregisterAPIRoute(context.Background(), apiReq)
	return err
}
