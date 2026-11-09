# Face models

Both files come from the [OpenCV Zoo](https://github.com/opencv/opencv_zoo). Heirfold runs them with its own small engine in `internal/nn`, in pure Go.

| File | Job | License | In the repository | SHA-256 |
| --- | --- | --- | --- | --- |
| `face_detection_yunet_2023mar.onnx` | Finds faces and five marks on each face (YuNet) | MIT, see `LICENSE-YuNet.txt` | Yes, 227 KB. It is a part of the binary. | `8f2383e4dd3cfbb4553ea8718107fc0423210dc964f9f4280604804ed2552fa4` |
| `face_recognition_sface_2021dec.onnx` | Turns a face into 128 numbers for the comparison of faces (SFace) | Apache-2.0 | No, 36.9 MB. `make models` gets it. | `0ba9fbfa01b5270c96627c4ef784da859931e02f04419c829e83484087c34e79` |

The server finds faces with the first file alone. To let it say who a face is like, the operator puts the second file into the folder `models` of the data directory. The server loads only a file with the checksum above.

The tests that need the second file skip when it is not here.
