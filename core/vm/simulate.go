package vm

import (
	"bytes"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	state2 "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

func GetMethodSelector(nameAndParams string) []byte {
	return crypto.Keccak256Hash([]byte(nameAndParams)).Bytes()[:4]
}

var (
	transferFromSelector = GetMethodSelector("transferFrom(address,address,uint256)")
	approveSelector      = GetMethodSelector("approve(address,uint256)")
	allowanceSelector    = GetMethodSelector("allowance(address,address)")
	balanceOfSelector    = GetMethodSelector("balanceOf(address)")
	transferSelector     = GetMethodSelector("transfer(address,uint256)")
)

type SimulateResponse struct {
	AssetChanges         []AssetChange
	GasCost              uint64
	SuccessWithPrePay    bool
	SuccessWithoutPrePay bool
	ErrInfo              string
}
type AssetChange struct {
	AssetAddress  string
	Sender        string
	Receiver      string
	AssetAmount   string
	Spender       string
	Allowance     string
	SenderBalance string
	ActionType    string
}

func (evm *EVM) simulateCall(caller ContractRef, addr common.Address, input []byte, gas uint64, value *big.Int) (ret []byte, leftOverGas uint64, err error) {
	initGas := gas
	// Fail if we're trying to execute above the call depth limit
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, ErrDepth
	}
	// Note although it's noop to transfer X ether to caller itself. But
	// if caller doesn't have enough balance, it would be an error to allow
	// over-charging itself. So the check here is necessary.
	// Fail if we're trying to transfer more than the available balance
	if value.Sign() != 0 {
		evm.simulateNativeAsset(caller.Address(), addr, value)
		if !evm.Context.CanTransfer(evm.StateDB, caller.Address(), value) {
			{
				return nil, gas, ErrInsufficientBalance
			}
		}
	}
	var snapshot = evm.StateDB.Snapshot()

	// Invoke tracer hooks that signal entering/exiting a call frame
	if evm.Config.Tracer != nil {
		evm.Config.Tracer.CaptureEnter(CALLCODE, caller.Address(), addr, input, gas, value)
		defer func(startGas uint64) {
			evm.Config.Tracer.CaptureExit(ret, startGas-gas, err)
		}(gas)
	}

	// It is allowed to call precompiles, even via delegatecall
	if p, isPrecompile := evm.precompile(addr); isPrecompile {
		ret, gas, err = RunPrecompiledContract(p, input, gas)
	} else {
		addrCopy := addr
		// Initialise a new contract and set the code that is to be used by the EVM.
		// The contract is a scoped environment for this execution context only.
		contract := NewContract(caller, AccountRef(caller.Address()), value, gas)
		contract.SetCallCode(&addrCopy, evm.StateDB.GetCodeHash(addrCopy), evm.StateDB.GetCode(addrCopy))
		ret, err = evm.simulateAction(contract, caller, addr, input)
		gas = contract.Gas
	}
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != ErrExecutionReverted {
			evm.SimulateResp.GasCost += initGas - gas
			gas = 0
		}
		evm.SimulateResp.SuccessWithPrePay = false
	}
	evm.SimulateResp.SuccessWithPrePay = true
	if gas != 0 {
		evm.SimulateResp.GasCost += initGas - gas
	}
	return ret, gas, err
}
func (evm *EVM) erc20Allowance(contract *Contract, from, to common.Address) *big.Int {
	// get allowance
	var buf bytes.Buffer
	buf.Write(allowanceSelector)
	buf.Write(new(big.Int).SetBytes(from.Bytes()).FillBytes(make([]byte, 32)))
	buf.Write(new(big.Int).SetBytes(to.Bytes()).FillBytes(make([]byte, 32)))
	var (
		allowanceRet []byte
		err          error
	)
	allowanceRet, err = evm.interpreter.Run(contract, buf.Bytes(), false)
	if err != nil {
		log.Warn("simulate: cannot get allowance:", err)
	}
	return new(big.Int).SetBytes(allowanceRet)
}
func (evm *EVM) erc20Balance(contract *Contract, from common.Address, expectAmount *big.Int) *big.Int {
	// get balance
	var buf bytes.Buffer
	buf.Write(balanceOfSelector)
	buf.Write(new(big.Int).SetBytes(from.Bytes()).FillBytes(make([]byte, 32)))
	var (
		balanceRet []byte
		err        error
	)
	// force to increase user's balance
	stateDB := evm.StateDB.(*state2.StateDB)
	stateDB.IsERC20BalanceOf = true
	var value [32]byte
	copy(value[:], expectAmount.FillBytes(make([]byte, 32))[:])
	stateDB.ERC20BalanceOfValue = value
	balanceRet, err = evm.interpreter.Run(contract, buf.Bytes(), true)
	if err != nil {
		log.Warn("simulate: cannot get balance for sender:", err)
	}
	return new(big.Int).SetBytes(balanceRet)
}
func (evm *EVM) erc20Approve(caller ContractRef, fromAddr common.Address, addr common.Address, amount *big.Int) {
	// force approve
	var buf bytes.Buffer
	buf.Write(approveSelector)
	buf.Write(new(big.Int).SetBytes(caller.Address().Bytes()).FillBytes(make([]byte, 32)))
	buf.Write(amount.FillBytes(make([]byte, 32)))
	var err error
	_, _, err = evm.Call(AccountRef(fromAddr), addr, buf.Bytes(), 80000, big.NewInt(0))
	if err != nil {
		log.Warn("simulate: cannot approve for sender:", err)
	}
}
func (evm *EVM) simulateAction(contract *Contract, caller ContractRef, addr common.Address, input []byte) (ret []byte, err error) {
	// catch transferFrom call
	// if that's transferFrom call, decode inputs
	var assetChange AssetChange
	ignoreErr := false
	if bytes.Equal(transferFromSelector, input[:4]) && len(input) == 100 {
		ignoreErr = true
		info := input[4:]
		fromAddr := common.BytesToAddress(info[:32])
		toAddr := common.BytesToAddress(info[32:64])
		amount := new(big.Int).SetBytes(info[64:])
		// get allowance
		allowance := evm.erc20Allowance(contract, fromAddr, caller.Address())
		assetChange.Allowance = allowance.String()
		// force approve
		if allowance.Cmp(amount) < 0 {
			evm.erc20Approve(caller, fromAddr, addr, amount)
		}
		// fill asset change info
		assetChange.AssetAddress = addr.Hex()
		assetChange.AssetAmount = amount.String()
		assetChange.Sender = fromAddr.Hex()
		balance := evm.erc20Balance(contract, fromAddr, amount)
		assetChange.SenderBalance = balance.String()
		if balance.Cmp(amount) < 0 {
			evm.SimulateResp.SuccessWithoutPrePay = false
		}
		assetChange.Receiver = toAddr.Hex()
		assetChange.Spender = caller.Address().Hex()
		assetChange.ActionType = "transferFrom"
		evm.SimulateResp.AssetChanges = append(evm.SimulateResp.AssetChanges, assetChange)
	} else if bytes.Equal(transferSelector, input[:4]) && len(input) == 68 {
		ignoreErr = true
		info := input[4:]
		toAddr := common.BytesToAddress(info[:32])
		amount := new(big.Int).SetBytes(info[32:])
		// fill asset change info
		assetChange.AssetAddress = addr.Hex()
		assetChange.AssetAmount = amount.String()
		assetChange.Sender = caller.Address().Hex()
		balance := evm.erc20Balance(contract, caller.Address(), amount)
		assetChange.SenderBalance = balance.String()
		assetChange.Receiver = toAddr.Hex()
		assetChange.Spender = common.Address{}.Hex()
		assetChange.Allowance = "0"
		assetChange.ActionType = "transfer"
		if balance.Cmp(amount) < 0 {
			evm.SimulateResp.SuccessWithoutPrePay = false
		}
		evm.SimulateResp.AssetChanges = append(evm.SimulateResp.AssetChanges, assetChange)
	}
	ret, err = evm.interpreter.Run(contract, input, false)
	if err != nil {
		evm.SimulateResp.ErrInfo = err.Error()
		log.Warn("simulate: unable to run contract:", err)
	}
	if ignoreErr {
		return ret, nil
	}
	return ret, nil
}
func (evm *EVM) simulateNativeAsset(from, to common.Address, value *big.Int) {
	if value.Cmp(big.NewInt(0)) == 0 {
		return
	}
	// catch transferFrom call
	// if that's transferFrom call, decode inputs
	var assetChange AssetChange
	// fill asset change info
	assetChange.AssetAddress = common.Address{}.Hex()
	assetChange.AssetAmount = value.String()
	assetChange.Sender = from.Hex()
	balance := evm.StateDB.GetBalance(from)
	assetChange.SenderBalance = balance.String()
	assetChange.Receiver = to.Hex()
	assetChange.Spender = common.Address{}.Hex()
	assetChange.Allowance = "0"
	assetChange.ActionType = "native"
	if balance.Cmp(value) < 0 {
		evm.SimulateResp.SuccessWithoutPrePay = false
		// force to add balance
		evm.StateDB.AddBalance(from, value)
	}
	evm.SimulateResp.AssetChanges = append(evm.SimulateResp.AssetChanges, assetChange)
}
