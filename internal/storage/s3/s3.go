package s3

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	UseSSL    bool
}

type Client struct {
	client *s3.Client
}

func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	scheme := "http"
	if cfg.UseSSL {
		scheme = "https"
	}

	endpoint := fmt.Sprintf("%s://%s", scheme, cfg.Endpoint)

	awsCfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				cfg.AccessKey,
				cfg.SecretKey,
				"",
			),
		),
		awsconfig.WithRegion("us-east-1"),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = true
	})

	return &Client{
		client: client,
	}, nil
}

func (c *Client) EnsureBucket(ctx context.Context, bucket string) error {
	_, err := c.client.HeadBucket(
		ctx,
		&s3.HeadBucketInput{
			Bucket: aws.String(bucket),
		},
	)
	if err == nil {
		return nil
	}

	_, err = c.client.CreateBucket(
		ctx,
		&s3.CreateBucketInput{
			Bucket: aws.String(bucket),
		},
	)
	if err != nil {
		return fmt.Errorf("create bucket %q: %w", bucket, err)
	}

	return nil
}

func (c *Client) Ping(ctx context.Context, bucket string) error {
	_, err := c.client.HeadBucket(
		ctx,
		&s3.HeadBucketInput{
			Bucket: aws.String(bucket),
		},
	)
	if err != nil {
		return fmt.Errorf("head bucket %q: %w", bucket, err)
	}

	return nil
}

func (c *Client) PutObject(
	ctx context.Context,
	bucket string,
	key string,
	body io.Reader,
	contentType string,
	size int64,
) error {
	_, err := c.client.PutObject(
		ctx,
		&s3.PutObjectInput{
			Bucket:        aws.String(bucket),
			Key:           aws.String(key),
			Body:          body,
			ContentType:   aws.String(contentType),
			ContentLength: aws.Int64(size),
		},
	)
	if err != nil {
		return fmt.Errorf("put object %q: %w", key, err)
	}

	return nil
}

func (c *Client) GetObject(
	ctx context.Context,
	bucket string,
	key string,
) (io.ReadCloser, string, int64, error) {
	output, err := c.client.GetObject(
		ctx,
		&s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		},
	)
	if err != nil {
		return nil, "", 0, fmt.Errorf("get object %q: %w", key, err)
	}

	contentType := ""
	if output.ContentType != nil {
		contentType = *output.ContentType
	}

	contentLength := int64(0)
	if output.ContentLength != nil {
		contentLength = *output.ContentLength
	}

	return output.Body, contentType, contentLength, nil
}

func (c *Client) DeleteObject(
	ctx context.Context,
	bucket string,
	key string,
) error {
	_, err := c.client.DeleteObject(
		ctx,
		&s3.DeleteObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		},
	)
	if err != nil {
		return fmt.Errorf("delete object %q: %w", key, err)
	}

	return nil
}
