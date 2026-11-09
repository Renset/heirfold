VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: web build test check demo clean models

# The model that says who a face is like. It is large, so it is not in the
# repository and not in the binary. Put the file into the folder "models" of
# the data directory of the server. The server checks the same checksum.
SFACE_URL = https://github.com/opencv/opencv_zoo/raw/refs/heads/main/models/face_recognition_sface/face_recognition_sface_2021dec.onnx
SFACE_SHA256 = 0ba9fbfa01b5270c96627c4ef784da859931e02f04419c829e83484087c34e79
SFACE_FILE = internal/faces/models/face_recognition_sface_2021dec.onnx

models:
	test -f $(SFACE_FILE) || curl -fsSL -o $(SFACE_FILE) $(SFACE_URL)
	echo "$(SFACE_SHA256)  $(SFACE_FILE)" | shasum -a 256 -c -

web:
	cd web && npm ci --ignore-scripts && npm run build

build: web
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o heirfold ./cmd/heirfold

test:
	go test -race -count=1 ./...
	cd web && npm test

check:
	go vet ./...
	cd web && npm run check
	scripts/check-data-guard.sh

demo:
	scripts/build-demo.sh dist-demo

clean:
	rm -rf heirfold dist dist-demo web/dist-demo web/node_modules
	find internal/web/dist -mindepth 1 ! -name .gitkeep -delete
