//go:build !ORT

package classify

import "errors"

func openONNX(name, dir, libDir string) (Classifier, error) {
	return nil, errors.New("onnx: built without ONNX Runtime; build with --tags=ORT")
}
