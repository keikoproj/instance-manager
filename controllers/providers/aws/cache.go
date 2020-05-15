package aws

import (
	"time"

	"github.com/keikoproj/aws-sdk-go-cache/cache"
)

const (
	CacheDefaultTTL                 time.Duration = time.Second * 0
	GetInstanceProfileTTL           time.Duration = 60 * time.Second
	GetRoleTTL                      time.Duration = 60 * time.Second
	DescribeNodegroupTTL            time.Duration = 30 * time.Second
	DescribeAutoScalingGroupsTTL    time.Duration = 30 * time.Second
	DescribeLaunchConfigurationsTTL time.Duration = 30 * time.Second
	CacheMaxItems                   int64         = 5000
	CacheItemsToPrune               uint32        = 500
)

var cacheCfg = cache.NewConfig(CacheDefaultTTL, CacheMaxItems, CacheItemsToPrune)
