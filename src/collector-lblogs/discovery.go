package main

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
)

// LogGroupInfo describes a discovered CloudWatch Log Group.
type LogGroupInfo struct {
	Name           string    `json:"name"`
	ARN            string    `json:"arn,omitempty"`
	RetentionDays  int32     `json:"retentionDays,omitempty"`
	StoredBytes    int64     `json:"storedBytes"`
	LastEventAt    time.Time `json:"lastEventAt,omitempty"`
}

// DiscoverLogGroups finds CloudWatch Log Groups matching the given prefix.
// Pass an empty prefix to list all log groups.
func DiscoverLogGroups(ctx context.Context, client *cloudwatchlogs.Client, prefix string) ([]LogGroupInfo, error) {
	input := &cloudwatchlogs.DescribeLogGroupsInput{}
	if prefix != "" {
		input.LogGroupNamePrefix = aws.String(prefix)
	}

	var results []LogGroupInfo

	paginator := cloudwatchlogs.NewDescribeLogGroupsPaginator(client, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, lg := range page.LogGroups {
			info := LogGroupInfo{
				Name:        aws.ToString(lg.LogGroupName),
				ARN:         aws.ToString(lg.Arn),
				StoredBytes: aws.ToInt64(lg.StoredBytes),
			}
			if lg.RetentionInDays != nil {
				info.RetentionDays = *lg.RetentionInDays
			}
			if lg.CreationTime != nil {
				info.LastEventAt = time.UnixMilli(*lg.CreationTime)
			}
			results = append(results, info)
		}
	}

	return results, nil
}
