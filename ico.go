package ico

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"io/ioutil"

	"github.com/ur65/go-ico/internal/bmp"
)

const (
	headerSize    = 6
	directorySize = 16
)

const (
	bmpHeaderSize     = 14
	bmpInfoHeaderSize = 40
)

type header struct {
	Reserved  int16
	ImageType int16
	Count     int16
}

func readHeader(r io.Reader) (header, error) {
	h := header{}
	binary.Read(r, binary.LittleEndian, &h)

	if h.ImageType != 1 {
		return h, fmt.Errorf("ico: image type should be 1 (got: %d)", h.ImageType)
	}

	if h.Count <= 0 {
		return h, fmt.Errorf("ico: invalid the number of images (got: %d)", h.Count)
	}

	return h, nil
}

type directory struct {
	Width       uint8
	Height      uint8
	ColorCount  uint8
	Reserved    uint8
	Planes      int16
	BitCount    int16
	BytesInRes  int32
	ImageOffset int32
}

func readDirectoris(r io.Reader, size int) ([]directory, error) {
	ds := make([]directory, size)

	binary.Read(r, binary.LittleEndian, ds)

	return ds, nil
}

type bmpReader struct {
	s []byte
	p int64
}

func newBMPReader(dir directory, data []byte) (xor, and *bmpReader, err error) {
	// read DIB header
	r := bytes.NewReader(data)
	ih, err := bmp.ReadInfoHeader(r)
	if err != nil {
		return nil, nil, err
	}

	d, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, nil, err
	}

	psize := uint(ih.ColorUsed) * 4
	if psize == 0 && ih.BitCount < 16 {
		psize = (1 << ih.BitCount) * 4
	}

	w := uint(dir.Width)
	// when width is 0, it is treated as 256 instead.
	if w == 0 {
		w = 256
	}
	// width must be an integer multiple of 4 bytes
	w = (w*uint(ih.BitCount) + 31) / 32 * 4

	h := uint(dir.Height)
	// when height is 0, it is treated as 256 instead.
	if h == 0 {
		h = 256
	}

	xorSize := psize + (w * h)

	bb := &bytes.Buffer{}

	// BITMAPFILEHEADER
	bb.WriteString("BM")
	binary.Write(bb, binary.LittleEndian, bmpHeaderSize+ih.Size+uint32(xorSize))
	binary.Write(bb, binary.LittleEndian, int16(0))
	binary.Write(bb, binary.LittleEndian, int16(0))
	binary.Write(bb, binary.LittleEndian, bmpHeaderSize+ih.Size+uint32(psize))

	// BITMAPINFOHEADER
	binary.Write(bb, binary.LittleEndian, uint32(ih.Size))
	binary.Write(bb, binary.LittleEndian, int32(ih.Width))
	binary.Write(bb, binary.LittleEndian, int32(ih.Height/2))
	binary.Write(bb, binary.LittleEndian, uint16(ih.Planes))
	binary.Write(bb, binary.LittleEndian, uint16(ih.BitCount))
	binary.Write(bb, binary.LittleEndian, uint32(ih.Compression))
	binary.Write(bb, binary.LittleEndian, uint32(ih.SizeImage))
	binary.Write(bb, binary.LittleEndian, int32(ih.XPelsPerMeter))
	binary.Write(bb, binary.LittleEndian, int32(ih.YPelsPerMeter))
	binary.Write(bb, binary.LittleEndian, uint32(ih.ColorUsed))
	binary.Write(bb, binary.LittleEndian, uint32(ih.ColorImportant))

	// COLOR TABLE + IMAGEDATA
	bb.Write(d[:xorSize])

	xor = &bmpReader{
		s: bb.Bytes(),
	}

	bb = &bytes.Buffer{}

	psize = 2 * 4
	andSize := psize + uint(len(d)) - xorSize

	// BITMAPFILEHEADER
	bb.WriteString("BM")
	binary.Write(bb, binary.LittleEndian, bmpHeaderSize+bmpInfoHeaderSize+uint32(andSize))
	binary.Write(bb, binary.LittleEndian, int16(0))
	binary.Write(bb, binary.LittleEndian, int16(0))
	binary.Write(bb, binary.LittleEndian, bmpHeaderSize+bmpInfoHeaderSize+uint32(psize))

	// BITMAPINFOHEADER
	binary.Write(bb, binary.LittleEndian, uint32(bmpInfoHeaderSize))
	binary.Write(bb, binary.LittleEndian, int32(ih.Width))
	binary.Write(bb, binary.LittleEndian, int32(ih.Height/2))
	binary.Write(bb, binary.LittleEndian, uint16(0))
	binary.Write(bb, binary.LittleEndian, uint16(1))
	binary.Write(bb, binary.LittleEndian, uint32(0))
	binary.Write(bb, binary.LittleEndian, uint32(0))
	binary.Write(bb, binary.LittleEndian, int32(0))
	binary.Write(bb, binary.LittleEndian, int32(0))
	binary.Write(bb, binary.LittleEndian, uint32(0))
	binary.Write(bb, binary.LittleEndian, uint32(0))

	// COLOR TABLE
	bb.Write([]byte{0x00, 0x00, 0x00, 0xff})
	bb.Write([]byte{0xff, 0xff, 0xff, 0xff})

	// IMAGEDATA
	bb.Write(d[xorSize:])

	and = &bmpReader{
		s: bb.Bytes(),
	}

	return xor, and, nil
}

func (r *bmpReader) Read(p []byte) (n int, err error) {
	if r.p >= int64(len(r.s)) {
		return 0, io.EOF
	}
	n = copy(p, r.s[r.p:])
	r.p += int64(n)
	return

}

// Decode decodes the given io.Reader and returns all images contained in the data.
func Decode(r io.Reader) ([]image.Image, error) {
	h, err := readHeader(r)
	if err != nil {
		return nil, err
	}

	ds, err := readDirectoris(r, int(h.Count))
	if err != nil {
		return nil, err
	}

	buf, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}

	imgs := make([]image.Image, len(ds))
	for i, v := range ds {
		offset := v.ImageOffset - int32(headerSize+directorySize*len(ds))
		size := v.BytesInRes
		data := buf[offset : offset+size]

		// PNG
		if string(data[1:4]) == "PNG" {
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				return nil, err
			}
			imgs[i] = img
			continue
		}

		// BMP
		xor, and, err := newBMPReader(v, data)
		if err != nil {
			return nil, err
		}

		xorImg, err := bmp.Decode(xor)
		if err != nil {
			return nil, err
		}

		// AND Bitmap has no transparent
		andImg, err := bmp.Decode(and)
		if err != nil {
			return nil, err
		}

		// bmp.Decode always returns image.Paletted from 1 bpp BMP Image
		andImg.(*image.Paletted).Palette[1] = color.RGBA{0, 0, 0, 0}

		img := image.NewRGBA(xorImg.Bounds())
		draw.DrawMask(img, img.Bounds(), xorImg, image.Point{0, 0}, andImg, image.Point{0, 0}, draw.Src)

		imgs[i] = img
	}

	return imgs, nil
}
