package ethapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
	"math"
	"time"
)

func (s *PublicBlockChainAPI) SimulateCall(ctx context.Context, args TransactionArgs, blockNrOrHash rpc.BlockNumberOrHash, overrides *StateOverride) ([]vm.AssetChange, error) {
	result, err := DoSimulateCall(ctx, s.b, args, blockNrOrHash, overrides, s.b.RPCEVMTimeout(), s.b.RPCGasCap())
	if err != nil {
		return nil, err
	}
	// If the result contains a revert reason, try to unpack and return it.
	if len(result.Revert()) > 0 {
		return nil, newRevertError(result)
	}
	resp, err := unmarshalSimulateResp(result.ReturnData)
	if err != nil {
		return nil, err
	}
	return resp, result.Err
}

func (s *PublicBlockChainAPI) BlockReceipts(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (types.Receipts, error) {
	block, err := s.b.BlockByNumberOrHash(ctx, blockNrOrHash)
	if err != nil {
		return nil, err
	}
	hash := block.Hash()
	if hash == (common.Hash{}) {
		hash = block.Header().Hash()
	}
	receipts, err := s.b.GetReceipts(ctx, hash)
	if err != nil {
		return nil, err
	}
	return receipts, nil
}

func (s *PublicBlockChainAPI) Headers(ctx context.Context, blockNums []rpc.BlockNumber) ([]map[string]interface{}, error) {
	if len(blockNums) > 1000 {
		return nil, errors.New("request for max 1000 blocks, exceed the max")
	}
	var blockInfos []map[string]interface{}
	for _, blockNum := range blockNums {
		blockInfo, err := s.GetBlockByNumber(ctx, blockNum, false)
		if err != nil {
			return nil, err
		}
		blockInfos = append(blockInfos, blockInfo)
	}
	return blockInfos, nil
}

func DoSimulateCall(ctx context.Context, b Backend, args TransactionArgs, blockNrOrHash rpc.BlockNumberOrHash, overrides *StateOverride, timeout time.Duration, globalGasCap uint64) (*core.ExecutionResult, error) {
	defer func(start time.Time) { log.Debug("Executing EVM call finished", "runtime", time.Since(start)) }(time.Now())

	state, header, err := b.StateAndHeaderByNumberOrHash(ctx, blockNrOrHash)
	if state == nil || err != nil {
		return nil, err
	}
	if err := overrides.Apply(state); err != nil {
		return nil, err
	}
	// Setup context so it may be cancelled the call has completed
	// or, in case of unmetered gas, setup a context with a timeout.
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	// Make sure the context is cancelled when the call has completed
	// this makes sure resources are cleaned up.
	defer cancel()

	// Get a new instance of the EVM.
	msg, err := args.ToMessage(globalGasCap, header.BaseFee)
	if err != nil {
		return nil, err
	}
	evm, vmError, err := b.GetEVM(ctx, msg, state, header, &vm.Config{NoBaseFee: true})
	if err != nil {
		return nil, err
	}
	// Wait for the context to be done and cancel the evm. Even if the
	// EVM has finished, cancelling may be done (repeatedly)
	go func() {
		<-ctx.Done()
		evm.Cancel()
	}()

	evm.IsSimulated = true

	// Execute the message.
	gp := new(core.GasPool).AddGas(math.MaxUint64)
	result, err := core.ApplyMessage(evm, msg, gp, context.Background())
	if err := vmError(); err != nil {
		return nil, err
	}

	// If the timer caused an abort, return an appropriate error message
	if evm.Cancelled() {
		return nil, fmt.Errorf("execution aborted (timeout = %v)", timeout)
	}
	if err != nil {
		return result, fmt.Errorf("err: %w (supplied gas %d)", err, msg.Gas())
	}
	simulateResp, err := json.Marshal(evm.SimulateResp)
	if err != nil {
		return result, err
	}
	result.ReturnData = simulateResp
	return result, nil
}

func unmarshalSimulateResp(resp []byte) ([]vm.AssetChange, error) {
	if len(resp) == 0 {
		return nil, nil
	}
	var res []vm.AssetChange
	err := json.Unmarshal(resp, &res)
	if err != nil {
		return nil, err
	}
	return res, nil
}
