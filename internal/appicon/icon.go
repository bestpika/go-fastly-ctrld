// Package appicon 提供執行檔、主視窗與系統匣共用的綠底白色 F 圖示。
package appicon

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
)

// PNG 將原有 32 像素圖樣等比例繪製為指定大小。
func PNG(size int) []byte {
	if size < 16 || size > 256 {
		panic("圖示大小必須介於 16 與 256 像素")
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			px, py := x*32/size, y*32/size
			if px >= 3 && px < 29 && py >= 3 && py < 29 {
				img.Set(x, y, color.RGBA{R: 16, G: 185, B: 129, A: 255})
			}
			if px >= 9 && px < 13 && py >= 8 && py < 25 || px >= 9 && px < 24 && py >= 8 && py < 12 || px >= 9 && px < 21 && py >= 15 && py < 19 {
				img.Set(x, y, color.White)
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// ICO 包含 Windows 不同縮放比例使用的六種 PNG 圖示。
func ICO() []byte {
	sizes := []int{16, 32, 48, 64, 128, 256}
	data := make([]byte, 6+16*len(sizes))
	binary.LittleEndian.PutUint16(data[2:], 1)
	binary.LittleEndian.PutUint16(data[4:], uint16(len(sizes)))
	for i, size := range sizes {
		image := PNG(size)
		entry := data[6+16*i : 6+16*(i+1)]
		entry[0], entry[1] = byte(size), byte(size)
		binary.LittleEndian.PutUint16(entry[4:], 1)
		binary.LittleEndian.PutUint16(entry[6:], 32)
		binary.LittleEndian.PutUint32(entry[8:], uint32(len(image)))
		binary.LittleEndian.PutUint32(entry[12:], uint32(len(data)))
		data = append(data, image...)
	}
	return data
}
