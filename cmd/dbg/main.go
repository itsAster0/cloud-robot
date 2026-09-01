package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func main() {
	ctx := context.Background()
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithBaseEndpoint("http://localhost:4566"),
	)
	if err != nil {
		panic(err)
	}
	client := dynamodb.NewFromConfig(cfg)
	tables, err := client.ListTables(ctx, &dynamodb.ListTablesInput{})
	if err != nil {
		panic(err)
	}
	fmt.Println("tables:", tables.TableNames)
	for _, name := range tables.TableNames {
		scan, err := client.Scan(ctx, &dynamodb.ScanInput{TableName: aws.String(name)})
		if err != nil {
			fmt.Println("scan", name, "error:", err)
			continue
		}
		fmt.Printf("table %s items=%d\n", name, len(scan.Items))
		for _, item := range scan.Items {
			for key, value := range item {
				switch v := value.(type) {
				case *types.AttributeValueMemberS:
					fmt.Printf("  %s=%s\n", key, v.Value)
				default:
					fmt.Printf("  %s=(%T)\n", key, value)
				}
			}
			fmt.Println("  ---")
		}
	}
}
