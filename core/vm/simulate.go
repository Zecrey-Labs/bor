package vm

import (
	"bytes"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
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

type AssetChange struct {
	AssetAddress  string
	Sender        string
	Receiver      string
	AssetAmount   string
	Spender       string
	Allowance     string
	SenderBalance string
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

func (evm *EVM) erc20Balance(contract *Contract, from common.Address) *big.Int {
	// get balance
	var buf bytes.Buffer
	buf.Write(balanceOfSelector)
	buf.Write(new(big.Int).SetBytes(from.Bytes()).FillBytes(make([]byte, 32)))
	var (
		balanceRet []byte
		err        error
	)
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
	if bytes.Equal(transferFromSelector, input[:4]) && len(input) == 100 {
		info := input[4:]
		fromAddr := common.BytesToAddress(info[:32])
		toAddr := common.BytesToAddress(info[32:64])
		amount := new(big.Int).SetBytes(info[64:])
		// get allowance
		allowance := evm.erc20Allowance(contract, fromAddr, caller.Address())
		assetChange.Allowance = allowance.String()
		// force approve
		evm.erc20Approve(caller, fromAddr, addr, amount)
		// fill asset change info
		assetChange.AssetAddress = addr.Hex()
		assetChange.AssetAmount = amount.String()
		assetChange.Sender = fromAddr.Hex()
		assetChange.SenderBalance = evm.erc20Balance(contract, fromAddr).String()
		assetChange.Receiver = toAddr.Hex()
		assetChange.Spender = caller.Address().Hex()
		evm.SimulateResp = append(evm.SimulateResp, assetChange)
	} else if bytes.Equal(transferSelector, input[:4]) && len(input) == 68 {
		info := input[4:]
		toAddr := common.BytesToAddress(info[:32])
		amount := new(big.Int).SetBytes(info[32:])
		// fill asset change info
		assetChange.AssetAddress = addr.Hex()
		assetChange.AssetAmount = amount.String()
		assetChange.Sender = caller.Address().Hex()
		assetChange.SenderBalance = evm.erc20Balance(contract, caller.Address()).String()
		assetChange.Receiver = toAddr.Hex()
		assetChange.Spender = common.Address{}.Hex()
		assetChange.Allowance = "0"
		evm.SimulateResp = append(evm.SimulateResp, assetChange)
	}
	ret, err = evm.interpreter.Run(contract, input, false)
	if err != nil {
		log.Warn("simulate: unable to run contract:", err)
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
	assetChange.SenderBalance = evm.StateDB.GetBalance(from).String()
	assetChange.Receiver = to.Hex()
	assetChange.Spender = common.Address{}.Hex()
	assetChange.Allowance = "0"
	evm.SimulateResp = append(evm.SimulateResp, assetChange)
}
