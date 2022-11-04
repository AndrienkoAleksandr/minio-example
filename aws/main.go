package main

import (
	"context"
	"fmt"
	"log"

	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go/service/dynamodb"
)

const (
	awsRegion = "us-east-1"
)

func main() {
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		if service == dynamodb.ServiceID && region == awsRegion {
			fmt.Println("------------")
		}
		//	fmt.Printf("Endpoint should work, but I guess for region '%s'", region)
		return aws.Endpoint{
			URL:               "https://minio.tekton-results-2.svc.cluster.local",
			SigningRegion:     awsRegion,
			HostnameImmutable: true,
		}, nil
	})
	credentialsOpt := config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("console", "console123", ""))

	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithEndpointResolverWithOptions(customResolver),
		credentialsOpt,
		config.WithRegion(awsRegion),
	)

	if err != nil {
		fmt.Printf("configuration error, %s \n", err.Error())
	}

	client := s3.NewFromConfig(cfg)

	bucket := "tekton-results"
	key := "my-object-key.txt"

	lo, err := client.ListBuckets(context.TODO(), &s3.ListBucketsInput{})
	if err != nil {
		log.Fatal(err)
	}
	a := lo.Buckets
	for _, bucket := range a {
		fmt.Println(bucket.Name)
	}
	if len(lo.Buckets) == 0 {
		fmt.Println("no buckets!")
	}

	uploader := manager.NewUploader(client)
	out, err := uploader.S3.CreateMultipartUpload(context.TODO(),
		&s3.CreateMultipartUploadInput{
			Bucket: &bucket,
			Key:    &key,
		},
	)
	if err != nil {
		log.Fatal(err)
	}
	uploadId := out.UploadId

	content1 := "Hello multi-part"
	reader1 := strings.NewReader(content1)
	part1, err := uploader.S3.UploadPart(context.TODO(),
		&s3.UploadPartInput{
			UploadId:   uploadId,
			Bucket:     &bucket,
			Key:        &key,
			PartNumber: 1,
			Body:       reader1,
		})
	if err != nil {
		log.Fatal(err)
	}

	// content2 := "Hello multi-part2"
	// reader2 := strings.NewReader(content2)
	// part2, err := uploader.S3.UploadPart(context.TODO(),
	// &s3.UploadPartInput{
	// 	UploadId: uploadId,
	// 	Bucket: &bucket,
	// 	Key: &key,
	// 	PartNumber: 2,
	// 	Body: reader2,
	// })
	// if err != nil {
	// 	log.Fatal(err)
	// }

	_, err = uploader.S3.CompleteMultipartUpload(context.TODO(),
		&s3.CompleteMultipartUploadInput{
			Bucket:   &bucket,
			Key:      &key,
			UploadId: uploadId,
			MultipartUpload: &types.CompletedMultipartUpload{
				Parts: []types.CompletedPart{
					{
						PartNumber: 1,
						ETag:       part1.ETag,
					},
					// {
					// 	PartNumber: 2,
					// 	ETag: part2.ETag,
					// },
				},
			},
		},
	)

	if err != nil {
		log.Fatal(err)
	}
}
