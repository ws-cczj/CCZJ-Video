package util

import (
	"fmt"
	"time"

	"github.com/bwmarrin/snowflake"
)

var node *snowflake.Node

func InitSnowFlake() error {
	var err error
	node, err = snowflake.NewNode(1)
	if err != nil {
		return fmt.Errorf("init snowflake failed: %w", err)
	}
	snowflake.Epoch = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano() / 1e6
	return nil
}

func GenID() int64 {
	if node == nil {
		InitSnowFlake()
	}
	id := node.Generate()
	for id.Int64() < 0 {
		id = node.Generate()
	}
	return id.Int64()
}
