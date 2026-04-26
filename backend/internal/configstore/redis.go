package configstore

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/redis/go-redis/v9"
)

const redisConfigKey = "config:overrides"

type RedisStore struct {
	client *redis.Client
	prefix string
}

func NewRedis(addr, password string, db int, prefix string) *RedisStore {
	return &RedisStore{
		client: redis.NewClient(&redis.Options{
			Addr:     strings.TrimSpace(addr),
			Password: password,
			DB:       db,
		}),
		prefix: firstNonEmpty(prefix, "chatgpt2api:studio"),
	}
}

func (s *RedisStore) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *RedisStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *RedisStore) Load(ctx context.Context) (map[string]map[string]any, error) {
	raw, err := s.client.Get(ctx, s.key(redisConfigKey)).Bytes()
	if err == redis.Nil {
		return map[string]map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := map[string]map[string]any{}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if result == nil {
		return map[string]map[string]any{}, nil
	}
	return result, nil
}

func (s *RedisStore) Save(ctx context.Context, values map[string]map[string]any) error {
	raw, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, s.key(redisConfigKey), raw, 0).Err()
}

func (s *RedisStore) key(name string) string {
	return s.prefix + ":" + name
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
