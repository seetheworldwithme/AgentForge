// internal/rag/store/vecutil.go
package store

import "math"

func float32ToBytes(f float32) [4]byte {
	bits := math.Float32bits(f)
	return [4]byte{byte(bits), byte(bits >> 8), byte(bits >> 16), byte(bits >> 24)}
}

func vecToBlob(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		b := float32ToBytes(f)
		copy(buf[i*4:i*4+4], b[:])
	}
	return buf
}
