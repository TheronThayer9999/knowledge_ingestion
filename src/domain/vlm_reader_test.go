package domain

import (
	"errors"
	"testing"
)

func TestNewImageRef(t *testing.T) {
	img, err := NewImageRef(3, "IMAGE/PNG ", []byte{1, 2, 3})
	if err != nil {
		t.Fatalf("NewImageRef: %v", err)
	}
	if img.PageNum() != 3 || img.MIME() != "image/png" || img.Size() != 3 || img.IsZero() {
		t.Errorf("ImageRef sai: page=%d mime=%q size=%d", img.PageNum(), img.MIME(), img.Size())
	}

	if _, err := NewImageRef(1, "image/png", nil); !errors.Is(err, ErrImageEmpty) {
		t.Errorf("ảnh rỗng: err = %v", err)
	}
	if _, err := NewImageRef(1, "image/webp", []byte{1}); !errors.Is(err, ErrImageFormat) {
		t.Errorf("MIME lạ: err = %v", err)
	}
	if !(ImageRef{}).IsZero() {
		t.Error("zero value phải IsZero")
	}
}
