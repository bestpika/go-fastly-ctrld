package appicon

import (
	"bytes"
	"encoding/binary"
	"image/color"
	"image/png"
	"testing"
)

func TestIconUsesOriginalArtwork(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(PNG(32)))
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		x, y int
		want color.RGBA
	}{{0, 0, color.RGBA{}}, {3, 3, color.RGBA{16, 185, 129, 255}}, {9, 8, color.RGBA{255, 255, 255, 255}}, {20, 16, color.RGBA{255, 255, 255, 255}}, {20, 24, color.RGBA{16, 185, 129, 255}}} {
		if got := color.RGBAModel.Convert(img.At(sample.x, sample.y)); got != sample.want {
			t.Fatalf("圖樣像素不符：%d,%d = %v", sample.x, sample.y, got)
		}
	}
}

func TestWindowsIconContainsAllSizes(t *testing.T) {
	data := ICO()
	if binary.LittleEndian.Uint16(data[2:]) != 1 || binary.LittleEndian.Uint16(data[4:]) != 6 {
		t.Fatal("ICO 標頭不正確")
	}
	for i, size := range []int{16, 32, 48, 64, 128, 256} {
		entry := data[6+16*i : 6+16*(i+1)]
		length, offset := binary.LittleEndian.Uint32(entry[8:]), binary.LittleEndian.Uint32(entry[12:])
		img, err := png.Decode(bytes.NewReader(data[offset : offset+length]))
		if err != nil || img.Bounds().Dx() != size || img.Bounds().Dy() != size || entry[0] != byte(size) || entry[1] != byte(size) {
			t.Fatalf("ICO 尺寸 %d 不正確：%v", size, err)
		}
	}
}
