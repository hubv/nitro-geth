// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"bufio"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

// StateProcessor is a basic Processor, which takes care of transitioning
// state from one point to another.
//
// StateProcessor implements Processor.
type StateProcessor struct {
	config *params.ChainConfig // Chain configuration options
	bc     *BlockChain         // Canonical block chain
	engine consensus.Engine    // Consensus engine used for block rewards
}

// NewStateProcessor initialises a new StateProcessor.
func NewStateProcessor(config *params.ChainConfig, bc *BlockChain, engine consensus.Engine) *StateProcessor {
	return &StateProcessor{
		config: config,
		bc:     bc,
		engine: engine,
	}
}

// Process processes the state changes according to the Ethereum rules by running
// the transaction messages using the statedb and applying any rewards to both
// the processor (coinbase) and any included uncles.
//
// Process returns the receipts and logs accumulated during the process and
// returns the amount of gas that was used in the process. If any of the
// transactions failed to execute due to insufficient gas it will return an error.
func (p *StateProcessor) Process(block *types.Block, statedb *state.StateDB, cfg vm.Config) (types.Receipts, []*types.Log, uint64, error) {
	var (
		receipts    types.Receipts
		usedGas     = new(uint64)
		header      = block.Header()
		blockHash   = block.Hash()
		blockNumber = block.Number()
		allLogs     []*types.Log
		gp          = new(GasPool).AddGas(block.GasLimit())
	)
	// Mutate the block and state according to any hard-fork specs
	if p.config.DAOForkSupport && p.config.DAOForkBlock != nil && p.config.DAOForkBlock.Cmp(block.Number()) == 0 {
		misc.ApplyDAOHardFork(statedb)
	}
	var (
		context = NewEVMBlockContext(header, p.bc, nil)
		vmenv   = vm.NewEVM(context, vm.TxContext{}, statedb, p.config, cfg)
	)
	if beaconRoot := block.BeaconRoot(); beaconRoot != nil {
		ProcessBeaconBlockRoot(*beaconRoot, vmenv, statedb)
	}
	blockContext := NewEVMBlockContext(header, p.bc, nil)
	signer := types.MakeSigner(p.config, header.Number, header.Time)

	txinfo := vm.NewTxInfo()
	var file1 *os.File = nil
	var file2 *os.File = nil
	var file3 *os.File = nil
	t := time.Unix(int64(block.Time()), 0)
	day := t.UTC().Format("2006-01-02")
	blknum := blockNumber.String()
	hash_prefix := blockHash.String()
	txfile := fmt.Sprintf("%s_%s.csv", blknum, hash_prefix[2:8])
	if block.Transactions().Len() != 0 {

		blkinfo := vm.NewBlockInfo(block.Time(), day, blockHash.Hex(), blknum)

		path1 := "/data01/full_node/dump"
		path2 := "/data01/full_node/events"

		//path := "/Users/vincent/Downloads/heco/dump"
		subpath1 := filepath.Join(path1, day, strconv.Itoa(t.UTC().Hour()))
		if _, err1 := os.Stat(subpath1); os.IsNotExist(err1) {
			err1 = os.MkdirAll(subpath1, os.ModePerm)
			if err1 != nil {
				fmt.Printf("failed creating subpath: %s", err1)
			}
		}

		var err12 error
		file1, err12 = os.OpenFile(filepath.Join(subpath1, txfile), os.O_WRONLY|os.O_CREATE, 0644)

		if err12 != nil {
			fmt.Printf("failed creating file: %s", err12)
			file1 = nil
		}
		datawriter1 := bufio.NewWriter(file1)

		if block.Bloom().Big().String() != "0" {
			subpath2 := filepath.Join(path2, day, strconv.Itoa(t.UTC().Hour()))
			if _, err2 := os.Stat(subpath2); os.IsNotExist(err2) {
				err2 = os.MkdirAll(subpath2, os.ModePerm)
				if err2 != nil {
					fmt.Printf("failed creating subpath: %s", err2)
				}
			}

			var err22 error
			file2, err22 = os.OpenFile(filepath.Join(subpath2, txfile), os.O_WRONLY|os.O_CREATE, 0644)

			if err22 != nil {
				fmt.Printf("failed creating file: %s", err22)
				file2 = nil
			}

		}

		datawriter2 := bufio.NewWriter(file2)

		cfg.Tracer = vm.NewCSVTracer(blkinfo, txinfo, datawriter1, datawriter2)
	}

	vmenv = vm.NewEVM(blockContext, vm.TxContext{}, statedb, p.config, cfg)
	// Iterate over and process the individual transactions
	for i, tx := range block.Transactions() {
		txhash := tx.Hash().Hex()
		msg, err := TransactionToMessage(tx, signer, header.BaseFee, MessageCommitMode)
		if msg.To != nil {
			txinfo.UpdateTxInfo(i, txhash, msg.From.Hex(), msg.To.Hex())
		} else {
			txinfo.UpdateTxInfo(i, txhash, msg.From.Hex(), "")
		}
		if err != nil {
			return nil, nil, 0, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx.Hash().Hex(), err)
		}
		statedb.SetTxContext(tx.Hash(), i)
		receipt, _, reason, err := applyTransaction2(msg, p.config, gp, statedb, blockNumber, blockHash, tx, usedGas, vmenv, nil)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx.Hash().Hex(), err)
		}
		if reason != "" {
			path3 := "/data01/full_node/errors"
			subpath3 := filepath.Join(path3, day, strconv.Itoa(t.UTC().Hour()))
			if _, err3 := os.Stat(subpath3); os.IsNotExist(err3) {
				err3 = os.MkdirAll(subpath3, os.ModePerm)
				if err3 != nil {
					fmt.Printf("failed creating subpath: %s", err3)
				}
			}

			var err32 error
			file3, err32 = os.OpenFile(filepath.Join(subpath3, txfile), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)

			if err32 != nil {
				fmt.Printf("failed creating file: %s", err32)
				file3 = nil
			}
			datawriter3 := bufio.NewWriter(file3)
			errinfo := []string{
				strings.ToLower(txhash),
				reason}
			_, _ = datawriter3.WriteString(strings.Join(errinfo, "^") + "\n")
			datawriter3.Flush()
		}
		receipts = append(receipts, receipt)
		allLogs = append(allLogs, receipt.Logs...)
	}
	if block.Transactions().Len() != 0 && file1 != nil {
		//close file, so that finalize can reopen it for systemTxs traces
		file1.Close()

		if block.Bloom().Big().String() != "0" && file2 != nil {
			file2.Close()
		}

	}

	// Fail if Shanghai not enabled and len(withdrawals) is non-zero.
	withdrawals := block.Withdrawals()
	if len(withdrawals) > 0 && !p.config.IsShanghai(block.Number(), block.Time(), types.DeserializeHeaderExtraInformation(header).ArbOSFormatVersion) {
		return nil, nil, 0, errors.New("withdrawals before shanghai")
	}

	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	p.engine.Finalize(p.bc, header, statedb, block.Transactions(), block.Uncles(), withdrawals)

	return receipts, allLogs, *usedGas, nil
}

func applyTransaction(msg *Message, config *params.ChainConfig, gp *GasPool, statedb *state.StateDB, blockNumber *big.Int, blockHash common.Hash, tx *types.Transaction, usedGas *uint64, evm *vm.EVM, resultFilter func(*ExecutionResult) error) (*types.Receipt, *ExecutionResult, error) {
	// Create a new context to be used in the EVM environment.
	txContext := NewEVMTxContext(msg)
	evm.Reset(txContext, statedb)

	// Apply the transaction to the current state (included in the env).
	result, err := ApplyMessage(evm, msg, gp)
	if err != nil {
		return nil, nil, err
	}

	if resultFilter != nil {
		err = resultFilter(result)
		if err != nil {
			return nil, nil, err
		}
	}

	// Update the state with pending changes.
	var root []byte
	if config.IsByzantium(blockNumber) {
		statedb.Finalise(true)
	} else {
		root = statedb.IntermediateRoot(config.IsEIP158(blockNumber)).Bytes()
	}
	*usedGas += result.UsedGas

	// Create a new receipt for the transaction, storing the intermediate root and gas used
	// by the tx.
	receipt := &types.Receipt{Type: tx.Type(), PostState: root, CumulativeGasUsed: *usedGas}
	if result.Failed() {
		receipt.Status = types.ReceiptStatusFailed
	} else {
		receipt.Status = types.ReceiptStatusSuccessful
	}
	receipt.TxHash = tx.Hash()
	receipt.GasUsed = result.UsedGas

	if tx.Type() == types.BlobTxType {
		receipt.BlobGasUsed = uint64(len(tx.BlobHashes()) * params.BlobTxBlobGasPerBlob)
		receipt.BlobGasPrice = evm.Context.BlobBaseFee
	}

	// If the transaction created a contract, store the creation address in the receipt.
	if result.TopLevelDeployed != nil {
		receipt.ContractAddress = *result.TopLevelDeployed
	}

	// Set the receipt logs and create the bloom filter.
	receipt.Logs = statedb.GetLogs(tx.Hash(), blockNumber.Uint64(), blockHash)
	receipt.Bloom = types.CreateBloom(types.Receipts{receipt})
	receipt.BlockHash = blockHash
	receipt.BlockNumber = blockNumber
	receipt.TransactionIndex = uint(statedb.TxIndex())
	evm.ProcessingHook.FillReceiptInfo(receipt)
	return receipt, result, err
}

func applyTransaction2(msg *Message, config *params.ChainConfig, gp *GasPool, statedb *state.StateDB, blockNumber *big.Int, blockHash common.Hash, tx *types.Transaction, usedGas *uint64, evm *vm.EVM, resultFilter func(*ExecutionResult) error) (*types.Receipt, *ExecutionResult, string, error) {
	// Create a new context to be used in the EVM environment.
	txContext := NewEVMTxContext(msg)
	evm.Reset(txContext, statedb)
	revertreason := ""
	// Apply the transaction to the current state (included in the env).
	result, err := ApplyMessage(evm, msg, gp)
	if err != nil {
		return nil, nil, revertreason, err
	}

	if resultFilter != nil {
		err = resultFilter(result)
		if err != nil {
			return nil, nil, revertreason, err
		}
	}

	if len(result.Revert()) > 0 {
		reason, errUnpack := abi.UnpackRevert(result.Revert())
		if errUnpack == nil {
			revertreason = reason
		}
	}

	// Update the state with pending changes.
	var root []byte
	if config.IsByzantium(blockNumber) {
		statedb.Finalise(true)
	} else {
		root = statedb.IntermediateRoot(config.IsEIP158(blockNumber)).Bytes()
	}
	*usedGas += result.UsedGas

	// Create a new receipt for the transaction, storing the intermediate root and gas used
	// by the tx.
	receipt := &types.Receipt{Type: tx.Type(), PostState: root, CumulativeGasUsed: *usedGas}
	if result.Failed() {
		receipt.Status = types.ReceiptStatusFailed
	} else {
		receipt.Status = types.ReceiptStatusSuccessful
	}
	receipt.TxHash = tx.Hash()
	receipt.GasUsed = result.UsedGas

	// If the transaction created a contract, store the creation address in the receipt.
	if result.TopLevelDeployed != nil {
		receipt.ContractAddress = *result.TopLevelDeployed
	}

	// Set the receipt logs and create the bloom filter.
	receipt.Logs = statedb.GetLogs(tx.Hash(), blockNumber.Uint64(), blockHash)
	receipt.Bloom = types.CreateBloom(types.Receipts{receipt})
	receipt.BlockHash = blockHash
	receipt.BlockNumber = blockNumber
	receipt.TransactionIndex = uint(statedb.TxIndex())
	evm.ProcessingHook.FillReceiptInfo(receipt)
	return receipt, result, revertreason, err
}

// ApplyTransaction attempts to apply a transaction to the given state database
// and uses the input parameters for its environment. It returns the receipt
// for the transaction, gas used and an error if the transaction failed,
// indicating the block was invalid.
func ApplyTransaction(config *params.ChainConfig, bc ChainContext, author *common.Address, gp *GasPool, statedb *state.StateDB, header *types.Header, tx *types.Transaction, usedGas *uint64, cfg vm.Config) (*types.Receipt, *ExecutionResult, error) {
	return ApplyTransactionWithResultFilter(config, bc, author, gp, statedb, header, tx, usedGas, cfg, nil)
}

func ApplyTransactionWithResultFilter(config *params.ChainConfig, bc ChainContext, author *common.Address, gp *GasPool, statedb *state.StateDB, header *types.Header, tx *types.Transaction, usedGas *uint64, cfg vm.Config, resultFilter func(*ExecutionResult) error) (*types.Receipt, *ExecutionResult, error) {
	msg, err := TransactionToMessage(tx, types.MakeSigner(config, header.Number, header.Time), header.BaseFee, MessageReplayMode)
	if err != nil {
		return nil, nil, err
	}
	// Create a new context to be used in the EVM environment
	blockContext := NewEVMBlockContext(header, bc, author)
	txContext := NewEVMTxContext(msg)
	vmenv := vm.NewEVM(blockContext, txContext, statedb, config, cfg)
	return applyTransaction(msg, config, gp, statedb, header.Number, header.Hash(), tx, usedGas, vmenv, resultFilter)
}

// ProcessBeaconBlockRoot applies the EIP-4788 system call to the beacon block root
// contract. This method is exported to be used in tests.
func ProcessBeaconBlockRoot(beaconRoot common.Hash, vmenv *vm.EVM, statedb *state.StateDB) {
	// If EIP-4788 is enabled, we need to invoke the beaconroot storage contract with
	// the new root
	msg := &Message{
		From:      params.SystemAddress,
		GasLimit:  30_000_000,
		GasPrice:  common.Big0,
		GasFeeCap: common.Big0,
		GasTipCap: common.Big0,
		To:        &params.BeaconRootsStorageAddress,
		Data:      beaconRoot[:],
	}
	vmenv.Reset(NewEVMTxContext(msg), statedb)
	statedb.AddAddressToAccessList(params.BeaconRootsStorageAddress)
	_, _, _ = vmenv.Call(vm.AccountRef(msg.From), *msg.To, msg.Data, 30_000_000, common.U2560)
	statedb.Finalise(true)
}
