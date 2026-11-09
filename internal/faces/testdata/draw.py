"""Draws the test picture: invented faces, no real people. The numbers in
drawn.json come from OpenCV for this picture:

    python3 draw.py ../models
"""
import json
import os
import random
import sys

import cv2
import numpy as np
from PIL import Image, ImageDraw

here = os.path.dirname(os.path.abspath(__file__))
random.seed(7)
im = Image.new("RGB", (2000, 1000), (215, 205, 150))
d = ImageDraw.Draw(im)
for i in range(7):
    cx = 2000 * (i + 1) / 8
    cy = 380 + random.randint(-30, 30)
    r = 70
    skin = random.choice([(236, 200, 170), (214, 170, 135), (245, 215, 190)])
    hair = random.choice([(40, 30, 25), (120, 80, 40), (200, 200, 200), (20, 20, 20)])
    d.ellipse([cx - r, cy - r * 1.15, cx + r, cy + r * 1.15], fill=skin, outline=(60, 40, 30), width=3)
    d.pieslice([cx - r * 1.05, cy - r * 1.3, cx + r * 1.05, cy + r * 0.4], 180, 360, fill=hair)
    for dx in (-0.38, 0.38):
        d.ellipse([cx + dx * r - r * 0.09, cy - r * 0.15, cx + dx * r + r * 0.09, cy + r * 0.03], fill=(30, 30, 30))
    d.arc([cx - r * 0.4, cy + r * 0.2, cx + r * 0.4, cy + r * 0.7], 20, 160, fill=(120, 40, 40), width=4)
    d.rectangle([cx - r * 1.5, cy + r * 1.25, cx + r * 1.5, cy + r * 3.2], fill=tuple(random.randint(40, 200) for _ in range(3)))
im = im.resize((640, 320), Image.LANCZOS)
im.save(os.path.join(here, "drawn.png"), optimize=True)

models = sys.argv[1]
img = cv2.imread(os.path.join(here, "drawn.png"))
h, w = img.shape[:2]
finder = cv2.FaceDetectorYN.create(os.path.join(models, "face_detection_yunet_2023mar.onnx"), "", (w, h), 0.7, 0.3, 5000)
_, found = finder.detect(img)
out = []
recognizer = None
path = os.path.join(models, "face_recognition_sface_2021dec.onnx")
if os.path.exists(path):
    recognizer = cv2.FaceRecognizerSF.create(path, "")
for f in [] if found is None else found:
    item = {"box": [float(v) for v in f[:4]], "marks": [float(v) for v in f[4:14]], "score": float(f[14])}
    if recognizer is not None:
        vector = recognizer.feature(recognizer.alignCrop(img, f)).reshape(-1)
        item["vector"] = [float(v) for v in vector / np.linalg.norm(vector)]
    out.append(item)
json.dump({"width": w, "height": h, "faces": out}, open(os.path.join(here, "drawn.json"), "w"))
print(len(out), "faces", [round(o["score"], 3) for o in out])
