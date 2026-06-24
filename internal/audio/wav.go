package audio

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const wavHeaderSize = 44

// WriteSilencePCM16Mono writes a standard PCM16 mono WAV containing silence.
func WriteSilencePCM16Mono(path string, durationNanos int64, sampleRate int) error {
	if sampleRate <= 0 {
		return fmt.Errorf("sample rate must be positive")
	}
	if durationNanos < 0 {
		durationNanos = 0
	}
	frames := (durationNanos*int64(sampleRate) + 500_000_000) / 1_000_000_000
	dataSize := frames * 2
	if dataSize > int64(^uint32(0))-36 {
		return fmt.Errorf("WAV silence is too large")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create WAV directory: %w", err)
	}
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create silence WAV: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	writer := bufio.NewWriterSize(file, 64*1024)
	if err := writeWAVHeader(writer, uint32(dataSize), sampleRate); err != nil {
		return err
	}
	zeroes := make([]byte, 64*1024)
	remaining := dataSize
	for remaining > 0 {
		chunk := int64(len(zeroes))
		if remaining < chunk {
			chunk = remaining
		}
		if _, err := writer.Write(zeroes[:chunk]); err != nil {
			return fmt.Errorf("write silence WAV: %w", err)
		}
		remaining -= chunk
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush silence WAV: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync silence WAV: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close silence WAV: %w", err)
	}
	closed = true
	return nil
}

// ConcatenatePCM16Mono appends data chunks from compatible PCM WAV files.
func ConcatenatePCM16Mono(inputs []string, output string, sampleRate int) error {
	if sampleRate <= 0 {
		return fmt.Errorf("sample rate must be positive")
	}
	if len(inputs) == 0 {
		return fmt.Errorf("no WAV inputs to concatenate")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	file, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("create concatenated WAV: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()
	if _, err := file.Write(make([]byte, wavHeaderSize)); err != nil {
		return fmt.Errorf("reserve WAV header: %w", err)
	}

	var totalData uint64
	for _, input := range inputs {
		written, err := appendWAVData(file, input, sampleRate)
		if err != nil {
			return err
		}
		totalData += written
		if totalData > uint64(^uint32(0))-36 {
			return fmt.Errorf("concatenated WAV exceeds RIFF 4 GiB limit")
		}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek WAV header: %w", err)
	}
	if err := writeWAVHeader(file, uint32(totalData), sampleRate); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync concatenated WAV: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close concatenated WAV: %w", err)
	}
	closed = true
	return nil
}

func appendWAVData(destination *os.File, path string, expectedSampleRate int) (uint64, error) {
	source, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open WAV part %q: %w", path, err)
	}
	defer source.Close()

	header := make([]byte, 12)
	if _, err := io.ReadFull(source, header); err != nil {
		return 0, fmt.Errorf("read WAV header %q: %w", path, err)
	}
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return 0, fmt.Errorf("%q is not a RIFF/WAVE file", path)
	}

	var formatOK bool
	var total uint64
	chunkHeader := make([]byte, 8)
	for {
		_, err := io.ReadFull(source, chunkHeader)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("read WAV chunk header %q: %w", path, err)
		}
		chunkID := string(chunkHeader[0:4])
		chunkSize := binary.LittleEndian.Uint32(chunkHeader[4:8])
		switch chunkID {
		case "fmt ":
			payload := make([]byte, chunkSize)
			if _, err := io.ReadFull(source, payload); err != nil {
				return 0, fmt.Errorf("read WAV format %q: %w", path, err)
			}
			if len(payload) < 16 {
				return 0, fmt.Errorf("invalid WAV format chunk in %q", path)
			}
			audioFormat := binary.LittleEndian.Uint16(payload[0:2])
			channels := binary.LittleEndian.Uint16(payload[2:4])
			sampleRate := int(binary.LittleEndian.Uint32(payload[4:8]))
			bitsPerSample := binary.LittleEndian.Uint16(payload[14:16])
			if audioFormat != 1 || channels != 1 || sampleRate != expectedSampleRate || bitsPerSample != 16 {
				return 0, fmt.Errorf("unexpected WAV format in %q: format=%d channels=%d sampleRate=%d bits=%d", path, audioFormat, channels, sampleRate, bitsPerSample)
			}
			formatOK = true
		case "data":
			if !formatOK {
				return 0, fmt.Errorf("WAV data appeared before a supported format chunk in %q", path)
			}
			written, err := io.CopyN(destination, source, int64(chunkSize))
			if err != nil {
				return 0, fmt.Errorf("copy WAV data from %q: %w", path, err)
			}
			total += uint64(written)
		default:
			if _, err := source.Seek(int64(chunkSize), io.SeekCurrent); err != nil {
				return 0, fmt.Errorf("skip WAV chunk %q in %q: %w", chunkID, path, err)
			}
		}
		if chunkSize%2 == 1 {
			if _, err := source.Seek(1, io.SeekCurrent); err != nil {
				return 0, fmt.Errorf("skip WAV padding in %q: %w", path, err)
			}
		}
	}
	if !formatOK || total == 0 {
		return 0, fmt.Errorf("no compatible PCM data found in %q", path)
	}
	return total, nil
}

func writeWAVHeader(writer io.Writer, dataSize uint32, sampleRate int) error {
	header := make([]byte, wavHeaderSize)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], 36+dataSize)
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(header[32:34], 2)
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], dataSize)
	if _, err := writer.Write(header); err != nil {
		return fmt.Errorf("write WAV header: %w", err)
	}
	return nil
}
