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
	"bufio"
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

type BlockInfo struct {
	_st     uint64
	_st_day string
	blkhash string
	blknum  string
}

type TxInfo struct {
	txhash string
	txpos  int
	from   string
	to     string
	traces []TraceInfo
	logs   []LogInfo
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
	traceDepth int
}

type LogInfo struct {
	from       string
	to         string
	topiccount int
	address    string
	logindex   int
	data       string
	topic0     string
	topics     string
	tracepos   int
	tracedepth int
}

type CSVTracer struct {
	blkInfo      *BlockInfo
	txInfo       *TxInfo
	s            *StackU16
	logs_removed map[int]bool
	logs_list    map[int]map[int]int
	traceNum     int
	traceType    int8
	w1           *bufio.Writer
	w2           *bufio.Writer
}

// NewBlockInfo creates a new BlockInfo which saves block information
func NewBlockInfo(st uint64, day string, hash string, num string) *BlockInfo {
	b := &BlockInfo{st, day, hash, num}
	//fmt.Println("st ", st, ", _st_day ", day, ", blknum ", num, ", BlkHash ", hash)
	return b
}

// NewTxInfo creates a new TxInfo which saves tx hash and txpos
func NewTxInfo() *TxInfo {
	t := &TxInfo{}
	return t
}

// NewCSVTracer creates a new EVM tracer that prints execution traces in CSV
// into the provided stream.
// func NewCSVTracer(blkinfo *BlockInfo, txinfo *TxInfo, p *kafka.Producer, w *bufio.Writer) *CSVTracer {
func NewCSVTracer(blkinfo *BlockInfo, txinfo *TxInfo, w1 *bufio.Writer, w2 *bufio.Writer) *CSVTracer {
	t := &CSVTracer{blkinfo, txinfo, NewStack(), make(map[int]bool), make(map[int]map[int]int), 0, 0, w1, w2}
	return t
}

func (t *TxInfo) UpdateTxInfo(pos int, hash string, from string, to string) {
	t.from = from
	t.to = to
	t.txhash = hash
	t.txpos = pos
}

// TracerCSV is used to collect execution traces from an EVM transaction
// execution. CaptureState is called for each step of the VM with the
// current VM state.
// Note that reference types are actual VM data structures; make copies
// if you need to retain them beyond the current call.
type TracerCSV interface {
	// Arbitrum: capture a transfer, mint, or burn that happens outside of EVM exectuion
	CaptureArbitrumTransfer(env *EVM, from, to *common.Address, value *big.Int, before bool, purpose string)
	CaptureArbitrumStorageGet(key common.Hash, depth int, before bool)
	CaptureArbitrumStorageSet(key, value common.Hash, depth int, before bool)
	CaptureTxStart(gasLimit uint64)
	CaptureTxEnd(restGas uint64)
	CaptureStart(env *EVM, from common.Address, to common.Address, calltype int8, input []byte, gas uint64, value *big.Int)
	CaptureState(env *EVM, pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, rData []byte, depth int, err error)
	CaptureFault(env *EVM, pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, depth int, err error)
	CaptureEnd(output []byte, gasUsed uint64, err error)
	CaptureStylusHostio(name string, args, outs []byte, startInk, endInk uint64)
}

func (t *CSVTracer) SetTraceType(tracetype int8) {
	t.traceType = tracetype
}

func (t *CSVTracer) CaptureStart(env *EVM, from, to common.Address, create bool, input []byte, gas uint64, value *big.Int) {
	//fmt.Print("Start>>> Depth: ", env.depth, " TracePos: ", t.traceNum, "\n")
	ti := TraceInfo{
		subtraces:  0,
		traceaddr:  "",
		trType:     Trace_UNKNOWN,
		from:       from.String(),
		to:         to.String(),
		gas:        gas,
		value:      value.String(),
		callType:   Trace_UNKNOWN,
		input:      common.Bytes2Hex(input),
		output:     "",
		gasUsed:    0,
		err:        "",
		tracePos:   t.traceNum,
		traceDepth: env.depth,
	}

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
		top := &(t.txInfo.traces[pos])
		if top.tracePos > 0 {
			ti.traceaddr = top.traceaddr + "," + strconv.Itoa(int(top.subtraces))
		} else {
			ti.traceaddr = top.traceaddr + strconv.Itoa(int(top.subtraces))
		}
		top.subtraces += 1
	}
	t.txInfo.traces = append(t.txInfo.traces, ti)
	t.s.Push(&Node{t.traceNum})
	t.traceNum += 1
}

func (t *CSVTracer) CaptureFault(pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, depth int, err error) {
	/*
		if err != nil {
			t.txInfo.traces[t.traceNum-1].err = err.Error()
		}
	*/
}

// CaptureState outputs state information on the logger.
func (t *CSVTracer) CaptureState(pc uint64, op OpCode, gas, cost uint64, scope *ScopeContext, rData []byte, depth int, err error) {
	if err == nil && strings.HasPrefix(op.String(), "LOG") {
		logindex := len(t.txInfo.logs)
		memory := scope.Memory
		stack := scope.Stack
		topicCount, _ := strconv.Atoi(strings.Split(op.String(), "LOG")[1])
		address := scope.Contract.Address().String()
		data := []byte{uint8(0)}
		if stack.len() >= 2 {
			data = memory.GetCopy(stack.peek().ToBig().Int64(), stack.Back(1).ToBig().Int64())
		}
		topics := make([]string, int(topicCount), int(topicCount+1))
		if topicCount != 0 && stack.len() >= topicCount+2 {
			for i := 0; i < topicCount; i++ {
				addr := stack.Back(i + 2)
				topics[i] = "0x" + common.Bytes2Hex(addr.PaddedBytes(32))
			}
		} else {
			topics = topics[:cap(topics)]
		}
		li := LogInfo{
			from:       t.txInfo.from,
			to:         t.txInfo.to,
			topiccount: topicCount,
			address:    address,
			logindex:   logindex,
			data:       "0x" + strings.ToLower(common.Bytes2Hex(data)),
			topic0:     topics[0],
			topics:     "[" + strings.Join(topics, ",") + "]",
			tracepos:   t.txInfo.traces[len(t.txInfo.traces)-1].tracePos,
			tracedepth: depth}
		t.txInfo.logs = append(t.txInfo.logs, li)
		if _, ok := t.logs_list[li.tracepos]; ok {
			t.logs_list[li.tracepos][logindex] = li.tracedepth
		} else {
			logs := make(map[int]int)
			logs[logindex] = li.tracedepth
			t.logs_list[li.tracepos] = logs
		}
	}
}

// CaptureEnd is triggered at end of execution.
func (t *CSVTracer) CaptureEnd(output []byte, gasUsed uint64, err error) {
	pos := t.s.Pop().Value
	ti := &(t.txInfo.traces[pos])
	ti.gasUsed = gasUsed
	ti.output = common.Bytes2Hex(output)
	ti.traceaddr += "]"
	if err != nil {
		ti.err = err.Error()
	}

	if t.s.count == 0 {
		t.dumpCSV()
		t.txInfo.traces = nil
		t.txInfo.logs = nil
		t.traceNum = 0
		t.logs_removed = map[int]bool{}
		t.logs_list = map[int]map[int]int{}
	}
}

func (t *CSVTracer) CaptureEnter(typ OpCode, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int) {
}

func (t *CSVTracer) CaptureExit(output []byte, gasUsed uint64, err error) {}

func (t *CSVTracer) CaptureTxStart(gasLimit uint64) {}

func (t *CSVTracer) CaptureTxEnd(restGas uint64) {}

func (t *CSVTracer) dumpCSV() {
	//fmt.Println(time.Now().UnixNano())
	blkinfo := []string{
		strconv.FormatUint(t.blkInfo._st, 10),
		t.blkInfo._st_day,
		t.blkInfo.blkhash,
		t.blkInfo.blknum}
	txinfo := []string{t.txInfo.txhash, strconv.Itoa(t.txInfo.txpos)}
	errdep := -1
	for _, v := range t.txInfo.traces {
		if v.err != "" {
			if errdep == -1 || v.traceDepth < errdep {
				errdep = v.traceDepth
			}
		} else {
			if v.traceDepth <= errdep {
				errdep = -1
			}
		}
		if errdep != -1 {
			for index, dep := range t.logs_list[v.tracePos] {
				if dep > errdep {
					t.logs_removed[index] = true
				}
			}
		}
		traceinfo := []string{
			v.trType.String(),
			strings.ToLower(v.from),
			strings.ToLower(v.to),
			strconv.FormatUint(v.gas, 10),
			v.value,
			v.callType.String(),
			v.input,
			v.output,
			strconv.FormatUint(v.gasUsed, 10),
			v.err,
			strconv.Itoa(v.tracePos)}

		traceinfo = append(txinfo, traceinfo...)
		traceinfo = append([]string{strconv.Itoa(int(v.subtraces)), v.traceaddr}, traceinfo...)
		traceinfo = append(blkinfo, traceinfo...)
		_, _ = t.w1.WriteString(strings.Join(traceinfo, "^") + "\n")
		//fmt.Println(time.Now().UnixNano())
	}
	t.w1.Flush()
	removed_count := 0
	for _, v := range t.txInfo.logs {
		if !t.logs_removed[v.logindex] {
			loginfo := []string{
				strings.ToLower(v.from),
				strings.ToLower(v.to),
				strconv.Itoa(v.logindex - removed_count),
				strings.ToLower(v.address),
				v.data,
				strconv.Itoa(v.topiccount),
				v.topic0,
				v.topics,
				strconv.Itoa(v.tracepos),
				strconv.Itoa(v.tracedepth)}

			loginfo = append(txinfo, loginfo...)
			loginfo = append(blkinfo, loginfo...)
			_, _ = t.w2.WriteString(strings.Join(loginfo, "^") + "\n")
		} else {
			removed_count++
		}
	}
	t.w2.Flush()
}

func (t *CSVTracer) CaptureArbitrumTransfer(env *EVM, from, to *common.Address, value *big.Int, before bool, purpose string) {
}

func (t *CSVTracer) CaptureArbitrumStorageGet(key common.Hash, depth int, before bool) {}

func (t *CSVTracer) CaptureArbitrumStorageSet(key, value common.Hash, depth int, before bool) {}

func (t *CSVTracer) CaptureStylusHostio(name string, args, outs []byte, startInk, endInk uint64) {}
