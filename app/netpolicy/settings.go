package netpolicy

import (
	"cczjVideo/app/apperror"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"sort"
	"time"
)

const SettingsKey = "network_request_policy_v1"

type Settings struct {
	Frequency string `json:"frequency"`
}
type profile struct {
	burst, rate float64
}

var profiles = map[string]profile{
	"standard": {30, 10},
	"steady":   {20, 6},
	"low":      {10, 3},
}

func DecodeSettings(raw string) (Settings, error) {
	settings := Settings{Frequency: "standard"}
	if raw != "" {
		var value map[string]json.RawMessage
		if len(raw) > 4096 || json.Unmarshal([]byte(raw), &value) != nil || value == nil {
			return Settings{}, fmt.Errorf("invalid network policy")
		}
		if field, ok := value["frequency"]; ok {
			var name *string
			if json.Unmarshal(field, &name) != nil || name == nil {
				return Settings{}, fmt.Errorf("invalid network frequency")
			}
			settings.Frequency = *name
		}
	}
	if _, ok := profiles[settings.Frequency]; !ok {
		return Settings{}, fmt.Errorf("unknown network frequency")
	}
	return settings, nil
}
func randomJitter(maximum time.Duration) time.Duration {
	return time.Duration(rand.Int64N(int64(maximum) + 1))
}
func seconds(until, now time.Time) int {
	return min(86400, max(1, int(math.Ceil(until.Sub(now).Seconds()))))
}
func cooldown(until, now time.Time) error {
	return &apperror.Error{Code: apperror.RateLimited, Message: "请求正在冷却，请稍后重试", RetryAfterSeconds: seconds(until, now)}
}
func delayed(err error, until, now time.Time) error {
	p := apperror.Public(err)
	return &apperror.Error{Code: p.Code, Message: p.Message, Cause: err, RetryAfterSeconds: seconds(until, now)}
}
func sortStats(stats []Stat) {
	sort.Slice(stats, func(i, j int) bool { return stats[i].Category < stats[j].Category })
}

func responseFailure(response *http.Response, host string, until, now time.Time) error {
	if response.StatusCode < 400 {
		return nil
	}
	code := apperror.Unavailable
	switch response.StatusCode {
	case 401:
		code = apperror.AuthenticationRequired
	case 403, 412:
		code = apperror.PermissionDenied
	case 404:
		code = apperror.NotFound
	case 429:
		code = apperror.RateLimited
	}
	return &apperror.Error{Code: code, Message: "源站请求失败", Cause: fmt.Errorf("HTTP %d host=%s", response.StatusCode, host), RetryAfterSeconds: seconds(until, now)}
}
