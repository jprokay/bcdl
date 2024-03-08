package internal

type FileType string

// All of the file types Bandcamp supports.
// Unknown to me if EVERY album supports every format.
const (
	MP3_VO        FileType = "mp3-v0"
	MP3_320       FileType = "mp3-320"
	FLAC          FileType = "flac"
	AAC_HI        FileType = "aac-hi"
	VORBIS        FileType = "vorbis"
	ALAC          FileType = "alac"
	WAV           FileType = "wave"
	AIFF_LOSSLESS FileType = "aiff-lossless"
)

var AllFileTypes = []FileType{MP3_320, MP3_VO, FLAC, AAC_HI, VORBIS, ALAC, WAV, AIFF_LOSSLESS}
