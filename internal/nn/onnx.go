// Package nn runs small neural networks from ONNX files in pure Go. It knows
// only the operators that the face models of Heirfold use, and it refuses a
// file with any other operator when it loads the file. It has no dependency
// outside the standard library.
package nn

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// ErrFormat means that the file is not an ONNX model that this package can run.
var ErrFormat = errors.New("nn: the model file is not valid")

// Tensor is an array of 32-bit floats with a shape.
type Tensor struct {
	Shape []int
	Data  []float32
}

// NewTensor returns a tensor of zeros.
func NewTensor(shape ...int) *Tensor {
	size := 1
	for _, d := range shape {
		size *= d
	}
	return &Tensor{Shape: append([]int(nil), shape...), Data: make([]float32, size)}
}

type attribute struct {
	f      float32
	i      int64
	s      string
	ints   []int64
	floats []float32
}

type node struct {
	op      string
	inputs  []string
	outputs []string
	attrs   map[string]attribute
}

func (n *node) ints(name string, fallback []int64) []int64 {
	if a, ok := n.attrs[name]; ok && len(a.ints) > 0 {
		return a.ints
	}
	return fallback
}

func (n *node) int(name string, fallback int64) int64 {
	if a, ok := n.attrs[name]; ok {
		return a.i
	}
	return fallback
}

func (n *node) float(name string, fallback float32) float32 {
	if a, ok := n.attrs[name]; ok {
		return a.f
	}
	return fallback
}

// Model is a loaded network.
type Model struct {
	nodes   []node
	weights map[string]*Tensor
	// shapes holds the constant tensors of whole numbers, such as the target of a Reshape.
	shapes  map[string][]int64
	input   string
	outputs []string
	// uses counts the readers of each value, so a run frees a value after its last use.
	uses map[string]int
}

// Outputs returns the names of the results, in the order of the file.
func (m *Model) Outputs() []string { return append([]string(nil), m.outputs...) }

// reader walks the fields of one protobuf message.
type reader struct {
	data []byte
	at   int
}

func (r *reader) varint() (uint64, error) {
	var value uint64
	for shift := uint(0); shift < 64; shift += 7 {
		if r.at >= len(r.data) {
			return 0, ErrFormat
		}
		b := r.data[r.at]
		r.at++
		value |= uint64(b&0x7F) << shift
		if b < 0x80 {
			return value, nil
		}
	}
	return 0, ErrFormat
}

// next returns the next field: its number, its wire type, and its value as
// a number or as bytes.
func (r *reader) next() (field int, wire int, number uint64, bytes []byte, err error) {
	key, err := r.varint()
	if err != nil {
		return 0, 0, 0, nil, err
	}
	field, wire = int(key>>3), int(key&7)
	switch wire {
	case 0:
		number, err = r.varint()
	case 1:
		if r.at+8 > len(r.data) {
			return 0, 0, 0, nil, ErrFormat
		}
		bytes = r.data[r.at : r.at+8]
		r.at += 8
	case 2:
		var size uint64
		if size, err = r.varint(); err != nil {
			return 0, 0, 0, nil, err
		}
		left := len(r.data) - r.at
		if left < 0 || size > uint64(left) { // #nosec G115 -- left is not negative here.
			return 0, 0, 0, nil, ErrFormat
		}
		length := int(size) // #nosec G115 -- size is at most left, and left is an int.
		bytes = r.data[r.at : r.at+length]
		r.at += length
	case 5:
		if r.at+4 > len(r.data) {
			return 0, 0, 0, nil, ErrFormat
		}
		bytes = r.data[r.at : r.at+4]
		r.at += 4
	default:
		err = ErrFormat
	}
	return field, wire, number, bytes, err
}

func (r *reader) more() bool { return r.at < len(r.data) }

// packedInts reads whole numbers that are stored one by one or as a block.
func packedInts(out []int64, wire int, number uint64, bytes []byte) ([]int64, error) {
	if wire == 0 {
		return append(out, int64(number)), nil // #nosec G115 -- the two's complement form is the value.
	}
	block := reader{data: bytes}
	for block.more() {
		v, err := block.varint()
		if err != nil {
			return nil, err
		}
		out = append(out, int64(v)) // #nosec G115 -- the two's complement form is the value.
	}
	return out, nil
}

func floats(bytes []byte) []float32 {
	out := make([]float32, len(bytes)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(bytes[i*4:]))
	}
	return out
}

const (
	typeFloat = 1
	typeInt64 = 7
	// maxElements bounds one tensor: the largest of the face models has 6.4 million numbers.
	maxElements = 64 << 20
)

// parseTensor reads a TensorProto. It returns floats or whole numbers.
func parseTensor(data []byte) (name string, tensor *Tensor, whole []int64, err error) {
	r := reader{data: data}
	var dims []int64
	var kind uint64
	var raw []byte
	var floatData []float32
	var intData []int64
	for r.more() {
		field, wire, number, bytes, err := r.next()
		if err != nil {
			return "", nil, nil, err
		}
		switch field {
		case 1:
			if dims, err = packedInts(dims, wire, number, bytes); err != nil {
				return "", nil, nil, err
			}
		case 2:
			kind = number
		case 4:
			if wire == 2 || wire == 5 {
				floatData = append(floatData, floats(bytes)...)
			}
		case 7:
			if intData, err = packedInts(intData, wire, number, bytes); err != nil {
				return "", nil, nil, err
			}
		case 8:
			name = string(bytes)
		case 9:
			raw = bytes
		}
	}
	size := int64(1)
	shape := make([]int, len(dims))
	for i, d := range dims {
		if d < 0 || d > maxElements {
			return "", nil, nil, ErrFormat
		}
		size *= d
		if size > maxElements {
			return "", nil, nil, ErrFormat
		}
		shape[i] = int(d)
	}
	switch kind {
	case typeFloat:
		if raw != nil {
			floatData = floats(raw)
		}
		if int64(len(floatData)) != size {
			return "", nil, nil, ErrFormat
		}
		return name, &Tensor{Shape: shape, Data: floatData}, nil, nil
	case typeInt64:
		if raw != nil {
			intData = make([]int64, len(raw)/8)
			for i := range intData {
				intData[i] = int64(binary.LittleEndian.Uint64(raw[i*8:])) // #nosec G115 -- the two's complement form is the value.
			}
		}
		if int64(len(intData)) != size {
			return "", nil, nil, ErrFormat
		}
		return name, nil, intData, nil
	}
	return "", nil, nil, fmt.Errorf("%w: tensor %q has a type that is not supported", ErrFormat, name)
}

func parseAttribute(data []byte) (string, attribute, error) {
	r := reader{data: data}
	var name string
	var out attribute
	for r.more() {
		field, wire, number, bytes, err := r.next()
		if err != nil {
			return "", out, err
		}
		switch field {
		case 1:
			name = string(bytes)
		case 2:
			if wire == 5 {
				out.f = math.Float32frombits(binary.LittleEndian.Uint32(bytes))
			}
		case 3:
			out.i = int64(number) // #nosec G115 -- the two's complement form is the value.
		case 4:
			out.s = string(bytes)
		case 7:
			if wire == 2 || wire == 5 {
				out.floats = append(out.floats, floats(bytes)...)
			}
		case 8:
			if out.ints, err = packedInts(out.ints, wire, number, bytes); err != nil {
				return "", out, err
			}
		}
	}
	return name, out, nil
}

func parseNode(data []byte) (node, error) {
	r := reader{data: data}
	out := node{attrs: map[string]attribute{}}
	for r.more() {
		field, _, _, bytes, err := r.next()
		if err != nil {
			return out, err
		}
		switch field {
		case 1:
			out.inputs = append(out.inputs, string(bytes))
		case 2:
			out.outputs = append(out.outputs, string(bytes))
		case 4:
			out.op = string(bytes)
		case 5:
			name, value, err := parseAttribute(bytes)
			if err != nil {
				return out, err
			}
			out.attrs[name] = value
		}
	}
	return out, nil
}

// valueName reads the name of a ValueInfoProto.
func valueName(data []byte) (string, error) {
	r := reader{data: data}
	for r.more() {
		field, _, _, bytes, err := r.next()
		if err != nil {
			return "", err
		}
		if field == 1 {
			return string(bytes), nil
		}
	}
	return "", nil
}

// Load reads an ONNX model. It checks every operator, so Run meets no
// surprise in the graph.
func Load(data []byte) (*Model, error) {
	r := reader{data: data}
	var graph []byte
	for r.more() {
		field, _, _, bytes, err := r.next()
		if err != nil {
			return nil, err
		}
		if field == 7 {
			graph = bytes
		}
	}
	if graph == nil {
		return nil, ErrFormat
	}
	m := &Model{weights: map[string]*Tensor{}, shapes: map[string][]int64{}, uses: map[string]int{}}
	var inputs []string
	g := reader{data: graph}
	for g.more() {
		field, _, _, bytes, err := g.next()
		if err != nil {
			return nil, err
		}
		switch field {
		case 1:
			n, err := parseNode(bytes)
			if err != nil {
				return nil, err
			}
			m.nodes = append(m.nodes, n)
		case 5:
			name, tensor, whole, err := parseTensor(bytes)
			if err != nil {
				return nil, err
			}
			if tensor != nil {
				m.weights[name] = tensor
			} else {
				m.shapes[name] = whole
			}
		case 11:
			name, err := valueName(bytes)
			if err != nil {
				return nil, err
			}
			inputs = append(inputs, name)
		case 12:
			name, err := valueName(bytes)
			if err != nil {
				return nil, err
			}
			m.outputs = append(m.outputs, name)
		}
	}
	for _, name := range inputs {
		if _, weight := m.weights[name]; weight {
			continue
		}
		if _, shape := m.shapes[name]; shape {
			continue
		}
		if m.input != "" {
			return nil, fmt.Errorf("%w: more than one input", ErrFormat)
		}
		m.input = name
	}
	if m.input == "" || len(m.outputs) == 0 || len(m.nodes) == 0 {
		return nil, ErrFormat
	}
	known := map[string]bool{m.input: true}
	for i := range m.nodes {
		n := &m.nodes[i]
		if err := check(m, n); err != nil {
			return nil, err
		}
		for _, name := range n.inputs {
			if name == "" {
				continue
			}
			_, weight := m.weights[name]
			_, shape := m.shapes[name]
			if !known[name] && !weight && !shape {
				return nil, fmt.Errorf("%w: %s reads %q before a node makes it", ErrFormat, n.op, name)
			}
			m.uses[name]++
		}
		for _, name := range n.outputs {
			known[name] = true
		}
	}
	for _, name := range m.outputs {
		if !known[name] {
			return nil, fmt.Errorf("%w: no node makes the output %q", ErrFormat, name)
		}
		m.uses[name]++
	}
	return m, nil
}
