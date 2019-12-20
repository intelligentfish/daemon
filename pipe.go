package daemon

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"sync/atomic"
)

// XPipe 管道
type XPipe struct {
	closed    int32
	ReadPipe  *os.File
	WritePipe *os.File
}

// NewXPipe 工厂方法
func NewXPipe() *XPipe {
	object := &XPipe{}
	var err error
	object.ReadPipe, object.WritePipe, err = os.Pipe()
	panicOnError(err)
	return object
}

// GetReadPipe 获取管道
func (object *XPipe) GetReadPipe() *os.File {
	return object.ReadPipe
}

// SetReadPipe 设置读管道
func (object *XPipe) SetReadPipe(readPipe *os.File) *XPipe {
	object.ReadPipe = readPipe
	return object
}

// GetWritePipe 获取写管道
func (object *XPipe) GetWritePipe() *os.File {
	return object.WritePipe
}

// SetWritePipe 设置写管道
func (object *XPipe) SetWritePipe(writePipe *os.File) *XPipe {
	object.WritePipe = writePipe
	return object
}

// IsClosed 是否已关闭
func (object *XPipe) IsClosed() bool {
	return 1 == atomic.LoadInt32(&object.closed)
}

// Close 关闭命名管道
func (object *XPipe) Close() (err error) {
	if !atomic.CompareAndSwapInt32(&object.closed, 0, 1) {
		return
	}
	if nil != object.ReadPipe {
		if err = object.ReadPipe.Close(); nil != err {
			return
		}
		object.ReadPipe = nil
	}
	if nil != object.WritePipe {
		if err = object.WritePipe.Close(); nil != err {
			return
		}
		object.WritePipe = nil
	}
	return
}

// writeEmpty 写空
func (object *XPipe) writeEmpty(raw []byte) (err error) {
	var n int
	for 0 < len(raw) {
		n, err = object.WritePipe.Write(raw)
		if nil != err {
			return
		}
		raw = raw[n:]
	}
	return
}

// Write 写入
func (object *XPipe) Write(raw []byte) (err error) {
	if object.IsClosed() {
		err = errors.New("XPipe closed")
		return
	}
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(raw)))
	if err = object.writeEmpty(header); nil != err {
		return
	}
	if err = object.writeEmpty(raw); nil != err {
		return
	}
	return
}

// Read 读取
func (object *XPipe) Read(callback func(data []byte) bool) (err error) {
	if object.IsClosed() {
		err = errors.New("XPipe closed")
		return
	}
	flag := true
	readBuf := NewBuffer(1 << 16)
	var n int
	for flag {
		n, err = object.ReadPipe.Read(readBuf.Internal[readBuf.GetWriteIndex():])
		if nil != err {
			if io.EOF == err {
				flag = false
			} else {
				return
			}
		}
		if 0 >= n {
			break
		}
		readBuf.SetWriteIndex(readBuf.GetWriteIndex() + n)
		for 4 <= readBuf.ReadableBytes() {
			chunkSize := int(binary.BigEndian.Uint32(readBuf.Slice(4)))
			if chunkSize+4 > readBuf.ReadableBytes() {
				break
			}
			readBuf.SetReadIndex(readBuf.GetReadIndex() + 4)
			flag = callback(readBuf.Slice(chunkSize))
			if !flag {
				break
			}
			readBuf.SetReadIndex(readBuf.GetReadIndex() + chunkSize)
			readBuf.DiscardReadBytes()
		}
	}
	return
}
