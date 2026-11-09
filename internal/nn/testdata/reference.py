"""Writes reference.json: the outputs of the face models for a fixed input,
as OpenCV computes them. The Go tests compare the engine of Heirfold with
these numbers. Run it again only when a model file changes:

    python3 reference.py ../../faces/models
"""
import json
import os
import sys

import cv2
import numpy as np

MODELS = {
    "face_detection_yunet_2023mar.onnx": (1, 3, 640, 640),
    "face_recognition_sface_2021dec.onnx": (1, 3, 112, 112),
}


def pattern(shape):
    """The same numbers as the function pattern in model_test.go."""
    size = int(np.prod(shape))
    index = np.arange(size, dtype=np.int64)
    return ((index * 7919) % 256).astype(np.float32).reshape(shape)


out = {}
for name, shape in MODELS.items():
    path = os.path.join(sys.argv[1], name)
    if not os.path.exists(path):
        continue
    net = cv2.dnn.readNetFromONNX(path)
    net.setInput(pattern(shape))
    names = net.getUnconnectedOutLayersNames()
    results = net.forward(names)
    out[name] = {
        layer: {
            "shape": list(result.shape),
            "head": [float(v) for v in result.reshape(-1)[:12]],
            "sum": float(result.astype(np.float64).sum()),
            "abs": float(np.abs(result.astype(np.float64)).sum()),
        }
        for layer, result in zip(names, results)
    }
json.dump(out, open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "reference.json"), "w"), indent=1)
print({k: len(v) for k, v in out.items()})
