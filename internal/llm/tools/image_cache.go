package tools

import (
	"fmt"
	"sync"
)

// CachedImage holds image data in memory for the duration of a single
// SendMessageStream call, so OCR / grading tools receive image bytes
// directly without temp files or LLM-path-copying.
//
// AttachmentID / URL 让工具调用结果(如 grade_with_ocr 的 ImageRefs)
// 能直接回带图片的稳定引用,前端无需再靠"上一条消息的 attachments"
// 这种位置耦合去倒推图片来源。
type CachedImage struct {
	Data         []byte
	MimeType     string
	OrigName     string
	AttachmentID string
	URL          string
}

var (
	imageCacheMu sync.RWMutex
	imageCache   = map[string][]CachedImage{} // sessionID → images
)

// StoreSessionImages sets the image list for a session.
func StoreSessionImages(sessionID string, images []CachedImage) {
	imageCacheMu.Lock()
	defer imageCacheMu.Unlock()
	imageCache[sessionID] = images
}

// GetSessionImages returns images by index. The session must have been
// populated by StoreSessionImages first.
func GetSessionImages(sessionID string, indices []int) ([]CachedImage, error) {
	imageCacheMu.RLock()
	defer imageCacheMu.RUnlock()
	imgs, ok := imageCache[sessionID]
	if !ok {
		return nil, fmt.Errorf("no images for session %s", sessionID)
	}
	out := make([]CachedImage, 0, len(indices))
	for _, i := range indices {
		if i < 0 || i >= len(imgs) {
			return nil, fmt.Errorf("image index %d out of range (0-%d)", i, len(imgs)-1)
		}
		out = append(out, imgs[i])
	}
	return out, nil
}

// DeleteSessionImages frees the cached images for a session.
func DeleteSessionImages(sessionID string) {
	imageCacheMu.Lock()
	defer imageCacheMu.Unlock()
	delete(imageCache, sessionID)
}
