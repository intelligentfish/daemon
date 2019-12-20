package daemon

import "encoding/binary"

// buffer 缓冲区
type buffer struct {
	Internal         []byte // 存储
	readIndex        int    // 读索引
	writeIndex       int    // 写索引
	markedReadIndex  int    // 标记的读索引
	markedWriteIndex int    // 标记的写索引
}

// NewBuffer 新建缓存
func NewBuffer(capacity int) *buffer {
	return &buffer{
		Internal: make([]byte, capacity),
	}
}

// growth 增长
func (object *buffer) growth(needSize int) *buffer {
	if object.writeIndex+needSize > cap(object.Internal) {
		size := 2 * cap(object.Internal)
		if 0 == size {
			size = needSize
		}
		copied := make([]byte, size)
		copy(copied, object.Internal)
		object.Internal = copied
	}
	return object
}

// IsEmpty 是否为空
func (object *buffer) IsEmpty() bool {
	return object.readIndex == object.writeIndex
}

// WriteableBytes 可写入字节数
func (object *buffer) WriteableBytes() int {
	return cap(object.Internal) - object.writeIndex
}

// ReadableBytes 可读取字节数
func (object *buffer) ReadableBytes() int {
	return object.writeIndex - object.readIndex
}

// MarkReadIndex 标记读索引
func (object *buffer) MarkReadIndex() *buffer {
	object.markedReadIndex = object.readIndex
	return object
}

// MarkWriteIndex 标记写索引
func (object *buffer) MarkWriteIndex() *buffer {
	object.markedWriteIndex = object.writeIndex
	return object
}

// ResetReadIndex 重置读索引
func (object *buffer) ResetReadIndex() *buffer {
	object.readIndex = object.markedReadIndex
	object.markedReadIndex = 0
	return object
}

// ResetWriteIndex 重置写索引
func (object *buffer) ResetWriteIndex() *buffer {
	object.writeIndex = object.markedWriteIndex
	object.markedWriteIndex = 0
	return object
}

// GetReadIndex 获取读索引
func (object *buffer) GetReadIndex() int {
	return object.readIndex
}

// GetWriteIndex 获取写索引
func (object *buffer) GetWriteIndex() int {
	return object.writeIndex
}

// SetReadIndex 设置读索引
func (object *buffer) SetReadIndex(index int) *buffer {
	object.readIndex = index
	return object
}

// SetWriteIndex 设置写索引
func (object *buffer) SetWriteIndex(index int) *buffer {
	object.writeIndex = index
	return object
}

// WriteBytes 写字节
func (object *buffer) WriteBytes(bytes []byte) *buffer {
	object.growth(len(bytes))
	copy(object.Internal[object.writeIndex:], bytes)
	object.writeIndex += len(bytes)
	return object
}

// Slice 返回切片
func (object *buffer) Slice(size int) []byte {
	return object.Internal[object.readIndex : object.readIndex+size]
}

// WriteUint32 写Uint32
func (object *buffer) WriteUint32(v uint32) *buffer {
	object.growth(4)
	binary.BigEndian.PutUint32(object.Internal[object.writeIndex:], v)
	object.writeIndex += 4
	return object
}

// ReadUint32 读Uint32
func (object *buffer) ReadUint32() (v uint32) {
	v = binary.BigEndian.Uint32(object.Internal[object.readIndex:])
	object.readIndex += 4
	return
}

// DiscardReadBytes 丢弃已读的数据
func (object *buffer) DiscardReadBytes() *buffer {
	copy(object.Internal, object.Internal[object.readIndex:object.writeIndex])
	object.writeIndex -= object.readIndex
	object.readIndex = 0
	return object
}
