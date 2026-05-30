package storage

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"

	"mechhub-back/internal/config"
)

type OSS struct {
	bucket *oss.Bucket
}

func NewOSS(cfg config.OSSConfig) (*OSS, error) {
	endpoint := fmt.Sprintf("https://oss-%s.aliyuncs.com", cfg.Region)
	client, err := oss.New(endpoint, cfg.AccessKeyID, cfg.AccessKeySecret)
	if err != nil {
		return nil, err
	}
	bucket, err := client.Bucket(cfg.Bucket)
	if err != nil {
		return nil, err
	}
	return &OSS{bucket: bucket}, nil
}

func (o *OSS) Upload(ctx context.Context, key string, body io.Reader, contentType string) error {
	return o.bucket.PutObject(key, body, oss.ContentType(contentType))
}

func (o *OSS) Delete(ctx context.Context, key string) error {
	if key == "" {
		return errors.New("empty key")
	}
	return o.bucket.DeleteObject(key)
}

func (o *OSS) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	return o.bucket.GetObject(key)
}

// Copy 同 bucket 内服务端拷贝(不下载再上传)。用于把 solochat 附件复制成
// 频道附件,让分享出去的批改/对话片段自包含、不依赖原 solochat 权限。
func (o *OSS) Copy(ctx context.Context, srcKey, destKey string) error {
	if srcKey == "" || destKey == "" {
		return errors.New("empty key")
	}
	_, err := o.bucket.CopyObject(srcKey, destKey)
	return err
}
