package remotefs

// ByteCounter is a simple io.Writer that counts the number of bytes written to it, to be used in
// conjunction with io.MultiWriter / io.TeeReader.
type ByteCounter struct {
	count int64
}

// Write implements io.Writer.
func (bc *ByteCounter) Write(p []byte) (int, error) {
	bc.count += int64(len(p))
	return len(p), nil
}

// Count returns the number of bytes written to the ByteCounter.
func (bc *ByteCounter) Count() int64 {
	return bc.count
}
