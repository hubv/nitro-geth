// Copyright 2021 ningyuxin@peckshield.com
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

package vm

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

type TraceType int8

const (
	Trace_CALL         TraceType = 0
	Trace_CALLCODE     TraceType = 1
	Trace_DELEGATECALL TraceType = 2
	Trace_STATICCALL   TraceType = 3
	Trace_CREATE       TraceType = 4
	Trace_CREATE2      TraceType = 5
	Trace_SELFDESTRUCT TraceType = 6
	Trace_UNKNOWN      TraceType = 15
)

func (tt TraceType) String() string {
	switch tt {
	case Trace_CALL:
		return "call"
	case Trace_CREATE:
		return "create"
	case Trace_CREATE2:
		return "create2"
	case Trace_SELFDESTRUCT:
		return "suicide"
	case Trace_CALLCODE:
		return "callcode"
	case Trace_DELEGATECALL:
		return "delegatecall"
	case Trace_STATICCALL:
		return "staticcall"
	case Trace_UNKNOWN:
		return ""
	default:
		return ""
	}
}

type Node struct {
	Value int
}

func (n *Node) String() string {
	return fmt.Sprint(n.Value)
}

// NewStack returns a new stack.
func NewStack() *StackU16 {
	return &StackU16{}
}

// Stack is a basic LIFO stack that resizes as needed.
type StackU16 struct {
	nodes []*Node
	count int
}

// Push adds a node to the stack.
func (s *StackU16) Push(n *Node) {
	s.nodes = append(s.nodes[:s.count], n)
	s.count++
}

// Pop removes and returns a node from the stack in last to first order.
func (s *StackU16) Pop() *Node {
	if s.count == 0 {
		return nil
	}
	s.count--
	return s.nodes[s.count]
}

// Top returns a node from the top of the stack.
func (s *StackU16) Top() *Node {
	if s.count == 0 {
		return nil
	}
	pos := s.count - 1
	return s.nodes[pos]
}

type TInfo struct {
	traces []TraceInfo
}

type TraceInfo struct {
	subtraces  uint16
	traceaddr  string
	trType     TraceType
	from       string
	to         string
	gas        uint64
	value      string
	callType   TraceType
	input      string
	output     string
	gasUsed    uint64
	err        string
	tracePos   int
	tracedepth int
}

type TracerCall4 struct {
	tInfo     *TInfo
	s         *StackU16
	traceNum  int
	traceType int8
	env       *EVM
}

// NewTxInfo creates a new TxInfo which saves tx hash and txpos
func NewTInfo() *TInfo {
	t := &TInfo{}
	return t
}

// NewTracerCall creates a new EVM tracer that prints execution traces in CSV
// into the provided stream.
func NewTracerCall(txinfo *TInfo) *TracerCall4 {
	t := &TracerCall4{txinfo, NewStack(), 0, 0, nil}
	return t
}

// TracerCSV is used to collect execution traces from an EVM transaction
// execution. CaptureState is called for each step of the VM with the
// current VM state.
// Note that reference types are actual VM data structures; make copies
// if you need to retain them beyond the current call.
type TracerCall interface {
	CaptureEnter(typ OpCode, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int)
	CaptureExit(output []byte, gasUsed uint64, err error)
	CaptureStart(env *EVM, from common.Address, to common.Address, calltype int8, input []byte, gas uint64, value *big.Int)
	CaptureState(pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, rData []byte, depth int, err error)
	CaptureFault(pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, depth int, err error)
	CaptureEnd(output []byte, gasUsed uint64, err error)
	// Transaction level
	CaptureTxStart(gasLimit uint64)
	CaptureTxEnd(restGas uint64)
	CaptureArbitrumTransfer(env *EVM, from, to *common.Address, value *big.Int, before bool, purpose string)
	CaptureArbitrumStorageGet(key common.Hash, depth int, before bool)
	CaptureArbitrumStorageSet(key, value common.Hash, depth int, before bool)
}

func (t *TracerCall4) CaptureTxStart(gasLimit uint64) {
}

func (t *TracerCall4) CaptureTxEnd(restGas uint64) {
}

func (t *TracerCall4) SetTraceType(tracetype int8) {
	t.traceType = tracetype
}

func (t *TracerCall4) CaptureEnter(typ OpCode, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int) {
}

func (t *TracerCall4) CaptureExit(output []byte, gasUsed uint64, err error) {
}

func (t *TracerCall4) CaptureStart(env *EVM, from, to common.Address, create bool, input []byte, gas uint64, value *big.Int) {
	t.env = env
	//fmt.Print("Start>>> Depth: ", env.depth, " TracePos: ", t.traceNum, "\n")
	ti := TraceInfo{
		tracedepth: env.depth,
		subtraces:  0,
		traceaddr:  "",
		trType:     Trace_UNKNOWN,
		from:       strings.ToLower(from.String()),
		to:         strings.ToLower(to.String()),
		gas:        gas,
		value:      value.String(),
		callType:   Trace_UNKNOWN,
		input:      common.Bytes2Hex(input),
		output:     "",
		gasUsed:    0,
		err:        "",
		tracePos:   t.traceNum}

	if t.traceType <= int8(Trace_STATICCALL) {
		ti.trType = Trace_CALL
		ti.callType = TraceType(t.traceType)
	} else {
		ti.trType = TraceType(t.traceType)
	}

	if t.s.Top() == nil {
		ti.traceaddr = "["
	} else {
		// get the traceaddr info of its parent
		pos := t.s.Top().Value
		top := &(t.tInfo.traces[pos])
		if top.tracePos > 0 {
			ti.traceaddr = top.traceaddr + "," + strconv.Itoa(int(top.subtraces))
		} else {
			ti.traceaddr = top.traceaddr + strconv.Itoa(int(top.subtraces))
		}
		top.subtraces += 1
	}
	t.tInfo.traces = append(t.tInfo.traces, ti)
	t.s.Push(&Node{t.traceNum})
	t.traceNum += 1
}

func (t *TracerCall4) CaptureFault(pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, depth int, err error) {
	/*
		if err != nil {
			t.txInfo.traces[t.traceNum-1].err = err.Error()
		}
	*/
}

// CaptureState outputs state information on the logger.
func (t *TracerCall4) CaptureState(pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, rData []byte, depth int, err error) {
}

// CaptureEnd is triggered at end of execution.
func (t *TracerCall4) CaptureEnd(output []byte, gasUsed uint64, err error) {
	pos := t.s.Pop().Value
	ti := &(t.tInfo.traces[pos])
	ti.gasUsed = gasUsed
	ti.output = common.Bytes2Hex(output)
	ti.traceaddr += "]"
	if err != nil {
		ti.err = err.Error()
	}
	if t.s.count == 0 {
		for _, v := range t.tInfo.traces {
			t.env.Config.Traces[v.tracePos] = make(map[string]string)
			t.env.Config.Traces[v.tracePos]["type"] = v.trType.String()
			t.env.Config.Traces[v.tracePos]["action_from"] = strings.ToLower(v.from)
			t.env.Config.Traces[v.tracePos]["action_to"] = strings.ToLower(v.to)
			t.env.Config.Traces[v.tracePos]["action_gas"] = strconv.FormatUint(v.gas, 10)
			t.env.Config.Traces[v.tracePos]["action_value"] = v.value
			t.env.Config.Traces[v.tracePos]["action_calltype"] = v.callType.String()
			t.env.Config.Traces[v.tracePos]["action_input"] = v.input
			t.env.Config.Traces[v.tracePos]["result_output"] = v.output
			t.env.Config.Traces[v.tracePos]["result_gasused"] = strconv.FormatUint(v.gasUsed, 10)
			t.env.Config.Traces[v.tracePos]["error"] = v.err
			t.env.Config.Traces[v.tracePos]["tracepos"] = strconv.Itoa(v.tracePos)
			t.env.Config.Traces[v.tracePos]["tracedepth"] = strconv.Itoa(v.tracedepth)
		}
		t.tInfo.traces = nil
		t.traceNum = 0
	}
}
func (t *TracerCall4) CaptureArbitrumStorageGet(key common.Hash, depth int, before bool)        {}
func (t *TracerCall4) CaptureArbitrumStorageSet(key, value common.Hash, depth int, before bool) {}
func (t *TracerCall4) CaptureArbitrumTransfer(
	env *EVM, from, to *common.Address, value *big.Int, before bool, purpose string,
) {
}
