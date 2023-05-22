package bor

import (
	"context"

	"github.com/ethereum/go-ethereum/polygon/common"
	"github.com/ethereum/go-ethereum/polygon/consensus/bor/heimdall/span"
	"github.com/ethereum/go-ethereum/polygon/consensus/bor/valset"
	"github.com/ethereum/go-ethereum/polygon/core"
	"github.com/ethereum/go-ethereum/polygon/core/state"
	"github.com/ethereum/go-ethereum/polygon/core/types"
	"github.com/ethereum/go-ethereum/polygon/rpc"
)

//go:generate mockgen -destination=./span_mock.go -package=bor . Spanner
type Spanner interface {
	GetCurrentSpan(ctx context.Context, headerHash common.Hash) (*span.Span, error)
	GetCurrentValidatorsByHash(ctx context.Context, headerHash common.Hash, blockNumber uint64) ([]*valset.Validator, error)
	GetCurrentValidatorsByBlockNrOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash, blockNumber uint64) ([]*valset.Validator, error)
	CommitSpan(ctx context.Context, heimdallSpan span.HeimdallSpan, state *state.StateDB, header *types.Header, chainContext core.ChainContext) error
}
