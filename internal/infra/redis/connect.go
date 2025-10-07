package redis

import (
	"context"
	"fmt"
	"log"

	"github.com/redis/go-redis/v9"
	"github.com/vogiaan1904/ticketbottle-waitroom/config"
	pkgRedis "github.com/vogiaan1904/ticketbottle-waitroom/pkg/redis"
)

func Connect(ctx context.Context, cfg config.RedisConfig) (*redis.Client, error) {
	cli, err := pkgRedis.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	if err := cli.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping Redis: %w", err)
	}

	log.Println("Connected to Redis.")

	return cli, nil
}

func Disconnect(cli *redis.Client) {
	if cli == nil {
		return
	}

	cli.Close()

	log.Println("Connection to Redis closed.")
}
