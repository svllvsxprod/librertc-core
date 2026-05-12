package videochannel

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"strings"

	"github.com/makiuchi-d/gozxing"
	gzqr "github.com/makiuchi-d/gozxing/qrcode"
	rscqr "rsc.io/qr"
)

const (
	visualTileWidth      = 1080
	visualTileHeight     = 1080
	visualTileHeaderSize = 16
	visualTileMagic      = uint32(0x4c525443)
	visualTileBlack      = byte(64)
	visualTileWhite      = byte(192)
)

// ErrUnexpectedQRFrameSize is returned when the decoded frame size does not match the expected dimensions.
var ErrUnexpectedQRFrameSize = errors.New("unexpected qr frame size")

func eccLevel(level string) rscqr.Level {
	switch level {
	case "medium":
		return rscqr.M
	case "high":
		return rscqr.Q
	case "highest":
		return rscqr.H
	default:
		return rscqr.L
	}
}

func renderVisualFrame(
	payload []byte,
	width, height int,
	codec, recoveryLevel string,
	tileModule, tileRS int,
) ([]byte, error) {
	if codec == "tile" {
		return renderTileFrame(payload, tileModule, tileRS)
	}
	return renderQRFrame(payload, width, height, recoveryLevel)
}

func renderQRFrame(payload []byte, width, height int, recoveryLevel string) ([]byte, error) {
	if len(payload) == 0 {
		return whiteFrame(width, height), nil
	}

	code, err := rscqr.Encode(string(payload), eccLevel(recoveryLevel))
	if err != nil {
		return nil, fmt.Errorf("qr encode: %w", err)
	}

	modules := code.Size + 8
	margin := 2
	scale := min((width-margin*2)/modules, (height-margin*2)/modules)
	if scale < 1 {
		scale = 1
	}

	qrWidth := modules * scale
	qrHeight := modules * scale
	offsetX := (width - qrWidth) / 2
	offsetY := (height - qrHeight) / 2
	frame := whiteFrame(width, height)

	for row := 0; row < modules; row++ {
		for col := 0; col < modules; col++ {
			if !code.Black(col-4, row-4) {
				continue
			}
			x0 := offsetX + col*scale
			y0 := offsetY + row*scale
			for dy := 0; dy < scale; dy++ {
				base := (y0+dy)*width + x0
				for dx := 0; dx < scale; dx++ {
					frame[base+dx] = 0
				}
			}
		}
	}

	return frame, nil
}

func renderTileFrame(payload []byte, tileModule, _ int) ([]byte, error) {
	if len(payload) == 0 {
		return whiteFrame(visualTileWidth, visualTileHeight), nil
	}

	module, err := normalizeTileModule(tileModule)
	if err != nil {
		return nil, err
	}
	capacity := tileCapacity(module)
	if len(payload) > capacity {
		return nil, fmt.Errorf("tile encode: payload %d > maxPayload %d", len(payload), capacity)
	}

	wire := make([]byte, visualTileHeaderSize+len(payload))
	binary.BigEndian.PutUint32(wire[0:4], visualTileMagic)
	binary.BigEndian.PutUint32(wire[4:8], uint32(len(payload)))
	binary.BigEndian.PutUint32(wire[8:12], crc32.ChecksumIEEE(payload))
	copy(wire[visualTileHeaderSize:], payload)

	frame := make([]byte, visualTileWidth*visualTileHeight)
	for i := range frame {
		frame[i] = visualTileWhite
	}
	renderTileBits(frame, wire, module)
	return frame, nil
}

func extractVisualPayload(frame []byte, width, height int, codec string, tileModule, tileRS int) ([]byte, error) {
	if codec == "tile" {
		return extractTilePayload(frame, tileModule, tileRS)
	}
	return extractQRPayload(frame, width, height)
}

func extractQRPayload(frame []byte, width, height int) ([]byte, error) {
	if len(frame) != width*height {
		return nil, fmt.Errorf("%w: got %d expected %dx%d=%d",
			ErrUnexpectedQRFrameSize, len(frame), width, height, width*height)
	}

	img := image.NewGray(image.Rect(0, 0, width, height))
	copy(img.Pix, frame)
	source := gozxing.NewLuminanceSourceFromImage(img)
	bmp, err := gozxing.NewBinaryBitmap(gozxing.NewHybridBinarizer(source))
	if err != nil {
		return nil, fmt.Errorf("qr decode: %w", err)
	}

	result, err := gzqr.NewQRCodeReader().Decode(bmp, nil)
	if err != nil {
		if strings.Contains(err.Error(), "NotFoundException") || strings.Contains(err.Error(), "not found") {
			return nil, nil
		}
		return nil, fmt.Errorf("qr decode: %w", err)
	}

	return qrResultBytes(result), nil
}

func extractTilePayload(frame []byte, tileModule, _ int) ([]byte, error) {
	if len(frame) != visualTileWidth*visualTileHeight {
		return nil, nil
	}
	module, err := normalizeTileModule(tileModule)
	if err != nil {
		return nil, fmt.Errorf("tile codec: %w", err)
	}

	wire := readTileBits(frame, module)
	if len(wire) < visualTileHeaderSize || binary.BigEndian.Uint32(wire[0:4]) != visualTileMagic {
		return nil, nil
	}
	payloadLen := int(binary.BigEndian.Uint32(wire[4:8]))
	if payloadLen < 0 || payloadLen > tileCapacity(module) || visualTileHeaderSize+payloadLen > len(wire) {
		return nil, nil
	}
	payload := make([]byte, payloadLen)
	copy(payload, wire[visualTileHeaderSize:visualTileHeaderSize+payloadLen])
	if crc32.ChecksumIEEE(payload) != binary.BigEndian.Uint32(wire[8:12]) {
		return nil, nil
	}
	return payload, nil
}

func whiteFrame(width, height int) []byte {
	frame := make([]byte, width*height)
	for i := range frame {
		frame[i] = 0xff
	}
	return frame
}

func normalizeTileModule(module int) (int, error) {
	if module <= 0 {
		module = 4
	}
	if module < 1 || module > visualTileWidth || module > visualTileHeight {
		return 0, fmt.Errorf("tile module must fit frame, got %d", module)
	}
	return module, nil
}

func tileCapacity(module int) int {
	cols := visualTileWidth / module
	rows := visualTileHeight / module
	return max(0, (cols*rows)/8-visualTileHeaderSize)
}

func renderTileBits(frame, wire []byte, module int) {
	cols := visualTileWidth / module
	rows := visualTileHeight / module
	maxBits := cols * rows
	for bitIndex := 0; bitIndex < len(wire)*8 && bitIndex < maxBits; bitIndex++ {
		if (wire[bitIndex/8] & (1 << (7 - bitIndex%8))) == 0 {
			continue
		}
		col := bitIndex % cols
		row := bitIndex / cols
		x0 := col * module
		y0 := row * module
		for dy := 0; dy < module; dy++ {
			base := (y0+dy)*visualTileWidth + x0
			for dx := 0; dx < module; dx++ {
				frame[base+dx] = visualTileBlack
			}
		}
	}
}

func readTileBits(frame []byte, module int) []byte {
	cols := visualTileWidth / module
	rows := visualTileHeight / module
	bits := cols * rows
	out := make([]byte, (bits+7)/8)
	for bitIndex := 0; bitIndex < bits; bitIndex++ {
		col := bitIndex % cols
		row := bitIndex / cols
		x := min(col*module+module/2, visualTileWidth-1)
		y := min(row*module+module/2, visualTileHeight-1)
		if frame[y*visualTileWidth+x] < 128 {
			out[bitIndex/8] |= 1 << (7 - bitIndex%8)
		}
	}
	return out
}

func qrResultBytes(result *gozxing.Result) []byte {
	segments, ok := result.GetResultMetadata()[gozxing.ResultMetadataType_BYTE_SEGMENTS].([][]byte)
	if !ok || len(segments) == 0 {
		return []byte(result.GetText())
	}
	if len(segments) == 1 {
		return segments[0]
	}
	total := 0
	for _, segment := range segments {
		total += len(segment)
	}
	out := make([]byte, 0, total)
	for _, segment := range segments {
		out = append(out, segment...)
	}
	return out
}
