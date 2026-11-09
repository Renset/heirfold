package nn

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type reference struct {
	Shape []int     `json:"shape"`
	Head  []float32 `json:"head"`
	Sum   float64   `json:"sum"`
	Abs   float64   `json:"abs"`
}

// pattern fills a tensor with the same numbers as testdata/reference.py.
func pattern(shape ...int) *Tensor {
	t := NewTensor(shape...)
	for i := range t.Data {
		t.Data[i] = float32((i * 7919) % 256)
	}
	return t
}

// The engine must give the numbers of OpenCV for the same input.
func TestModelsGiveTheNumbersOfOpenCV(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "reference.json"))
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]map[string]reference
	if err := json.Unmarshal(data, &all); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]int{
		"face_detection_yunet_2023mar.onnx":   {1, 3, 640, 640},
		"face_recognition_sface_2021dec.onnx": {1, 3, 112, 112},
	}
	for name, shape := range inputs {
		file, err := os.ReadFile(filepath.Join("..", "faces", "models", name))
		if errors.Is(err, os.ErrNotExist) {
			// The recognition model is large and is not in the repository.
			t.Logf("%s is not here: skipped", name)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		model, err := Load(file)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		started := time.Now()
		results, err := model.Run(pattern(shape...))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		t.Logf("%s: %d outputs in %s", name, len(results), time.Since(started).Round(time.Millisecond))
		want := all[name]
		if len(want) == 0 || len(want) != len(results) {
			t.Fatalf("%s: %d outputs, the reference has %d", name, len(results), len(want))
		}
		for layer, ref := range want {
			got := results[layer]
			if got == nil {
				t.Fatalf("%s: no output %s", name, layer)
			}
			if len(got.Shape) != len(ref.Shape) {
				t.Fatalf("%s %s: shape %v, want %v", name, layer, got.Shape, ref.Shape)
			}
			for i := range ref.Shape {
				if got.Shape[i] != ref.Shape[i] {
					t.Fatalf("%s %s: shape %v, want %v", name, layer, got.Shape, ref.Shape)
				}
			}
			var sum, abs float64
			for _, v := range got.Data {
				sum += float64(v)
				abs += math.Abs(float64(v))
			}
			// The two engines add the numbers in another order, so the last digits differ.
			tolerance := 1e-4 * (1 + ref.Abs)
			if math.Abs(sum-ref.Sum) > tolerance || math.Abs(abs-ref.Abs) > tolerance {
				t.Errorf("%s %s: sum %.6f and %.6f, want %.6f and %.6f", name, layer, sum, abs, ref.Sum, ref.Abs)
			}
			for i, v := range ref.Head {
				if math.Abs(float64(got.Data[i]-v)) > 1e-3*(1+math.Abs(float64(v))) {
					t.Errorf("%s %s: value %d is %.6f, want %.6f", name, layer, i, got.Data[i], v)
				}
			}
		}
	}
}

func TestLoadRefusesOtherFiles(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty":        nil,
		"text":         []byte("this is not a model"),
		"short field":  {0x3a, 0x7f, 0x01},
		"only a graph": {0x3a, 0x00},
	} {
		if _, err := Load(data); !errors.Is(err, ErrFormat) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
