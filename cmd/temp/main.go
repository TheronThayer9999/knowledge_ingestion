package main

import (
	"context"

	"github.com/redis/go-redis/v9"
)

func main() {
	var ctx = context.Background()
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})
	defer rdb.Close()

	err := rdb.Ping(ctx).Err()
	if err != nil {
		panic(err)
	}
	println("Connected to redis")
}
