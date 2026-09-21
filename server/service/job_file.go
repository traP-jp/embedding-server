package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"

	"embedding-server/api/config"
)

var errUnsupportedJobImageType = errors.New("unsupported job image type")
var errInvalidS3JobFileConfig = errors.New("invalid s3 job file config")

type JobFileService struct {
	s3     *s3.Client
	bucket string
	prefix string
}

func NewS3JobFileService(ctx context.Context, cfg config.S3Config) (*JobFileService, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.Region == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, errInvalidS3JobFileConfig
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")),
	)
	if err != nil {
		return nil, err
	}

	return &JobFileService{
		s3: s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		}),
		bucket: cfg.Bucket,
		prefix: strings.Trim(cfg.Prefix, "/"),
	}, nil
}

func (s *JobFileService) StoreJobImages(ctx context.Context, jobID uuid.UUID, images [][]byte) ([]string, error) {
	contentTypes := make([]string, 0, len(images))
	// 形式が正しいか確認する
	for _, raw := range images {
		contentType, err := jobImageContentType(raw)
		if err != nil {
			return nil, err
		}
		contentTypes = append(contentTypes, contentType)
	}

	keys := make([]string, 0, len(images))
	for i, raw := range images {
		key := s.objectKey(jobID, i)
		if _, err := s.s3.PutObject(ctx, &s3.PutObjectInput{
			Bucket:      aws.String(s.bucket),
			Key:         aws.String(key),
			Body:        bytes.NewReader(raw),
			ContentType: aws.String(contentTypes[i]),
		}); err != nil {
			_ = s.RemoveJobImages(ctx, keys)
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func (s *JobFileService) RemoveJobImages(ctx context.Context, objectKeys []string) error {
	if len(objectKeys) == 0 {
		return nil
	}

	// S3 の 1 回のリクエストで指定できるキーは最大 1,000 件。
	// HTTP 200 でもオブジェクト単位の削除失敗が返るため、1 件でも失敗したら
	// DB のレコードを残す。
	for keys := range slices.Chunk(objectKeys, 1000) {
		objects := make([]types.ObjectIdentifier, 0, len(keys))
		for _, key := range keys {
			objects = append(objects, types.ObjectIdentifier{Key: aws.String(key)})
		}
		out, err := s.s3.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(s.bucket),
			Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return err
		}
		if len(out.Errors) > 0 {
			return fmt.Errorf("delete job images: %d object(s) failed (code=%s)", len(out.Errors), aws.ToString(out.Errors[0].Code))
		}
	}
	return nil
}

func (s *JobFileService) objectKey(jobID uuid.UUID, index int) string {
	key := jobID.String() + "/" + strconv.Itoa(index)
	if s.prefix == "" {
		return key
	}
	return s.prefix + "/" + key
}

func jobImageContentType(raw []byte) (string, error) {
	switch http.DetectContentType(raw) {
	case "image/jpeg":
		return "image/jpeg", nil
	case "image/png":
		return "image/png", nil
	case "image/webp":
		return "image/webp", nil
	default:
		return "", errUnsupportedJobImageType
	}
}
