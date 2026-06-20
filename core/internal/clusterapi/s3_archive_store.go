//nolint:wsl_v5 // S3 SDK setup is clearer as compact validation and option wiring.
package clusterapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"github.com/bastion-computer/bastion/core/internal/failure"
)

// DefaultS3ArchiveRegion is used when no archive S3 region is configured.
const DefaultS3ArchiveRegion = "us-east-1"

// S3ArchiveStoreConfig configures S3-compatible template archive storage.
type S3ArchiveStoreConfig struct {
	Bucket          string
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	ForcePathStyle  bool
}

// S3ArchiveStore stores template archives in an S3-compatible bucket.
type S3ArchiveStore struct {
	client *s3.Client
	bucket string
}

// NewS3ArchiveStore returns an S3-backed archive store.
func NewS3ArchiveStore(ctx context.Context, cfg S3ArchiveStoreConfig) (*S3ArchiveStore, error) {
	bucket := strings.TrimSpace(cfg.Bucket)
	if bucket == "" {
		return nil, errors.New("S3 archive bucket is required")
	}

	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = DefaultS3ArchiveRegion
	}

	loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if cfg.AccessKeyID != "" || cfg.SecretAccessKey != "" {
		if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
			return nil, errors.New("both S3 access key ID and secret access key are required when configuring static credentials")
		}

		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load S3 archive configuration: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		if cfg.Endpoint != "" {
			options.BaseEndpoint = aws.String(strings.TrimRight(cfg.Endpoint, "/"))
		}

		options.UsePathStyle = cfg.ForcePathStyle
	})

	return &S3ArchiveStore{client: client, bucket: bucket}, nil
}

// Put stores archive bytes in S3.
func (s *S3ArchiveStore) Put(ctx context.Context, key string, contents []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(contents),
	})
	if err != nil {
		return fmt.Errorf("put template archive: %w", err)
	}

	return nil
}

// Get returns archive bytes from S3.
func (s *S3ArchiveStore) Get(ctx context.Context, key string) ([]byte, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, s3ArchiveError(err, "get")
	}
	defer func() { _ = out.Body.Close() }()

	contents, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("read template archive: %w", err)
	}

	return contents, nil
}

// Delete removes archive bytes from S3.
func (s *S3ArchiveStore) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return s3ArchiveError(err, "delete")
	}

	return nil
}

func s3ArchiveError(err error, operation string) error {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound", "404":
			return fmt.Errorf("%w: template archive not found", failure.ErrNotFound)
		}
	}

	return fmt.Errorf("%s template archive: %w", operation, err)
}
