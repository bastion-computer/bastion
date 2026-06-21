package cluster

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/bastion-computer/bastion/core/internal/services/template"
)

// TemplateArchiveStore persists cluster template archives.
type TemplateArchiveStore interface {
	Put(context.Context, string, io.Reader) error
	Get(context.Context, string, io.Writer) error
	Delete(context.Context, string) error
}

// S3ArchiveStoreOptions configures S3-compatible template archive storage.
type S3ArchiveStoreOptions struct {
	Bucket          string
	Region          string
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	UsePathStyle    bool
}

type s3TemplateArchiveStore struct {
	client *s3.Client
	bucket string
}

// NewS3TemplateArchiveStore returns S3-compatible archive storage.
func NewS3TemplateArchiveStore(ctx context.Context, opts S3ArchiveStoreOptions) (TemplateArchiveStore, error) {
	if opts.Bucket == "" {
		return nil, errors.New("cluster S3 bucket is required")
	}

	region := opts.Region
	if region == "" {
		region = "us-east-1"
	}

	loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if opts.AccessKeyID != "" || opts.SecretAccessKey != "" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(opts.AccessKeyID, opts.SecretAccessKey, "")))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load S3 config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.UsePathStyle = opts.UsePathStyle
		if opts.Endpoint != "" {
			options.BaseEndpoint = aws.String(opts.Endpoint)
		}
	})

	return s3TemplateArchiveStore{client: client, bucket: opts.Bucket}, nil
}

func (s s3TemplateArchiveStore) Put(ctx context.Context, key string, body io.Reader) error {
	if _, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(template.ArchiveContentType),
	}); err != nil {
		return fmt.Errorf("put template archive: %w", err)
	}

	return nil
}

func (s s3TemplateArchiveStore) Get(ctx context.Context, key string, writer io.Writer) error {
	res, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("get template archive: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if _, err := io.Copy(writer, res.Body); err != nil {
		return fmt.Errorf("read template archive: %w", err)
	}

	return nil
}

func (s s3TemplateArchiveStore) Delete(ctx context.Context, key string) error {
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}); err != nil {
		return fmt.Errorf("delete template archive: %w", err)
	}

	return nil
}
