package api

import (
	"context"

	"github.com/ethereum/go-ethereum/polygon/common/hexutil"
	"github.com/ethereum/go-ethereum/polygon/internal/ethapi"
	"github.com/ethereum/go-ethereum/polygon/rpc"
)

//go:generate mockgen -destination=./caller_mock.go -package=api . Caller
type Caller interface {
	Call(ctx context.Context, args ethapi.TransactionArgs, blockNrOrHash rpc.BlockNumberOrHash, overrides *ethapi.StateOverride) (hexutil.Bytes, error)
}
