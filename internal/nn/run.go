package nn

import (
	"fmt"
	"math"
	"runtime"
	"sync"
)

// Workers is the number of goroutines that one run uses. Zero means all processors.
var Workers = 0

// each splits the range [0, n) between the workers.
func each(n int, fn func(from, to int)) {
	workers := Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	workers = min(workers, n)
	if workers <= 1 {
		fn(0, n)
		return
	}
	var wait sync.WaitGroup
	for w := range workers {
		from, to := n*w/workers, n*(w+1)/workers
		wait.Add(1)
		go func() {
			defer wait.Done()
			fn(from, to)
		}()
	}
	wait.Wait()
}

func unsupported(n *node, what string) error {
	return fmt.Errorf("%w: operator %s: %s", ErrFormat, n.op, what)
}

func same(values []int64, want int64) bool {
	for _, v := range values {
		if v != want {
			return false
		}
	}
	return true
}

// check refuses a node that Run cannot compute.
func check(m *Model, n *node) error {
	if len(n.outputs) < 1 {
		return unsupported(n, "no output")
	}
	need := func(count int) error {
		if len(n.inputs) < count {
			return unsupported(n, "too few inputs")
		}
		return nil
	}
	switch n.op {
	case "Conv":
		if err := need(2); err != nil {
			return err
		}
		w := m.weights[n.inputs[1]]
		if w == nil || len(w.Shape) != 4 {
			return unsupported(n, "the weights must be a constant with four dimensions")
		}
		group := n.int("group", 1)
		if group != 1 && (group != int64(w.Shape[0]) || w.Shape[1] != 1) {
			return unsupported(n, "only plain and depthwise convolutions")
		}
		if !same(n.ints("dilations", nil), 1) || len(n.ints("pads", []int64{0, 0, 0, 0})) != 4 || len(n.ints("strides", []int64{1, 1})) != 2 {
			return unsupported(n, "dilations, pads or strides")
		}
		if a, ok := n.attrs["auto_pad"]; ok && a.s != "" && a.s != "NOTSET" {
			return unsupported(n, "auto_pad")
		}
		if len(n.inputs) > 2 && m.weights[n.inputs[2]] == nil {
			return unsupported(n, "the bias must be a constant")
		}
	case "BatchNormalization":
		if err := need(5); err != nil {
			return err
		}
		for _, name := range n.inputs[1:5] {
			if m.weights[name] == nil {
				return unsupported(n, "the parameters must be constants")
			}
		}
	case "PRelu":
		if err := need(2); err != nil {
			return err
		}
		if m.weights[n.inputs[1]] == nil {
			return unsupported(n, "the slope must be a constant")
		}
	case "Relu", "Sigmoid", "Dropout", "Flatten":
		if err := need(1); err != nil {
			return err
		}
		if n.op == "Flatten" && n.int("axis", 1) != 1 {
			return unsupported(n, "axis")
		}
	case "MaxPool":
		if err := need(1); err != nil {
			return err
		}
		if !same(n.ints("pads", nil), 0) || n.int("ceil_mode", 0) != 0 || len(n.ints("kernel_shape", nil)) != 2 || !same(n.ints("dilations", nil), 1) {
			return unsupported(n, "pads, ceil_mode or kernel")
		}
	case "Resize":
		if err := need(3); err != nil {
			return err
		}
		scales := m.weights[n.inputs[2]]
		if n.attrs["mode"].s != "nearest" || n.attrs["coordinate_transformation_mode"].s != "asymmetric" || n.attrs["nearest_mode"].s != "floor" {
			return unsupported(n, "only nearest, asymmetric, floor")
		}
		if scales == nil || len(scales.Data) != 4 || scales.Data[0] != 1 || scales.Data[1] != 1 ||
			scales.Data[2] != scales.Data[3] || scales.Data[2] != float32(int(scales.Data[2])) || scales.Data[2] < 1 {
			return unsupported(n, "only a whole scale of the height and the width")
		}
	case "Add", "Sub", "Mul":
		if err := need(2); err != nil {
			return err
		}
	case "Transpose":
		if err := need(1); err != nil {
			return err
		}
		perm := n.ints("perm", nil)
		if len(perm) != 4 || perm[0] != 0 || perm[1] != 2 || perm[2] != 3 || perm[3] != 1 {
			return unsupported(n, "only the order 0, 2, 3, 1")
		}
	case "Reshape":
		if err := need(2); err != nil {
			return err
		}
		if m.shapes[n.inputs[1]] == nil {
			return unsupported(n, "the shape must be a constant")
		}
	case "Gemm":
		if err := need(2); err != nil {
			return err
		}
		w := m.weights[n.inputs[1]]
		if w == nil || len(w.Shape) != 2 || n.int("transA", 0) != 0 || n.int("transB", 0) != 1 {
			return unsupported(n, "only X times the transposed constant weights")
		}
		if len(n.inputs) > 2 && m.weights[n.inputs[2]] == nil {
			return unsupported(n, "the bias must be a constant")
		}
	default:
		return fmt.Errorf("%w: operator %s is not supported", ErrFormat, n.op)
	}
	return nil
}

// Run computes the outputs of the model for one input.
func (m *Model) Run(input *Tensor) (map[string]*Tensor, error) {
	values := map[string]*Tensor{m.input: input}
	left := make(map[string]int, len(m.uses))
	for name, count := range m.uses {
		left[name] = count
	}
	// own is true when the node is the last reader of a value that the run
	// made, so the node can write its result into the same memory.
	get := func(name string) (*Tensor, bool, error) {
		if t := m.weights[name]; t != nil {
			return t, false, nil
		}
		t := values[name]
		if t == nil {
			return nil, false, fmt.Errorf("%w: the value %q is missing", ErrFormat, name)
		}
		left[name]--
		last := left[name] == 0
		if last {
			delete(values, name)
		}
		return t, last && name != m.input, nil
	}
	for i := range m.nodes {
		n := &m.nodes[i]
		x, own, err := get(n.inputs[0])
		if err != nil {
			return nil, err
		}
		var out *Tensor
		switch n.op {
		case "Conv":
			var bias *Tensor
			if len(n.inputs) > 2 {
				bias = m.weights[n.inputs[2]]
			}
			out, err = conv(n, x, m.weights[n.inputs[1]], bias)
		case "BatchNormalization":
			out, err = batchNorm(n, target(x, own), m.weights[n.inputs[1]], m.weights[n.inputs[2]], m.weights[n.inputs[3]], m.weights[n.inputs[4]])
		case "PRelu":
			out, err = prelu(target(x, own), m.weights[n.inputs[1]])
		case "Relu":
			out = target(x, own)
			for i, v := range out.Data {
				if v < 0 {
					out.Data[i] = 0
				}
			}
		case "Sigmoid":
			out = target(x, own)
			for i, v := range out.Data {
				out.Data[i] = float32(1 / (1 + math.Exp(-float64(v))))
			}
		case "Dropout":
			out = x
		case "Flatten":
			if len(x.Shape) < 1 {
				return nil, unsupported(n, "shape")
			}
			out = &Tensor{Shape: []int{x.Shape[0], len(x.Data) / max(x.Shape[0], 1)}, Data: x.Data}
		case "MaxPool":
			out, err = maxPool(n, x)
		case "Resize":
			out, err = upsample(n, x, int(m.weights[n.inputs[2]].Data[2]))
		case "Add", "Sub", "Mul":
			var y *Tensor
			if y, _, err = get(n.inputs[1]); err == nil {
				out, err = arithmetic(n, target(x, own), y)
			}
		case "Transpose":
			out, err = toChannelsLast(n, x)
		case "Reshape":
			out, err = reshape(n, x, m.shapes[n.inputs[1]])
		case "Gemm":
			var bias *Tensor
			if len(n.inputs) > 2 {
				bias = m.weights[n.inputs[2]]
			}
			out, err = gemm(n, x, m.weights[n.inputs[1]], bias)
		}
		if err != nil {
			return nil, err
		}
		values[n.outputs[0]] = out
	}
	results := make(map[string]*Tensor, len(m.outputs))
	for _, name := range m.outputs {
		t := values[name]
		if t == nil {
			return nil, fmt.Errorf("%w: the output %q is missing", ErrFormat, name)
		}
		results[name] = t
	}
	return results, nil
}

// target returns the tensor that an operator writes to: the input itself if
// nothing else reads it, or a copy.
func target(x *Tensor, own bool) *Tensor {
	if own {
		return x
	}
	return &Tensor{Shape: append([]int(nil), x.Shape...), Data: append([]float32(nil), x.Data...)}
}

func conv(n *node, x, w, bias *Tensor) (*Tensor, error) {
	if len(x.Shape) != 4 || x.Shape[0] != 1 {
		return nil, unsupported(n, "the input must be one picture")
	}
	channels, height, width := x.Shape[1], x.Shape[2], x.Shape[3]
	filters, perGroup, kh, kw := w.Shape[0], w.Shape[1], w.Shape[2], w.Shape[3]
	depthwise := n.int("group", 1) != 1
	if (depthwise && channels != filters) || (!depthwise && perGroup != channels) {
		return nil, unsupported(n, "the channels of the input do not fit the weights")
	}
	pads := n.ints("pads", []int64{0, 0, 0, 0})
	strides := n.ints("strides", []int64{1, 1})
	top, left, bottom, right := int(pads[0]), int(pads[1]), int(pads[2]), int(pads[3])
	sh, sw := int(strides[0]), int(strides[1])
	if sh < 1 || sw < 1 || height+top+bottom < kh || width+left+right < kw {
		return nil, unsupported(n, "the picture is smaller than the kernel")
	}
	outH := (height+top+bottom-kh)/sh + 1
	outW := (width+left+right-kw)/sw + 1
	out := NewTensor(1, filters, outH, outW)
	plane, outPlane := height*width, outH*outW
	each(filters, func(from, to int) {
		for f := from; f < to; f++ {
			result := out.Data[f*outPlane : (f+1)*outPlane]
			if bias != nil {
				b := bias.Data[f]
				for i := range result {
					result[i] = b
				}
			}
			first, count := 0, channels
			if depthwise {
				first, count = f, 1
			}
			for c := range count {
				in := x.Data[(first+c)*plane : (first+c+1)*plane]
				kernel := w.Data[(f*perGroup+c)*kh*kw : (f*perGroup+c+1)*kh*kw]
				if kh == 1 && kw == 1 && sh == 1 && sw == 1 && top == 0 && left == 0 {
					weight := kernel[0]
					for i, v := range in {
						result[i] += weight * v
					}
					continue
				}
				for ky := range kh {
					for kx := range kw {
						weight := kernel[ky*kw+kx]
						if weight == 0 {
							continue
						}
						// The columns of the output whose input column is inside the picture.
						xFrom := 0
						if left > kx {
							xFrom = (left - kx + sw - 1) / sw
						}
						xTo := min(outW, (width-1+left-kx)/sw+1)
						for oy := range outH {
							iy := oy*sh + ky - top
							if iy < 0 || iy >= height {
								continue
							}
							row := in[iy*width : (iy+1)*width]
							dst := result[oy*outW : (oy+1)*outW]
							ix := xFrom*sw + kx - left
							for ox := xFrom; ox < xTo; ox++ {
								dst[ox] += weight * row[ix]
								ix += sw
							}
						}
					}
				}
			}
		}
	})
	return out, nil
}

// perChannel returns the number of channels and the size of one channel.
func perChannel(n *node, x *Tensor, params ...*Tensor) (int, int, error) {
	if len(x.Shape) < 2 || x.Shape[0] != 1 {
		return 0, 0, unsupported(n, "shape")
	}
	channels := x.Shape[1]
	for _, p := range params {
		if len(p.Data) != channels {
			return 0, 0, unsupported(n, "the parameters do not fit the channels")
		}
	}
	return channels, len(x.Data) / max(channels, 1), nil
}

func batchNorm(n *node, x, gamma, beta, mean, variance *Tensor) (*Tensor, error) {
	channels, plane, err := perChannel(n, x, gamma, beta, mean, variance)
	if err != nil {
		return nil, err
	}
	epsilon := float64(n.float("epsilon", 1e-5))
	for c := range channels {
		scale := float32(float64(gamma.Data[c]) / math.Sqrt(float64(variance.Data[c])+epsilon))
		shift := beta.Data[c] - mean.Data[c]*scale
		values := x.Data[c*plane : (c+1)*plane]
		for i, v := range values {
			values[i] = v*scale + shift
		}
	}
	return x, nil
}

func prelu(x, slope *Tensor) (*Tensor, error) {
	n := &node{op: "PRelu"}
	channels, plane, err := perChannel(n, x, slope)
	if err != nil {
		return nil, err
	}
	for c := range channels {
		s := slope.Data[c]
		values := x.Data[c*plane : (c+1)*plane]
		for i, v := range values {
			if v < 0 {
				values[i] = v * s
			}
		}
	}
	return x, nil
}

func maxPool(n *node, x *Tensor) (*Tensor, error) {
	if len(x.Shape) != 4 || x.Shape[0] != 1 {
		return nil, unsupported(n, "shape")
	}
	kernel := n.ints("kernel_shape", nil)
	strides := n.ints("strides", []int64{1, 1})
	kh, kw, sh, sw := int(kernel[0]), int(kernel[1]), int(strides[0]), int(strides[1])
	channels, height, width := x.Shape[1], x.Shape[2], x.Shape[3]
	if kh < 1 || kw < 1 || sh < 1 || sw < 1 || height < kh || width < kw {
		return nil, unsupported(n, "kernel")
	}
	outH, outW := (height-kh)/sh+1, (width-kw)/sw+1
	out := NewTensor(1, channels, outH, outW)
	each(channels, func(from, to int) {
		for c := from; c < to; c++ {
			in := x.Data[c*height*width:]
			dst := out.Data[c*outH*outW:]
			for oy := range outH {
				for ox := range outW {
					best := float32(math.Inf(-1))
					for ky := range kh {
						row := in[(oy*sh+ky)*width+ox*sw:]
						for kx := range kw {
							if row[kx] > best {
								best = row[kx]
							}
						}
					}
					dst[oy*outW+ox] = best
				}
			}
		}
	})
	return out, nil
}

func upsample(n *node, x *Tensor, scale int) (*Tensor, error) {
	if len(x.Shape) != 4 || x.Shape[0] != 1 {
		return nil, unsupported(n, "shape")
	}
	channels, height, width := x.Shape[1], x.Shape[2], x.Shape[3]
	out := NewTensor(1, channels, height*scale, width*scale)
	outW := width * scale
	for c := range channels {
		for y := range height * scale {
			src := x.Data[(c*height+y/scale)*width:]
			dst := out.Data[(c*height*scale+y)*outW:]
			for i := range outW {
				dst[i] = src[i/scale]
			}
		}
	}
	return out, nil
}

// arithmetic computes x op y into x. y has the shape of x or holds one number.
func arithmetic(n *node, x, y *Tensor) (*Tensor, error) {
	if len(y.Data) != 1 && len(y.Data) != len(x.Data) {
		return nil, unsupported(n, "the second input must have the shape of the first, or one number")
	}
	at := func(i int) float32 {
		if len(y.Data) == 1 {
			return y.Data[0]
		}
		return y.Data[i]
	}
	for i, v := range x.Data {
		switch n.op {
		case "Add":
			x.Data[i] = v + at(i)
		case "Sub":
			x.Data[i] = v - at(i)
		default:
			x.Data[i] = v * at(i)
		}
	}
	return x, nil
}

// toChannelsLast moves the channels behind the height and the width.
func toChannelsLast(n *node, x *Tensor) (*Tensor, error) {
	if len(x.Shape) != 4 {
		return nil, unsupported(n, "shape")
	}
	batch, channels, height, width := x.Shape[0], x.Shape[1], x.Shape[2], x.Shape[3]
	out := NewTensor(batch, height, width, channels)
	plane := height * width
	for b := range batch {
		for c := range channels {
			src := x.Data[(b*channels+c)*plane:]
			for i := range plane {
				out.Data[(b*plane+i)*channels+c] = src[i]
			}
		}
	}
	return out, nil
}

func reshape(n *node, x *Tensor, shape []int64) (*Tensor, error) {
	out := make([]int, len(shape))
	known, free := 1, -1
	for i, d := range shape {
		switch {
		case d == -1 && free < 0:
			free = i
		case d == 0 && i < len(x.Shape):
			out[i] = x.Shape[i]
			known *= out[i]
		case d > 0 && d <= maxElements:
			out[i] = int(d)
			known *= out[i]
		default:
			return nil, unsupported(n, "shape")
		}
	}
	if free >= 0 {
		if known == 0 || len(x.Data)%known != 0 {
			return nil, unsupported(n, "shape")
		}
		out[free] = len(x.Data) / known
		known *= out[free]
	}
	if known != len(x.Data) {
		return nil, unsupported(n, "the new shape has another size")
	}
	return &Tensor{Shape: out, Data: x.Data}, nil
}

func gemm(n *node, x, w, bias *Tensor) (*Tensor, error) {
	rows, columns := w.Shape[0], w.Shape[1]
	if len(x.Data) != columns || (bias != nil && len(bias.Data) != rows) {
		return nil, unsupported(n, "the input does not fit the weights")
	}
	alpha, beta := n.float("alpha", 1), n.float("beta", 1)
	out := NewTensor(1, rows)
	each(rows, func(from, to int) {
		for r := from; r < to; r++ {
			weights := w.Data[r*columns : (r+1)*columns]
			var sum float32
			for i, v := range x.Data {
				sum += weights[i] * v
			}
			sum *= alpha
			if bias != nil {
				sum += beta * bias.Data[r]
			}
			out.Data[r] = sum
		}
	})
	return out, nil
}
