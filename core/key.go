package core

import "encoding/binary"

// Helpers for writing canonical keys (D-002). Every encoding is
// self-delimiting, so a key built by appending several fields in a fixed
// order is injective: different field values always give different keys.

// AppendUint appends v as an unsigned varint.
func AppendUint(buf []byte, v uint64) []byte { return binary.AppendUvarint(buf, v) }

// AppendInt appends v as a zig-zag varint.
func AppendInt(buf []byte, v int64) []byte { return binary.AppendVarint(buf, v) }

// AppendBool appends v as one byte.
func AppendBool(buf []byte, v bool) []byte {
	if v {
		return append(buf, 1)
	}
	return append(buf, 0)
}

// AppendString appends s prefixed by its length.
func AppendString(buf []byte, s string) []byte {
	return append(AppendUint(buf, uint64(len(s))), s...)
}
