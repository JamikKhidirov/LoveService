// Package storage abstracts media persistence. Local disk is used by default;
// S3-compatible object storage (MinIO, AWS S3) is enabled with STORAGE_DRIVER=s3.
package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Storage interface {
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) (string, error)
	Delete(ctx context.Context, key string) error
	URL(key string) string
}

func NewLocal(dir, baseURL string) (*Local, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create media dir: %w", err)
	}
	return &Local{dir: dir, baseURL: strings.TrimRight(baseURL, "/")}, nil
}

type Local struct {
	dir     string
	baseURL string
}

func (l *Local) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) (string, error) {
	key = filepath.ToSlash(strings.TrimLeft(key, "/"))
	if key == "" || strings.Contains(key, "..") {
		return "", fmt.Errorf("invalid storage key")
	}
	path := filepath.Join(l.dir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return l.URL(key), nil
}

func (l *Local) Delete(_ context.Context, key string) error {
	path := filepath.Join(l.dir, filepath.FromSlash(strings.TrimLeft(key, "/")))
	if !strings.HasPrefix(path, filepath.Clean(l.dir)) {
		return fmt.Errorf("invalid storage key")
	}
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (l *Local) URL(key string) string {
	return l.baseURL + "/" + strings.TrimLeft(filepath.ToSlash(key), "/")
}

type S3 struct {
	client  *minio.Client
	bucket  string
	baseURL string
	log     *slog.Logger
}

func NewS3(ctx context.Context, endpoint, accessKey, secretKey, bucket, region string, useSSL bool, publicBaseURL string, log *slog.Logger) (*S3, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("create s3 client: %w", err)
	}
	var exists bool
	var lastErr error
	for attempt := 1; attempt <= 30; attempt++ {
		exists, lastErr = client.BucketExists(ctx, bucket)
		if lastErr == nil {
			break
		}
		log.Warn("waiting for object storage", "endpoint", endpoint, "attempt", attempt, "error", lastErr)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("check bucket: %w", lastErr)
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region}); err != nil {
			return nil, fmt.Errorf("create bucket: %w", err)
		}
	}
	// Public read access for profile media. Mutations still require credentials.
	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}`, bucket)
	if err := client.SetBucketPolicy(ctx, bucket, policy); err != nil {
		log.Warn("could not set public read policy on media bucket", "error", err)
	}
	scheme := "http"
	if useSSL {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s/%s", scheme, endpoint, bucket)
	if publicBaseURL != "" {
		baseURL = strings.TrimRight(publicBaseURL, "/")
	}
	return &S3{
		client:  client,
		bucket:  bucket,
		baseURL: baseURL,
		log:     log,
	}, nil
}

func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) (string, error) {
	key = strings.TrimLeft(key, "/")
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return "", fmt.Errorf("upload object: %w", err)
	}
	return s.URL(key), nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, strings.TrimLeft(key, "/"), minio.RemoveObjectOptions{})
}

func (s *S3) URL(key string) string {
	return strings.TrimRight(s.baseURL, "/") + "/" + strings.TrimLeft(key, "/")
}
