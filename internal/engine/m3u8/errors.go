package m3u8

import "errors"

var (
	ErrInvalidPlaylist = errors.New("m3u8: invalid playlist format")
	ErrNoVariants      = errors.New("m3u8: master playlist has no variants")
	ErrNoSegments      = errors.New("m3u8: media playlist has no segments")
	ErrEncryptedStream = errors.New("m3u8: encrypted streams not yet supported")
	ErrMergeFailed     = errors.New("m3u8: failed to merge segments")
)
